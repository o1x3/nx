package core

import (
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// makeCursorCLIStore builds a fixture store.db: a hex-encoded JSON meta row
// plus the given raw blob payloads.
func makeCursorCLIStore(t *testing.T, path, model string, createdAtMs int64, blobs ...string) {
	t.Helper()
	meta := fmt.Sprintf(`{"agentId":"a-1","name":"test","createdAt":%d,"lastUsedModel":%q,"mode":"agent"}`, createdAtMs, model)
	stmts := [][]any{
		{`CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT)`},
		{`CREATE TABLE blobs (id TEXT PRIMARY KEY, data BLOB)`},
		{`INSERT INTO meta (key, value) VALUES ('0', ?)`, hex.EncodeToString([]byte(meta))},
	}
	for i, b := range blobs {
		stmts = append(stmts, []any{`INSERT INTO blobs (id, data) VALUES (?, ?)`, fmt.Sprintf("b%d", i), []byte(b)})
	}
	makeSQLiteDB(t, path, stmts...)
}

// TestLoadCursorCLI covers the store.db parser: hex meta decoding, string,
// block-array and nested-object content shapes, non-JSON blob skipping, day
// attribution from meta.createdAt and always-estimated tokens.
func TestLoadCursorCLI(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	created := time.Date(2026, 6, 18, 9, 0, 0, 0, time.Local)
	makeCursorCLIStore(t, filepath.Join(home, ".cursor", "chats", "h1", "s1", "store.db"), "gpt-5.2", created.UnixMilli(),
		`{"role":"user","content":"hello there, please fix"}`,                                                // 23 bytes => 6 estimated input tokens
		`{"role":"assistant","content":[{"type":"text","text":"done, the fix is in"},{"type":"tool_call"}]}`, // 19 bytes => 5 estimated output tokens
		`{"role":"assistant","content":{"content":[{"type":"text","text":"nested reply here"}]}}`,            // nested object, 17 bytes => 5 estimated output tokens
		`{"role":"user","content":{}}`, // empty nested object => 0 tokens, still a counted user message
		"\x00\x01binary garbage",       // non-JSON blob, skipped
	)

	a := newAggregate(Cursor)
	loadCursorCLI(a)

	if a.Sessions != 1 {
		t.Errorf("Sessions = %d, want 1 (one store.db)", a.Sessions)
	}
	if a.Messages != 4 {
		t.Errorf("Messages = %d, want 4 (binary blob skipped, empty-content user counted)", a.Messages)
	}
	if a.InputTokens != 6 || a.OutputTokens != 10 {
		t.Errorf("tokens = %d/%d, want 6/10 (bytes/4 estimates)", a.InputTokens, a.OutputTokens)
	}
	if !a.TokensEstimated {
		t.Error("TokensEstimated = false, want true (CLI tokens are always estimated)")
	}

	day := created.Format("2006-01-02")
	if a.ByDayMsgs[day] != 4 {
		t.Errorf("ByDayMsgs[%s] = %d, want 4 (all messages on the session day)", day, a.ByDayMsgs[day])
	}
	if a.ByDayTokens[day] != 16 {
		t.Errorf("ByDayTokens[%s] = %d, want 16", day, a.ByDayTokens[day])
	}

	models := a.TopModels()
	if len(models) != 1 || models[0].ID != "gpt-5.2" || models[0].Name != "GPT-5.2" || models[0].Tokens != 10 || models[0].Messages != 2 {
		t.Errorf("TopModels = %+v, want single gpt-5.2 with 10 tokens / 2 messages", models)
	}
}

// TestLoadCursorCLISecondsAndMtime: a createdAt in epoch seconds is
// normalized, and a store without usable meta falls back to file mtime.
func TestLoadCursorCLISecondsAndMtime(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	created := time.Date(2026, 6, 17, 22, 0, 0, 0, time.Local)
	makeCursorCLIStore(t, filepath.Join(home, ".cursor", "chats", "h1", "s1", "store.db"), "", created.Unix(), // seconds, not ms
		`{"role":"user","content":"hiya"}`)

	touched := time.Date(2026, 6, 15, 8, 0, 0, 0, time.Local)
	s2 := filepath.Join(home, ".cursor", "chats", "h1", "s2", "store.db")
	makeCursorCLIStore(t, s2, "", 0, // no createdAt => fall back to file mtime
		`{"role":"assistant","content":"sure"}`)
	if err := os.Chtimes(s2, touched, touched); err != nil {
		t.Fatal(err)
	}

	a := newAggregate(Cursor)
	loadCursorCLI(a)

	if a.Sessions != 2 {
		t.Errorf("Sessions = %d, want 2", a.Sessions)
	}
	day := created.Format("2006-01-02")
	if a.ByDayMsgs[day] != 1 {
		t.Errorf("ByDayMsgs[%s] = %d, want 1 (epoch-seconds createdAt normalized)", day, a.ByDayMsgs[day])
	}
	mday := touched.Format("2006-01-02")
	if a.ByDayMsgs[mday] != 1 {
		t.Errorf("ByDayMsgs[%s] = %d, want 1 (mtime fallback)", mday, a.ByDayMsgs[mday])
	}
	if a.InputTokens != 1 { // "hiya" => ceil(4/4)
		t.Errorf("InputTokens = %d, want 1", a.InputTokens)
	}
	if m := a.TopModels(); len(m) != 1 || m[0].ID != "auto" || m[0].Name != "Auto" {
		t.Errorf("TopModels = %+v, want single Auto (missing lastUsedModel)", m)
	}
}
