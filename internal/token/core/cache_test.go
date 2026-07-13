package core

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStatFingerprintChangesOnEdit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.jsonl")
	if err := os.WriteFile(path, []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fp1 := statFingerprint([]string{path})
	time.Sleep(10 * time.Millisecond)
	if err := os.WriteFile(path, []byte("two\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fp2 := statFingerprint([]string{path})
	if fp1 == fp2 {
		t.Fatal("fingerprint should change when file content changes")
	}
}

func TestLoadCachedHitMiss(t *testing.T) {
	cacheDir := t.TempDir()
	t.Setenv("NX_CACHE_DIR", cacheDir)
	t.Setenv("NX_TOKEN_NO_CACHE", "")

	var calls int
	load := func() *Aggregate {
		calls++
		a := newAggregate(Claude)
		a.Messages = 7
		return a
	}

	a1 := loadCached(Claude, nil, load)
	if calls != 1 || a1.Messages != 7 {
		t.Fatalf("first load: calls=%d msgs=%d, want 1/7", calls, a1.Messages)
	}
	a2 := loadCached(Claude, nil, load)
	if calls != 1 || a2.Messages != 7 {
		t.Fatalf("cache hit: calls=%d msgs=%d, want still 1/7", calls, a2.Messages)
	}

	path := filepath.Join(cacheDir, "token", Claude+".gob")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("cache file missing: %v", err)
	}
}

func TestLoadCachedDisabled(t *testing.T) {
	cacheDir := t.TempDir()
	t.Setenv("NX_CACHE_DIR", cacheDir)
	t.Setenv("NX_TOKEN_NO_CACHE", "1")

	var calls int
	load := func() *Aggregate {
		calls++
		return newAggregate(Codex)
	}
	loadCached(Codex, nil, load)
	loadCached(Codex, nil, load)
	if calls != 2 {
		t.Fatalf("NX_TOKEN_NO_CACHE: calls=%d, want 2", calls)
	}
}

func TestLoadAllParallel(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("NX_TOKEN_NO_CACHE", "1")

	day := time.Date(2026, 6, 20, 10, 0, 0, 0, time.Local).Format(time.RFC3339)
	claudeDir := filepath.Join(home, ".claude", "projects", "p1")
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	claudeLine := `{"type":"assistant","timestamp":"` + day + `","requestId":"r1","message":{"id":"m1","role":"assistant","model":"claude-opus-4-8","usage":{"input_tokens":10,"output_tokens":5,"cache_read_input_tokens":0,"cache_creation_input_tokens":0}}}`
	if err := os.WriteFile(filepath.Join(claudeDir, "s1.jsonl"), []byte(claudeLine+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	makeSQLiteDB(t, cursorStatePath(home),
		[]any{`CREATE TABLE cursorDiskKV (key TEXT PRIMARY KEY, value BLOB)`},
		[]any{`INSERT INTO cursorDiskKV (key, value) VALUES (?, ?)`, "composerData:c1", `{}`},
		[]any{`INSERT INTO cursorDiskKV (key, value) VALUES (?, ?)`, "bubbleId:c1:a1",
			`{"type":2,"text":"hi","createdAt":"` + day + `","tokenCount":{"inputTokens":1,"outputTokens":1},"modelInfo":{"modelName":"auto"}}`},
	)

	a := LoadAll()
	if a.Messages < 2 {
		t.Fatalf("LoadAll messages = %d, want at least 2 from claude+cursor", a.Messages)
	}
	if a.Harness != Combined {
		t.Fatalf("Harness = %q, want %q", a.Harness, Combined)
	}
}
