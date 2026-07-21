package core

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// makeSQLiteDB creates a fixture database at path (creating parent dirs) and
// runs each statement in order. Statements may carry args as {sql, args...}.
func makeSQLiteDB(t *testing.T, path string, stmts ...[]any) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, s := range stmts {
		if _, err := db.Exec(s[0].(string), s[1:]...); err != nil {
			t.Fatalf("exec %q: %v", s[0], err)
		}
	}
}

// cursorStatePath returns the macOS-style state.vscdb location under home.
func cursorStatePath(home string) string {
	return filepath.Join(home, "Library", "Application Support", "Cursor", "User", "globalStorage", "state.vscdb")
}

// TestLoadCursorIDE covers the state.vscdb parser: session counting from
// composerData rows, real vs estimated token counts, the legacy epoch-ms
// timestamp fallback, newline-key and malformed-row skipping, model naming
// and day bucketing.
func TestLoadCursorIDE(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	day1 := time.Date(2026, 6, 20, 10, 0, 0, 0, time.Local)
	day2 := time.Date(2026, 6, 21, 9, 30, 0, 0, time.Local)

	kv := func(key, value string) []any {
		return []any{`INSERT INTO cursorDiskKV (key, value) VALUES (?, ?)`, key, value}
	}
	asst1 := fmt.Sprintf(`{"type":2,"text":"hi","createdAt":%q,"tokenCount":{"inputTokens":100,"outputTokens":50},"modelInfo":{"modelName":"claude-4.5-sonnet"}}`,
		day1.Format(time.RFC3339))
	asst2 := fmt.Sprintf(`{"type":2,"text":%q,"createdAt":%q,"tokenCount":{"inputTokens":0,"outputTokens":0},"modelInfo":{"modelName":"default"}}`,
		strings.Repeat("x", 40), day1.Format(time.RFC3339)) // no usage => estimate 10 output tokens
	user := fmt.Sprintf(`{"type":1,"text":%q,"clientRpcSendTime":%d}`,
		strings.Repeat("y", 20), day2.UnixMilli()) // legacy timestamp => estimate 5 input tokens

	makeSQLiteDB(t, cursorStatePath(home),
		[]any{`CREATE TABLE cursorDiskKV (key TEXT PRIMARY KEY, value BLOB)`},
		kv("composerData:c1", `{}`),
		kv("composerData:c2", `{}`),
		kv("bubbleId:c1:a1", asst1),
		kv("bubbleId:c1:a2", asst2),
		kv("bubbleId:c2:u1", user),
		kv("bubbleId:c1:tool\nsub", asst1), // newline key => tool-call row, skipped
		kv("bubbleId:c1:bad", `{not json`), // malformed => skipped
	)

	a := loadCursor()

	if a.Sessions != 2 {
		t.Errorf("Sessions = %d, want 2 (composerData rows)", a.Sessions)
	}
	if a.Messages != 3 {
		t.Errorf("Messages = %d, want 3 (newline key + malformed row skipped)", a.Messages)
	}
	if a.InputTokens != 105 {
		t.Errorf("InputTokens = %d, want 105 (100 real + 5 estimated)", a.InputTokens)
	}
	if a.OutputTokens != 60 {
		t.Errorf("OutputTokens = %d, want 60 (50 real + 10 estimated)", a.OutputTokens)
	}
	if a.CacheReadTokens != 0 || a.CacheWriteTokens != 0 {
		t.Errorf("cache tokens = %d/%d, want 0/0 (cursor has no cache)", a.CacheReadTokens, a.CacheWriteTokens)
	}
	if !a.TokensEstimated {
		t.Error("TokensEstimated = false, want true (two bubbles lacked usage)")
	}

	d1, d2 := day1.Format("2006-01-02"), day2.Format("2006-01-02")
	if a.ByDayMsgs[d1] != 2 || a.ByDayMsgs[d2] != 1 {
		t.Errorf("day msgs = %d/%d, want 2/1", a.ByDayMsgs[d1], a.ByDayMsgs[d2])
	}
	if a.ByDayTokens[d1] != 160 || a.ByDayTokens[d2] != 5 {
		t.Errorf("day tokens = %d/%d, want 160/5", a.ByDayTokens[d1], a.ByDayTokens[d2])
	}

	models := a.TopModels()
	if len(models) != 2 {
		t.Fatalf("TopModels = %+v, want 2 entries", models)
	}
	if models[0].ID != "claude-4.5-sonnet" || models[0].Name != "Sonnet 4.5" || models[0].Tokens != 150 {
		t.Errorf("models[0] = %+v, want Sonnet 4.5 with 150 tokens", models[0])
	}
	if models[1].ID != "auto" || models[1].Name != "Auto" || models[1].Tokens != 10 {
		t.Errorf("models[1] = %+v, want Auto with 10 estimated tokens", models[1])
	}
}

// TestLoadCursorIDEDuplicatePaths: when the same state.vscdb contents are
// visible at both probed install locations (macOS and Linux style), sessions
// and messages are deduped by row key rather than double-counted.
func TestLoadCursorIDEDuplicatePaths(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	ts := time.Date(2026, 6, 20, 10, 0, 0, 0, time.Local).Format(time.RFC3339)
	asst := fmt.Sprintf(`{"type":2,"text":"hi","createdAt":%q,"tokenCount":{"inputTokens":100,"outputTokens":50},"modelInfo":{"modelName":"claude-4.5-sonnet"}}`, ts)
	user := fmt.Sprintf(`{"type":1,"text":"hello there!","createdAt":%q}`, ts) // 12 bytes => 3 estimated input tokens

	paths := []string{
		cursorStatePath(home), // macOS style
		filepath.Join(home, ".config", "Cursor", "User", "globalStorage", "state.vscdb"), // Linux style
	}
	for _, p := range paths {
		makeSQLiteDB(t, p,
			[]any{`CREATE TABLE cursorDiskKV (key TEXT PRIMARY KEY, value BLOB)`},
			[]any{`INSERT INTO cursorDiskKV (key, value) VALUES (?, ?)`, "composerData:c1", `{}`},
			[]any{`INSERT INTO cursorDiskKV (key, value) VALUES (?, ?)`, "bubbleId:c1:a1", asst},
			[]any{`INSERT INTO cursorDiskKV (key, value) VALUES (?, ?)`, "bubbleId:c1:u1", user},
		)
	}

	a := loadCursor()

	if a.Sessions != 1 {
		t.Errorf("Sessions = %d, want 1 (composerData row deduped across paths)", a.Sessions)
	}
	if a.Messages != 2 {
		t.Errorf("Messages = %d, want 2 (bubbles deduped across paths)", a.Messages)
	}
	if a.InputTokens != 103 || a.OutputTokens != 50 {
		t.Errorf("tokens = %d/%d, want 103/50 (not doubled)", a.InputTokens, a.OutputTokens)
	}
}

// TestLoadCursorIDEExactTokens: when every bubble carries a real tokenCount,
// nothing is estimated and the flag stays false.
func TestLoadCursorIDEExactTokens(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	ts := time.Date(2026, 6, 20, 10, 0, 0, 0, time.Local).Format(time.RFC3339)
	makeSQLiteDB(t, cursorStatePath(home),
		[]any{`CREATE TABLE cursorDiskKV (key TEXT PRIMARY KEY, value BLOB)`},
		[]any{`INSERT INTO cursorDiskKV (key, value) VALUES (?, ?)`, "composerData:c1", `{}`},
		[]any{`INSERT INTO cursorDiskKV (key, value) VALUES (?, ?)`, "bubbleId:c1:a1",
			fmt.Sprintf(`{"type":2,"text":"hello","createdAt":%q,"tokenCount":{"inputTokens":30,"outputTokens":20},"modelInfo":{"modelName":"gpt-5.2"}}`, ts)},
		[]any{`INSERT INTO cursorDiskKV (key, value) VALUES (?, ?)`, "bubbleId:c1:a2",
			fmt.Sprintf(`{"type":2,"text":"world","createdAt":%q,"tokenCount":{"inputTokens":10,"outputTokens":5},"modelInfo":{"modelName":"gpt-5.2"}}`, ts)},
	)

	a := loadCursor()
	if a.TokensEstimated {
		t.Error("TokensEstimated = true, want false (all bubbles had real usage)")
	}
	if a.InputTokens != 40 || a.OutputTokens != 25 {
		t.Errorf("tokens = %d/%d, want 40/25", a.InputTokens, a.OutputTokens)
	}
	if m := a.TopModels(); len(m) != 1 || m[0].Name != "GPT-5.2" || m[0].Messages != 2 {
		t.Errorf("TopModels = %+v, want single GPT-5.2 with 2 messages", m)
	}
}

// TestLoadCursorCombined: the cursor harness merges the IDE database and the
// CLI stores into one aggregate via the public Load API.
func TestLoadCursorCombined(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	ts := time.Date(2026, 6, 20, 10, 0, 0, 0, time.Local)
	makeSQLiteDB(t, cursorStatePath(home),
		[]any{`CREATE TABLE cursorDiskKV (key TEXT PRIMARY KEY, value BLOB)`},
		[]any{`INSERT INTO cursorDiskKV (key, value) VALUES (?, ?)`, "composerData:c1", `{}`},
		[]any{`INSERT INTO cursorDiskKV (key, value) VALUES (?, ?)`, "bubbleId:c1:a1",
			fmt.Sprintf(`{"type":2,"text":"hi","createdAt":%q,"tokenCount":{"inputTokens":100,"outputTokens":50},"modelInfo":{"modelName":"claude-4.5-sonnet"}}`, ts.Format(time.RFC3339))},
	)
	makeCursorCLIStore(t, filepath.Join(home, ".cursor", "chats", "h1", "s1", "store.db"), "gpt-5.2", ts.UnixMilli(),
		`{"role":"user","content":"hello there"}`) // 11 chars => 3 estimated input tokens

	a := Load(Cursor)
	if a.Harness != Cursor {
		t.Errorf("Harness = %q, want %q", a.Harness, Cursor)
	}
	if a.Sessions != 2 {
		t.Errorf("Sessions = %d, want 2 (1 IDE composer + 1 CLI store)", a.Sessions)
	}
	if a.Messages != 2 {
		t.Errorf("Messages = %d, want 2", a.Messages)
	}
	if a.InputTokens != 103 || a.OutputTokens != 50 {
		t.Errorf("tokens = %d/%d, want 103/50", a.InputTokens, a.OutputTokens)
	}
	if !a.TokensEstimated {
		t.Error("TokensEstimated = false, want true (CLI tokens are estimated)")
	}
}

// TestMergeTokensEstimated: merging spreads the estimation taint only when
// the estimated source actually contributed tokens.
func TestMergeTokensEstimated(t *testing.T) {
	est := newAggregate(Cursor)
	est.TokensEstimated = true
	est.InputTokens = 10
	exact := newAggregate(Claude)
	exact.InputTokens = 100

	all := newAggregate(Combined)
	all.Merge(exact)
	all.Merge(est)
	if !all.TokensEstimated {
		t.Error("merge of contributing estimated aggregate must set the flag")
	}

	idle := newAggregate(Cursor)
	idle.TokensEstimated = true // flagged but no tokens at all
	all2 := newAggregate(Combined)
	all2.Merge(idle)
	all2.Merge(exact)
	if all2.TokensEstimated {
		t.Error("idle estimated aggregate must not taint the merge")
	}
}

// TestLoadCursorComposerMeter credits promptTokenBreakdown once when bubbles
// report {0,0}, and skips chars/4 input estimates for that conversation.
func TestLoadCursorComposerMeter(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	ts := time.Date(2026, 6, 20, 10, 0, 0, 0, time.Local)
	meta := fmt.Sprintf(`{"createdAt":%q,"promptTokenBreakdown":{"totalUsedTokens":32000}}`, ts.Format(time.RFC3339))
	user := fmt.Sprintf(`{"type":1,"text":%q,"createdAt":%q,"tokenCount":{"inputTokens":0,"outputTokens":0}}`,
		strings.Repeat("u", 40), ts.Format(time.RFC3339))
	asst := fmt.Sprintf(`{"type":2,"text":%q,"createdAt":%q,"tokenCount":{"inputTokens":0,"outputTokens":0},"modelInfo":{"modelName":"claude-4.5-sonnet"}}`,
		strings.Repeat("a", 20), ts.Format(time.RFC3339))

	makeSQLiteDB(t, cursorStatePath(home),
		[]any{`CREATE TABLE cursorDiskKV (key TEXT PRIMARY KEY, value BLOB)`},
		[]any{`INSERT INTO cursorDiskKV (key, value) VALUES (?, ?)`, "composerData:c1", meta},
		[]any{`INSERT INTO cursorDiskKV (key, value) VALUES (?, ?)`, "bubbleId:c1:u1", user},
		[]any{`INSERT INTO cursorDiskKV (key, value) VALUES (?, ?)`, "bubbleId:c1:a1", asst},
	)

	a := loadCursor()
	if a.InputTokens != 32000 {
		t.Errorf("InputTokens = %d, want 32000 from composer meter", a.InputTokens)
	}
	// Assistant text 20 bytes => 5 estimated output tokens.
	if a.OutputTokens != 5 {
		t.Errorf("OutputTokens = %d, want 5 (assistant text estimate)", a.OutputTokens)
	}
	if !a.TokensEstimated {
		t.Error("TokensEstimated = false, want true (meter path)")
	}
	if a.Messages != 2 {
		t.Errorf("Messages = %d, want 2", a.Messages)
	}
}

// TestLoadCursorMeterDisabledByExplicitBubbles: real bubble tokens win and
// the composer meter is not stacked on top.
func TestLoadCursorMeterDisabledByExplicitBubbles(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	ts := time.Date(2026, 6, 20, 10, 0, 0, 0, time.Local)
	meta := `{"createdAt":"2026-06-20T10:00:00Z","promptTokenBreakdown":{"totalUsedTokens":32000}}`
	asst := fmt.Sprintf(`{"type":2,"text":"hi","createdAt":%q,"tokenCount":{"inputTokens":100,"outputTokens":50},"modelInfo":{"modelName":"gpt-5.2"}}`,
		ts.Format(time.RFC3339))

	makeSQLiteDB(t, cursorStatePath(home),
		[]any{`CREATE TABLE cursorDiskKV (key TEXT PRIMARY KEY, value BLOB)`},
		[]any{`INSERT INTO cursorDiskKV (key, value) VALUES (?, ?)`, "composerData:c1", meta},
		[]any{`INSERT INTO cursorDiskKV (key, value) VALUES (?, ?)`, "bubbleId:c1:a1", asst},
	)

	a := loadCursor()
	if a.InputTokens != 100 || a.OutputTokens != 50 {
		t.Errorf("tokens = %d/%d, want 100/50 (explicit bubbles, no meter stack)", a.InputTokens, a.OutputTokens)
	}
	if a.TokensEstimated {
		t.Error("TokensEstimated = true, want false")
	}
}
