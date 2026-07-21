package core

import (
	"database/sql"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite" // registers the "sqlite" database/sql driver
)

// The Cursor harness merges two on-disk sources into one aggregate: the IDE's
// global-storage SQLite database (cursor.go) and the Cursor CLI's per-session
// store.db files (cursorcli.go).
//
// Token priority (codeburn / tokenuse):
//  1. Non-zero per-bubble tokenCount (older builds) — authoritative per turn
//  2. Else composerData.promptTokenBreakdown.totalUsedTokens || contextTokensUsed
//     credited once per conversation (latest context-window snapshot)
//  3. Else chars/4 text estimate
//
// Local figures undercount the Cursor admin dashboard (cache + cumulative
// billed input are server-side only).

// loadCursor loads Cursor usage from both the IDE and the CLI.
func loadCursor() *Aggregate {
	paths := cursorPaths()
	return loadCached(Cursor, paths, func() *Aggregate {
		a := newAggregate(Cursor)
		loadCursorIDE(a)
		part := loadParts(cursorCLIPaths(), loadCursorCLIStore)
		if part != nil {
			a.Merge(part)
		}
		return a
	})
}

func cursorPaths() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return cursorCLIPaths()
	}
	paths := []string{
		filepath.Join(home, "Library", "Application Support", "Cursor", "User", "globalStorage", "state.vscdb"),
		filepath.Join(home, ".config", "Cursor", "User", "globalStorage", "state.vscdb"),
	}
	paths = append(paths, cursorCLIPaths()...)
	return paths
}

func cursorCLIPaths() []string {
	return homeGlob(".cursor/chats/*/*/store.db")
}

// ---- Cursor IDE: <config>/Cursor/User/globalStorage/state.vscdb ----

const (
	cursorBatchRows  = 25_000
	cursorBudgetRows = 250_000
)

type cursorBubble struct {
	Type              int    `json:"type"` // 1 = user, 2 = assistant
	Text              string `json:"text"`
	CreatedAt         string `json:"createdAt"`
	ClientRpcSendTime int64  `json:"clientRpcSendTime"`
	ClientSettleTime  int64  `json:"clientSettleTime"`
	ClientEndTime     int64  `json:"clientEndTime"`
	TokenCount        struct {
		InputTokens  int64 `json:"inputTokens"`
		OutputTokens int64 `json:"outputTokens"`
	} `json:"tokenCount"`
	ModelInfo struct {
		ModelName string `json:"modelName"`
	} `json:"modelInfo"`
}

func (b *cursorBubble) time() time.Time {
	if t := parseTime(b.CreatedAt); !t.IsZero() {
		return t
	}
	for _, ms := range []int64{b.ClientRpcSendTime, b.ClientSettleTime, b.ClientEndTime} {
		if ms > 0 {
			return time.UnixMilli(ms)
		}
	}
	return time.Time{}
}

// composerMeter is Cursor's per-conversation context-window snapshot.
type composerMeter struct {
	tokens    int64
	createdAt time.Time
}

func loadCursorIDE(a *Aggregate) {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	paths := []string{
		filepath.Join(home, "Library", "Application Support", "Cursor", "User", "globalStorage", "state.vscdb"),
		filepath.Join(home, ".config", "Cursor", "User", "globalStorage", "state.vscdb"),
	}
	seen := map[string]bool{}
	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			loadCursorDB(a, p, seen)
		}
	}
}

func loadCursorDB(a *Aggregate, path string, seen map[string]bool) {
	db, cleanup, err := openDB(path)
	if err != nil {
		return
	}
	defer cleanup()

	meters := loadComposerMeters(db, seen, a)

	// Track which composers saw any explicit bubble tokenCount.
	explicit := map[string]bool{}
	// First bubble timestamp + assistant text estimate per composer (for meter path).
	scans := map[string]*compScan{}

	for offset := 0; offset < cursorBudgetRows; offset += cursorBatchRows {
		n := scanCursorBubbles(a, db, seen, meters, explicit, scans, cursorBatchRows, offset)
		if n < cursorBatchRows {
			break
		}
	}

	// Credit composer meters once for conversations without explicit bubble tokens.
	for id, m := range meters {
		if explicit[id] || m.tokens <= 0 {
			continue
		}
		t := m.createdAt
		var out int64
		var model string
		if sc, ok := scans[id]; ok {
			if t.IsZero() {
				t = sc.firstTS
			}
			if sc.asstChars > 0 {
				out = int64(sc.asstChars+3) / 4
			}
			model = sc.model
		}
		if t.IsZero() {
			continue
		}
		in := m.tokens
		tok := in + out
		// Bubbles already counted messages with tok=0; only add tokens here.
		a.InputTokens += in
		a.OutputTokens += out
		a.addTokensOnDay(t, tok)
		a.TokensEstimated = true // meter is a snapshot, not billed cumulative
		if model == "" {
			model = "auto"
		}
		a.addModelTokensOnDay(dayOf(t), model, tok)
	}
}

func loadComposerMeters(db *sql.DB, seen map[string]bool, a *Aggregate) map[string]composerMeter {
	out := map[string]composerMeter{}
	rows, err := db.Query(`SELECT key, value FROM cursorDiskKV WHERE key LIKE 'composerData:%'`)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var key string
		var value []byte
		if rows.Scan(&key, &value) != nil {
			continue
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		a.Sessions++

		id := strings.TrimPrefix(key, "composerData:")
		var meta struct {
			CreatedAt            any `json:"createdAt"`
			PromptTokenBreakdown struct {
				TotalUsedTokens int64 `json:"totalUsedTokens"`
			} `json:"promptTokenBreakdown"`
			ContextTokensUsed int64 `json:"contextTokensUsed"`
		}
		if unmarshalJSON(value, &meta) != nil {
			continue
		}
		tokens := meta.PromptTokenBreakdown.TotalUsedTokens
		if tokens == 0 {
			tokens = meta.ContextTokensUsed
		}
		var created time.Time
		switch v := meta.CreatedAt.(type) {
		case string:
			created = parseTime(v)
		case float64:
			if v > 1e12 {
				created = time.UnixMilli(int64(v))
			} else if v > 0 {
				created = time.Unix(int64(v), 0)
			}
		}
		if tokens > 0 || !created.IsZero() {
			out[id] = composerMeter{tokens: tokens, createdAt: created}
		}
	}
	return out
}

func composerIDFromBubbleKey(key string) string {
	// bubbleId:<composerId>:<bubbleId>
	rest := strings.TrimPrefix(key, "bubbleId:")
	if i := strings.IndexByte(rest, ':'); i >= 0 {
		return rest[:i]
	}
	return rest
}

type compScan struct {
	firstTS   time.Time
	asstChars int
	model     string
}

func scanCursorBubbles(
	a *Aggregate,
	db *sql.DB,
	seen map[string]bool,
	meters map[string]composerMeter,
	explicit map[string]bool,
	scans map[string]*compScan,
	limit, offset int,
) int {
	rows, err := db.Query(`SELECT key, value FROM cursorDiskKV WHERE key LIKE 'bubbleId:%' ORDER BY rowid DESC LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return 0
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		var (
			key   string
			value []byte
		)
		if rows.Scan(&key, &value) != nil {
			continue
		}
		n++
		if strings.ContainsRune(key, '\n') {
			continue
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		var b cursorBubble
		if unmarshalJSON(value, &b) != nil {
			continue
		}
		cid := composerIDFromBubbleKey(key)
		noteCursorBubble(a, &b, cid, meters, explicit, scans)
	}
	return n
}

func noteCursorBubble(
	a *Aggregate,
	b *cursorBubble,
	composerID string,
	meters map[string]composerMeter,
	explicit map[string]bool,
	scans map[string]*compScan,
) {
	if b.Type != 1 && b.Type != 2 {
		return
	}
	t := b.time()
	if t.IsZero() {
		return
	}

	sc := scans[composerID]
	if sc == nil {
		sc = &compScan{}
		scans[composerID] = sc
	}
	if sc.firstTS.IsZero() || t.Before(sc.firstTS) {
		sc.firstTS = t
	}
	if b.Type == 2 {
		sc.asstChars += len(b.Text)
		if m := b.ModelInfo.ModelName; m != "" && m != "default" && sc.model == "" {
			sc.model = m
		}
	}

	in := nonneg(b.TokenCount.InputTokens)
	out := nonneg(b.TokenCount.OutputTokens)

	if in+out > 0 {
		explicit[composerID] = true
		tok := in + out
		a.noteMessage(t, tok)
		a.InputTokens += in
		a.OutputTokens += out
		if b.Type == 2 {
			m := b.ModelInfo.ModelName
			if m == "" || m == "default" {
				m = "auto"
			}
			a.addModelOnDay(dayOf(t), m, tok)
		}
		return
	}

	// Zero bubble tokens: if a composer meter will cover input, only estimate
	// nothing here (meter credited later). Otherwise fall back to chars/4.
	if m, ok := meters[composerID]; ok && m.tokens > 0 {
		// Still count the message for activity, but don't estimate tokens.
		a.noteMessage(t, 0)
		return
	}

	if b.Text != "" {
		est := estTokens(b.Text)
		if est > 0 {
			if b.Type == 1 {
				in = est
			} else {
				out = est
			}
			a.TokensEstimated = true
		}
	}
	tok := in + out
	a.noteMessage(t, tok)
	a.InputTokens += in
	a.OutputTokens += out
	if b.Type == 2 {
		m := b.ModelInfo.ModelName
		if m == "" || m == "default" {
			m = "auto"
		}
		a.addModelOnDay(dayOf(t), m, tok)
	}
}

func estTokens(text string) int64 {
	return int64(len(text)+3) / 4
}

// ---- shared SQLite helpers ----

func openDB(path string) (*sql.DB, func(), error) {
	uri := "file:" + filepath.ToSlash(path) + "?mode=ro"
	db, err := sql.Open("sqlite", uri)
	if err == nil {
		if err := db.Ping(); err == nil {
			return db, func() { db.Close() }, nil
		}
		db.Close()
	}
	return openDBCopy(path)
}

func openDBCopy(path string) (*sql.DB, func(), error) {
	dir, err := os.MkdirTemp("", "nx-sqlite-")
	if err != nil {
		return nil, nil, err
	}
	rm := func() { os.RemoveAll(dir) }
	dst := filepath.Join(dir, filepath.Base(path))
	if err := copyFile(path, dst); err != nil {
		rm()
		return nil, nil, err
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		if _, err := os.Stat(path + suffix); err == nil {
			_ = copyFile(path+suffix, dst+suffix)
		}
	}
	db, err := sql.Open("sqlite", "file:"+dst+"?mode=ro")
	if err != nil {
		rm()
		return nil, nil, err
	}
	return db, func() { db.Close(); rm() }, nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}
