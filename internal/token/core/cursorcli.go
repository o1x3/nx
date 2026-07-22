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

func loadCursorCLIStore(path string) *Aggregate {
	a := newAggregate(Cursor)
	db, cleanup, err := openDB(path)
	if err != nil {
		return nil
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
		return nil
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
		if unmarshalJSON(data, &m) != nil {
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
	a.Sessions = 1
	return a
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
		if unmarshalJSON(raw, &m) == nil {
			return m
		}
	}
	return cursorCLIMeta{}
}

// cursorCLIText extracts estimable text from a message content value.
// Counts text, reasoning, and tool-call/result payloads so offline chars/4
// estimates are less starved than text-only (still far below billed totals).
func cursorCLIText(raw json.RawMessage) string {
	var s string
	if unmarshalJSON(raw, &s) == nil {
		return s
	}
	var blocks []struct {
		Type    string          `json:"type"`
		Text    string          `json:"text"`
		Name    string          `json:"name"`
		Args    json.RawMessage `json:"args"`
		Input   json.RawMessage `json:"input"`
		Result  json.RawMessage `json:"result"`
		Content json.RawMessage `json:"content"`
	}
	if unmarshalJSON(raw, &blocks) == nil {
		var sb strings.Builder
		for _, b := range blocks {
			switch strings.ToLower(b.Type) {
			case "text", "reasoning", "redacted-reasoning", "thinking":
				sb.WriteString(b.Text)
			case "tool-call", "tool_call", "tool-use", "tool_use":
				sb.WriteString(b.Name)
				sb.Write(b.Args)
				sb.Write(b.Input)
			case "tool-result", "tool_result":
				if len(b.Result) > 0 {
					sb.Write(b.Result)
				} else if len(b.Content) > 0 {
					sb.WriteString(cursorCLIText(b.Content))
				} else {
					sb.WriteString(b.Text)
				}
			default:
				sb.WriteString(b.Text)
			}
		}
		return sb.String()
	}
	// Nested object: unwrap its inner content and recurse.
	var obj struct {
		Content json.RawMessage `json:"content"`
	}
	if unmarshalJSON(raw, &obj) == nil && len(obj.Content) > 0 {
		return cursorCLIText(obj.Content)
	}
	return ""
}
