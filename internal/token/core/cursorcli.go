package core

import (
	"bytes"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
	"time"
)

// ---- Cursor CLI (cursor-agent): ~/.cursor/chats/<hash>/<id>/store.db ----

// cursorCLIMeta is the decoded meta row of a store.db (hex-encoded JSON).
type cursorCLIMeta struct {
	AgentID       string `json:"agentId"`
	Name          string `json:"name"`
	CreatedAt     int64  `json:"createdAt"` // epoch ms (seconds on some builds)
	LastUsedModel string `json:"lastUsedModel"`
	Mode          string `json:"mode"`
}

// cursorCLIBlob is one message row in the blobs table. Content is a plain
// string, an array of typed blocks, or a nested object wrapping the same.
type cursorCLIBlob struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

func loadCursorCLI(a *Aggregate) {
	files := homeGlob(".cursor/chats/*/*/store.db")
	a.Sessions += len(files)
	for _, f := range files {
		loadCursorCLIStore(a, f)
	}
}

func loadCursorCLIStore(a *Aggregate, path string) {
	db, cleanup, err := openDBCopy(path)
	if err != nil {
		return
	}
	defer cleanup()

	// The CLI stores no per-message timestamps, so every message is attributed
	// to the session's creation day (file mtime when meta is unreadable).
	meta := cursorCLIReadMeta(db)
	t := time.Time{}
	if ms := meta.CreatedAt; ms > 0 {
		if ms < 1e12 {
			ms *= 1000 // epoch seconds on some builds
		}
		t = time.UnixMilli(ms)
	}
	if t.IsZero() {
		if fi, err := os.Stat(path); err == nil {
			t = fi.ModTime()
		}
	}
	model := meta.LastUsedModel
	if model == "" {
		model = "auto"
	}

	rows, err := db.Query(`SELECT data FROM blobs`)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var data []byte
		if rows.Scan(&data) != nil {
			continue
		}
		if !bytes.HasPrefix(data, []byte(`{"`)) {
			continue // non-message blob (binary payloads share the table)
		}
		var m cursorCLIBlob
		if json.Unmarshal(data, &m) != nil {
			continue
		}
		if m.Role != "user" && m.Role != "assistant" {
			continue
		}
		// The CLI never records usage, so tokens are always estimated.
		tok := estTokens(cursorCLIText(m.Content))
		a.noteMessage(t, tok)
		if tok > 0 {
			a.TokensEstimated = true
			if m.Role == "user" {
				a.InputTokens += tok
			} else {
				a.OutputTokens += tok
			}
		}
		if m.Role == "assistant" {
			a.addModelOnDay(dayOf(t), model, tok)
		}
	}
}

// cursorCLIReadMeta finds and decodes the hex-encoded JSON meta row.
func cursorCLIReadMeta(db *sql.DB) cursorCLIMeta {
	rows, err := db.Query(`SELECT value FROM meta`)
	if err != nil {
		return cursorCLIMeta{}
	}
	defer rows.Close()
	for rows.Next() {
		var v []byte
		if rows.Scan(&v) != nil {
			continue
		}
		raw, err := hex.DecodeString(strings.TrimSpace(string(v)))
		if err != nil {
			continue
		}
		var m cursorCLIMeta
		if json.Unmarshal(raw, &m) == nil {
			return m
		}
	}
	return cursorCLIMeta{}
}

// cursorCLIText extracts the concatenated text of a message content value.
func cursorCLIText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &blocks) == nil {
		var sb strings.Builder
		for _, b := range blocks {
			if b.Type == "text" {
				sb.WriteString(b.Text)
			}
		}
		return sb.String()
	}
	// Nested object: unwrap its inner content and recurse.
	var obj struct {
		Content json.RawMessage `json:"content"`
	}
	if json.Unmarshal(raw, &obj) == nil && len(obj.Content) > 0 {
		return cursorCLIText(obj.Content)
	}
	return ""
}
