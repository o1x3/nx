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
// store.db files (cursorcli.go). Neither records reliable token usage, so
// totals are largely estimated from text length and flagged as such.

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

// Bubble paging bounds: state.vscdb can hold 500k+ bubbles, so we read the
// newest rows in batches and stop after a fixed budget.
const (
	cursorBatchRows  = 25_000
	cursorBudgetRows = 250_000
)

// cursorBubble is one chat message in cursorDiskKV (key bubbleId:<composer>:<uuid>).
type cursorBubble struct {
	Type      int    `json:"type"` // 1 = user, 2 = assistant
	Text      string `json:"text"`
	CreatedAt string `json:"createdAt"` // ISO-8601 on current builds
	// Older rows lack createdAt and carry epoch-millisecond client times.
	ClientRpcSendTime int64 `json:"clientRpcSendTime"`
	ClientSettleTime  int64 `json:"clientSettleTime"`
	ClientEndTime     int64 `json:"clientEndTime"`
	TokenCount        struct {
		InputTokens  int64 `json:"inputTokens"`
		OutputTokens int64 `json:"outputTokens"`
	} `json:"tokenCount"`
	ModelInfo struct {
		ModelName string `json:"modelName"`
	} `json:"modelInfo"`
}

// time resolves the bubble's timestamp, preferring createdAt and falling back
// to the legacy epoch-millisecond client fields.
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

func loadCursorIDE(a *Aggregate) {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	// Check both the macOS and Linux locations regardless of GOOS; a missing
	// path is harmless.
	paths := []string{
		filepath.Join(home, "Library", "Application Support", "Cursor", "User", "globalStorage", "state.vscdb"),
		filepath.Join(home, ".config", "Cursor", "User", "globalStorage", "state.vscdb"),
	}
	// The row key is the dedup identity, shared across paths so a database
	// visible under both install locations is only counted once.
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

	// Each composerData row is one chat session. Dedupe by key through the
	// shared `seen` map (composerData:/bubbleId: prefixes are disjoint) so a
	// database visible under both install locations is only counted once.
	rows, err := db.Query(`SELECT key FROM cursorDiskKV WHERE key LIKE 'composerData:%'`)
	if err == nil {
		for rows.Next() {
			var k string
			if rows.Scan(&k) == nil && !seen[k] {
				seen[k] = true
				a.Sessions++
			}
		}
		rows.Close()
	}

	// Newest bubbles first, in batches, up to the row budget.
	for offset := 0; offset < cursorBudgetRows; offset += cursorBatchRows {
		if scanCursorBubbles(a, db, seen, cursorBatchRows, offset) < cursorBatchRows {
			break
		}
	}
}

// scanCursorBubbles reads one batch of bubble rows and returns how many rows
// the query yielded (fewer than limit ⇒ the table is exhausted).
func scanCursorBubbles(a *Aggregate, db *sql.DB, seen map[string]bool, limit, offset int) int {
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
			continue // tool-call sub-composer row, not a chat message
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		var b cursorBubble
		if unmarshalJSON(value, &b) != nil {
			continue // malformed row; skip silently like the other parsers
		}
		noteCursorBubble(a, &b)
	}
	return n
}

func noteCursorBubble(a *Aggregate, b *cursorBubble) {
	if b.Type != 1 && b.Type != 2 {
		return
	}
	t := b.time()
	if t.IsZero() {
		return
	}
	in := nonneg(b.TokenCount.InputTokens)
	out := nonneg(b.TokenCount.OutputTokens)
	if in+out == 0 && b.Text != "" {
		// Current Cursor builds write tokenCount as {0,0}; estimate ~4 bytes
		// per token from the visible text instead (bytes/4 tracks BPE token
		// counts for non-ASCII text better than runes/4).
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

// estTokens approximates a token count as ceil(len/4) — ~4 bytes per token
// (bytes/4 tracks BPE token counts for non-ASCII text better than runes/4).
func estTokens(text string) int64 {
	return int64(len(text)+3) / 4
}

// ---- shared SQLite helpers ----

// openDB opens an SQLite database read-only. Cursor keeps its databases in WAL
// mode while running; WAL readers don't block writers, so we open the live
// file directly and only fall back to a private copy when that fails (e.g.
// an exclusive lock on some platforms).
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

// openDBCopy copies an SQLite database (plus -wal/-shm siblings when present)
// into a temp directory and opens the copy read-only. Cursor keeps its
// databases open in WAL mode while running, so reading a private copy avoids
// racing the live writer. The returned cleanup closes the db and removes the
// temp directory.
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
