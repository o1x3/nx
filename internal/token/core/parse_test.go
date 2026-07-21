package core

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestLoadClaudeDedup verifies the per-turn deduplication: Claude writes one
// JSONL line per content block of an assistant turn, each repeating the same
// cumulative usage. We must count each turn's usage exactly once, count real
// user prompts, and skip tool_result lines.
func TestLoadClaudeDedup(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".claude", "projects", "p")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	ts := "2026-06-20T10:00:00.000Z"
	asst := func(id string, in, out, cr, cw int) string {
		return fmt.Sprintf(`{"type":"assistant","timestamp":%q,"requestId":"req_%s","message":{"id":"msg_%s","role":"assistant","model":"claude-opus-4-8","usage":{"input_tokens":%d,"output_tokens":%d,"cache_read_input_tokens":%d,"cache_creation_input_tokens":%d}}}`,
			ts, id, id, in, out, cr, cw)
	}
	lines := []string{
		asst("1", 100, 50, 200, 10), // turn 1 — block a
		asst("1", 100, 50, 200, 10), // turn 1 — block b (duplicate)
		asst("1", 100, 50, 200, 10), // turn 1 — block c (duplicate)
		`{"type":"user","timestamp":"` + ts + `","message":{"role":"user","content":"hello"}}`,
		`{"type":"user","timestamp":"` + ts + `","message":{"role":"user","content":[{"type":"tool_result","content":"ok"}]}}`,
		asst("2", 5, 5, 0, 0), // turn 2
	}
	if err := os.WriteFile(filepath.Join(dir, "s.jsonl"), []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	a := loadClaude()

	if a.InputTokens != 105 {
		t.Errorf("InputTokens = %d, want 105 (turn1 100 + turn2 5)", a.InputTokens)
	}
	if a.OutputTokens != 55 {
		t.Errorf("OutputTokens = %d, want 55", a.OutputTokens)
	}
	if a.CacheReadTokens != 200 {
		t.Errorf("CacheReadTokens = %d, want 200", a.CacheReadTokens)
	}
	if a.CacheWriteTokens != 10 {
		t.Errorf("CacheWriteTokens = %d, want 10", a.CacheWriteTokens)
	}
	if a.TotalTokens() != 370 {
		t.Errorf("TotalTokens = %d, want 370 (360 + 10), not the 3x-inflated total", a.TotalTokens())
	}
	if a.Messages != 3 {
		t.Errorf("Messages = %d, want 3 (2 assistant turns + 1 real user; tool_result skipped)", a.Messages)
	}
	if a.Sessions != 1 {
		t.Errorf("Sessions = %d, want 1", a.Sessions)
	}
	models := a.TopModels()
	if len(models) != 1 || models[0].ID != "claude-opus-4-8" || models[0].Tokens != 370 {
		t.Errorf("TopModels = %+v, want single opus-4-8 with 370 tokens", models)
	}
}

// TestLoadEach: the single-pass loader returns every concrete harness plus a
// Combined entry whose totals match LoadAll's merge.
func TestLoadEach(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".claude", "projects", "p")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	line := `{"type":"assistant","timestamp":"2026-06-20T10:00:00.000Z","requestId":"req_1","message":{"id":"msg_1","role":"assistant","model":"claude-opus-4-8","usage":{"input_tokens":100,"output_tokens":50,"cache_read_input_tokens":0,"cache_creation_input_tokens":0}}}`
	if err := os.WriteFile(filepath.Join(dir, "s.jsonl"), []byte(line+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	each := LoadEach()
	for _, h := range append([]string{Combined}, Harnesses...) {
		if each[h] == nil {
			t.Fatalf("LoadEach missing %q", h)
		}
	}

	all := LoadAll()
	got := each[Combined]
	if got.TotalTokens() != all.TotalTokens() || got.Sessions != all.Sessions || got.Messages != all.Messages {
		t.Errorf("LoadEach Combined = %d tok / %d sessions / %d msgs, want LoadAll's %d / %d / %d",
			got.TotalTokens(), got.Sessions, got.Messages, all.TotalTokens(), all.Sessions, all.Messages)
	}
	if got.TotalTokens() != 150 {
		t.Errorf("Combined tokens = %d, want 150 from the claude fixture", got.TotalTokens())
	}
}

// TestScanLinesHugeLine ensures a single oversized line doesn't truncate the
// rest of the file (the old bufio.Scanner cap bug).
func TestScanLinesHugeLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "big.jsonl")
	huge := strings.Repeat("x", 70*1024*1024) // 70MB single line
	content := "first\n" + huge + "\nlast\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	var got []string
	scanLines(path, func(b []byte) {
		if len(b) < 100 { // ignore the huge line's content
			got = append(got, string(b))
		}
	})
	if len(got) != 2 || got[0] != "first" || got[1] != "last" {
		t.Errorf("scanLines dropped lines around a huge line: got %v", got)
	}
}

// TestLoadClaudeFinalChunk keeps the stop_reason chunk's real output tokens
// instead of the first streaming placeholder (ccmetrics semantics).
func TestLoadClaudeFinalChunk(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".claude", "projects", "p")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	ts := "2026-06-20T10:00:00.000Z"
	lines := []string{
		fmt.Sprintf(`{"type":"assistant","timestamp":%q,"requestId":"req_1","message":{"id":"msg_1","role":"assistant","model":"claude-opus-4-8","usage":{"input_tokens":100,"output_tokens":1,"cache_read_input_tokens":0,"cache_creation_input_tokens":0}}}`, ts),
		fmt.Sprintf(`{"type":"assistant","timestamp":%q,"requestId":"req_1","message":{"id":"msg_1","role":"assistant","model":"claude-opus-4-8","stop_reason":"end_turn","usage":{"input_tokens":100,"output_tokens":365,"cache_read_input_tokens":50,"cache_creation_input_tokens":10}}}`, ts),
	}
	if err := os.WriteFile(filepath.Join(dir, "s.jsonl"), []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := loadClaude()
	if a.OutputTokens != 365 {
		t.Errorf("OutputTokens = %d, want 365 (final chunk)", a.OutputTokens)
	}
	if a.CacheReadTokens != 50 || a.CacheWriteTokens != 10 {
		t.Errorf("cache = %d/%d, want 50/10", a.CacheReadTokens, a.CacheWriteTokens)
	}
	if a.Messages != 1 {
		t.Errorf("Messages = %d, want 1", a.Messages)
	}
}

// TestLoadClaudeSubagent includes nested subagents/*.jsonl in totals but not
// as separate top-level sessions.
func TestLoadClaudeSubagent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	proj := filepath.Join(home, ".claude", "projects", "p")
	sub := filepath.Join(proj, "sid", "subagents")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	ts := "2026-06-20T10:00:00.000Z"
	main := fmt.Sprintf(`{"type":"assistant","timestamp":%q,"requestId":"r1","message":{"id":"m1","role":"assistant","model":"claude-opus-4-8","stop_reason":"end_turn","usage":{"input_tokens":10,"output_tokens":5,"cache_read_input_tokens":0,"cache_creation_input_tokens":0}}}`, ts)
	agent := fmt.Sprintf(`{"type":"assistant","timestamp":%q,"requestId":"r2","message":{"id":"m2","role":"assistant","model":"claude-opus-4-8","stop_reason":"end_turn","usage":{"input_tokens":20,"output_tokens":10,"cache_read_input_tokens":0,"cache_creation_input_tokens":0}}}`, ts)
	if err := os.WriteFile(filepath.Join(proj, "main.jsonl"), []byte(main+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "agent-1.jsonl"), []byte(agent+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := loadClaude()
	if a.InputTokens != 30 || a.OutputTokens != 15 {
		t.Errorf("tokens = %d/%d, want 30/15 (main+subagent)", a.InputTokens, a.OutputTokens)
	}
	if a.Sessions != 1 {
		t.Errorf("Sessions = %d, want 1 (subagent file excluded)", a.Sessions)
	}
}

// TestLoadClaudeNestedCacheCreation sums ephemeral 5m/1h cache write tiers.
func TestLoadClaudeNestedCacheCreation(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".config", "claude", "projects", "p")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	line := `{"type":"assistant","timestamp":"2026-06-20T10:00:00.000Z","requestId":"r1","message":{"id":"m1","role":"assistant","model":"claude-opus-4-8","stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1,"cache_read_input_tokens":0,"cache_creation_input_tokens":99,"cache_creation":{"ephemeral_5m_input_tokens":10,"ephemeral_1h_input_tokens":40}}}}`
	if err := os.WriteFile(filepath.Join(dir, "s.jsonl"), []byte(line+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := loadClaude()
	if a.CacheWriteTokens != 50 {
		t.Errorf("CacheWriteTokens = %d, want 50 (5m+1h nested)", a.CacheWriteTokens)
	}
}

// TestLoadCodexLastTokenUsage prefers last_token_usage and skips duplicate
// cumulative snapshots.
func TestLoadCodexLastTokenUsage(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	dir := filepath.Join(home, ".codex", "sessions", "2026", "06", "20")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	lines := []string{
		`{"timestamp":"2026-06-20T10:00:00.000Z","type":"turn_context","payload":{"model":"gpt-5.4"}}`,
		`{"timestamp":"2026-06-20T10:00:01.000Z","type":"event_msg","payload":{"type":"token_count","info":{"last_token_usage":{"input_tokens":100,"cached_input_tokens":40,"output_tokens":10,"reasoning_output_tokens":5,"total_tokens":115},"total_token_usage":{"input_tokens":100,"cached_input_tokens":40,"output_tokens":10,"reasoning_output_tokens":5,"total_tokens":115}}}}`,
		// Duplicate cumulative — must be skipped.
		`{"timestamp":"2026-06-20T10:00:02.000Z","type":"event_msg","payload":{"type":"token_count","info":{"last_token_usage":{"input_tokens":100,"cached_input_tokens":40,"output_tokens":10,"reasoning_output_tokens":5,"total_tokens":115},"total_token_usage":{"input_tokens":100,"cached_input_tokens":40,"output_tokens":10,"reasoning_output_tokens":5,"total_tokens":115}}}}`,
		`{"timestamp":"2026-06-20T10:00:03.000Z","type":"event_msg","payload":{"type":"token_count","info":{"last_token_usage":{"input_tokens":50,"cached_input_tokens":10,"output_tokens":8,"reasoning_output_tokens":2,"total_tokens":60},"total_token_usage":{"input_tokens":150,"cached_input_tokens":50,"output_tokens":18,"reasoning_output_tokens":7,"total_tokens":175}}}}`,
	}
	if err := os.WriteFile(filepath.Join(dir, "rollout.jsonl"), []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := loadCodex()
	// Turn1: uncached 60 + cache 40 + out 15 (10+5) = 115
	// Turn2: uncached 40 + cache 10 + out 10 (8+2) = 60
	if a.InputTokens != 100 {
		t.Errorf("InputTokens = %d, want 100 uncached", a.InputTokens)
	}
	if a.CacheReadTokens != 50 {
		t.Errorf("CacheReadTokens = %d, want 50", a.CacheReadTokens)
	}
	if a.OutputTokens != 25 {
		t.Errorf("OutputTokens = %d, want 25 (incl reasoning)", a.OutputTokens)
	}
}

// TestLoadCodexArchivedDedup prefers sessions/ over archived_sessions/ for the
// same relative path.
func TestLoadCodexArchivedDedup(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	codex := filepath.Join(home, ".codex")
	t.Setenv("CODEX_HOME", codex)
	sess := filepath.Join(codex, "sessions")
	arch := filepath.Join(codex, "archived_sessions")
	if err := os.MkdirAll(sess, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(arch, 0o755); err != nil {
		t.Fatal(err)
	}
	active := `{"timestamp":"2026-06-20T10:00:01.000Z","type":"event_msg","payload":{"type":"token_count","info":{"last_token_usage":{"input_tokens":10,"cached_input_tokens":0,"output_tokens":5,"reasoning_output_tokens":0,"total_tokens":15},"total_token_usage":{"input_tokens":10,"cached_input_tokens":0,"output_tokens":5,"reasoning_output_tokens":0,"total_tokens":15}}}}`
	archived := `{"timestamp":"2026-06-20T10:00:01.000Z","type":"event_msg","payload":{"type":"token_count","info":{"last_token_usage":{"input_tokens":999,"cached_input_tokens":0,"output_tokens":999,"reasoning_output_tokens":0,"total_tokens":1998},"total_token_usage":{"input_tokens":999,"cached_input_tokens":0,"output_tokens":999,"reasoning_output_tokens":0,"total_tokens":1998}}}}`
	if err := os.WriteFile(filepath.Join(sess, "s.jsonl"), []byte(active+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(arch, "s.jsonl"), []byte(archived+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := loadCodex()
	if a.InputTokens != 10 || a.OutputTokens != 5 {
		t.Errorf("tokens = %d/%d, want 10/5 from sessions copy", a.InputTokens, a.OutputTokens)
	}
	if a.Sessions != 1 {
		t.Errorf("Sessions = %d, want 1", a.Sessions)
	}
}

// TestLoadPiAgentDir honors PI_AGENT_DIR overrides.
func TestLoadPiAgentDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	alt := filepath.Join(home, "alt-pi")
	t.Setenv("PI_AGENT_DIR", alt)
	dir := filepath.Join(alt, "proj")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	line := `{"type":"message","timestamp":"2026-06-20T10:00:00.000Z","message":{"role":"assistant","model":"gpt-5","usage":{"input":7,"output":3,"cacheRead":0,"cacheWrite":0}}}`
	if err := os.WriteFile(filepath.Join(dir, "s.jsonl"), []byte(line+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := loadPi()
	if a.InputTokens != 7 || a.OutputTokens != 3 {
		t.Errorf("tokens = %d/%d, want 7/3", a.InputTokens, a.OutputTokens)
	}
}
