package core

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func testJWT(sub string) string {
	hdr := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))
	payload := base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf(`{"sub":%q,"type":"session"}`, sub)))
	return hdr + "." + payload + ".sig"
}

func TestParseCursorSessionOverride(t *testing.T) {
	jwt := testJWT("user_01ABC")
	cases := []struct {
		in      string
		wantSub string
		ok      bool
	}{
		{jwt, "user_01ABC", true},
		{"user_01ABC::" + jwt, "user_01ABC", true},
		{"user_01ABC%3A%3A" + jwt, "user_01ABC", true},
		{"", "", false},
		{"not-a-jwt", "", false},
	}
	for _, tc := range cases {
		s, ok := parseCursorSessionOverride(tc.in)
		if ok != tc.ok {
			t.Errorf("parse(%q) ok=%v, want %v", tc.in, ok, tc.ok)
			continue
		}
		if ok && s.Sub != tc.wantSub {
			t.Errorf("parse(%q).Sub = %q, want %q", tc.in, s.Sub, tc.wantSub)
		}
	}
}

func TestReadCursorAccessTokenFromItemTable(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	jwt := testJWT("user_01XYZ")
	makeSQLiteDB(t, cursorStatePath(home),
		[]any{`CREATE TABLE ItemTable (key TEXT PRIMARY KEY, value TEXT)`},
		[]any{`INSERT INTO ItemTable (key, value) VALUES (?, ?)`, "cursorAuth/accessToken", jwt},
	)
	s, ok := readCursorAccessToken()
	if !ok {
		t.Fatal("readCursorAccessToken failed")
	}
	if s.Sub != "user_01XYZ" || s.JWT != jwt {
		t.Errorf("session = %+v, want sub user_01XYZ", s)
	}
	if s.cookieValue() != "user_01XYZ%3A%3A"+jwt {
		t.Errorf("cookieValue = %q", s.cookieValue())
	}
}

func TestApplyCursorDashboard(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("NX_TOKEN_CURSOR_LOCAL", "0")
	t.Setenv("NX_TOKEN_NO_CACHE", "1")
	t.Setenv("NX_CACHE_DIR", filepath.Join(home, "cache"))

	jwt := testJWT("user_01DASH")
	makeSQLiteDB(t, cursorStatePath(home),
		[]any{`CREATE TABLE cursorDiskKV (key TEXT PRIMARY KEY, value BLOB)`},
		[]any{`CREATE TABLE ItemTable (key TEXT PRIMARY KEY, value TEXT)`},
		[]any{`INSERT INTO ItemTable (key, value) VALUES (?, ?)`, "cursorAuth/accessToken", jwt},
		[]any{`INSERT INTO cursorDiskKV (key, value) VALUES (?, ?)`, "composerData:c1", `{}`},
		[]any{`INSERT INTO cursorDiskKV (key, value) VALUES (?, ?)`, "bubbleId:c1:a1",
			fmt.Sprintf(`{"type":2,"text":"hi","createdAt":%q,"tokenCount":{"inputTokens":1,"outputTokens":1},"modelInfo":{"modelName":"gpt-5.5"}}`,
				time.Date(2026, 6, 20, 10, 0, 0, 0, time.Local).Format(time.RFC3339))},
	)

	day := time.Date(2026, 7, 1, 15, 30, 0, 0, time.Local)
	ts := fmt.Sprintf("%d", day.UnixMilli())
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Cookie") == "" {
			t.Error("missing Cookie header")
		}
		switch r.URL.Path {
		case "/api/auth/me":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 42, "email": "u@example.com", "sub": "user_01DASH"})
		case "/api/dashboard/get-filtered-usage-events":
			if r.Header.Get("Origin") != "https://cursor.com" {
				t.Error("POST missing Origin CSRF header")
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"totalUsageEventsCount": 1,
				"usageEventsDisplay": []map[string]any{
					{
						"timestamp": ts,
						"model":     "gpt-5.5",
						"tokenUsage": map[string]any{
							"inputTokens":      1000,
							"outputTokens":     2000,
							"cacheReadTokens":  900_000_000,
							"cacheWriteTokens": 50_000,
						},
					},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	prevURL, prevHTTP := cursorDashBaseURL, cursorDashHTTP
	cursorDashBaseURL = srv.URL
	cursorDashHTTP = srv.Client()
	t.Cleanup(func() {
		cursorDashBaseURL = prevURL
		cursorDashHTTP = prevHTTP
	})

	a := loadCursor()
	if a.Sessions != 1 || a.Messages != 1 {
		t.Errorf("sessions/messages = %d/%d, want local 1/1", a.Sessions, a.Messages)
	}
	if a.InputTokens != 1000 || a.OutputTokens != 2000 {
		t.Errorf("in/out = %d/%d, want 1000/2000 from dashboard", a.InputTokens, a.OutputTokens)
	}
	if a.CacheReadTokens != 900_000_000 || a.CacheWriteTokens != 50_000 {
		t.Errorf("cache = %d/%d, want 900M/50K", a.CacheReadTokens, a.CacheWriteTokens)
	}
	if a.TokensEstimated {
		t.Error("TokensEstimated = true, want false after dashboard enrich")
	}
	wantTok := int64(1000 + 2000 + 900_000_000 + 50_000)
	if a.TotalTokens() != wantTok {
		t.Errorf("TotalTokens = %d, want %d", a.TotalTokens(), wantTok)
	}
	d := day.Format("2006-01-02")
	if a.ByDayTokens[d] != wantTok {
		t.Errorf("ByDayTokens[%s] = %d, want %d", d, a.ByDayTokens[d], wantTok)
	}
	models := a.TopModels()
	if len(models) != 1 || models[0].ID != "gpt-5.5" || models[0].Tokens != wantTok {
		t.Errorf("TopModels = %+v, want gpt-5.5 with %d tokens", models, wantTok)
	}
}

func TestApplyCursorDashboardSoftFail(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("NX_TOKEN_CURSOR_LOCAL", "0")
	t.Setenv("NX_TOKEN_NO_CACHE", "1")
	// No ItemTable token → soft fail, keep local estimates.
	ts := time.Date(2026, 6, 20, 10, 0, 0, 0, time.Local).Format(time.RFC3339)
	makeSQLiteDB(t, cursorStatePath(home),
		[]any{`CREATE TABLE cursorDiskKV (key TEXT PRIMARY KEY, value BLOB)`},
		[]any{`INSERT INTO cursorDiskKV (key, value) VALUES (?, ?)`, "composerData:c1", `{}`},
		[]any{`INSERT INTO cursorDiskKV (key, value) VALUES (?, ?)`, "bubbleId:c1:a1",
			fmt.Sprintf(`{"type":2,"text":%q,"createdAt":%q,"tokenCount":{"inputTokens":0,"outputTokens":0},"modelInfo":{"modelName":"gpt-5.5"}}`,
				"xxxxxxxxxxxxxxxxxxxx", ts)}, // 20 bytes => 5 estimated
	)
	a := loadCursor()
	if !a.TokensEstimated {
		t.Error("TokensEstimated = false, want true (dashboard unavailable)")
	}
	if a.OutputTokens != 5 {
		t.Errorf("OutputTokens = %d, want 5 local estimate", a.OutputTokens)
	}
}

func TestCursorDashLocalOptOut(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("NX_TOKEN_CURSOR_LOCAL", "1")
	t.Setenv("NX_CURSOR_SESSION_TOKEN", testJWT("user_01SKIP"))
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	prevURL := cursorDashBaseURL
	cursorDashBaseURL = srv.URL
	t.Cleanup(func() { cursorDashBaseURL = prevURL })

	a := newAggregate(Cursor)
	a.InputTokens = 7
	a.TokensEstimated = true
	applyCursorDashboard(a)
	if called {
		t.Error("dashboard should not be contacted when NX_TOKEN_CURSOR_LOCAL=1")
	}
	if a.InputTokens != 7 || !a.TokensEstimated {
		t.Errorf("aggregate mutated: in=%d est=%v", a.InputTokens, a.TokensEstimated)
	}
}

func TestRollupCursorEvents(t *testing.T) {
	t1 := time.Date(2026, 7, 2, 8, 0, 0, 0, time.Local)
	t2 := time.Date(2026, 7, 3, 9, 0, 0, 0, time.Local)
	u := rollupCursorEvents([]cursorUsageEvent{
		{
			Timestamp: fmt.Sprintf("%d", t1.UnixMilli()),
			Model:     "gpt-5.5",
			TokenUsage: struct {
				InputTokens      int64 `json:"inputTokens"`
				OutputTokens     int64 `json:"outputTokens"`
				CacheReadTokens  int64 `json:"cacheReadTokens"`
				CacheWriteTokens int64 `json:"cacheWriteTokens"`
			}{10, 20, 100, 5},
		},
		{
			Timestamp: fmt.Sprintf("%d", t2.UnixMilli()),
			Model:     "composer-2",
			TokenUsage: struct {
				InputTokens      int64 `json:"inputTokens"`
				OutputTokens     int64 `json:"outputTokens"`
				CacheReadTokens  int64 `json:"cacheReadTokens"`
				CacheWriteTokens int64 `json:"cacheWriteTokens"`
			}{1, 2, 3, 4},
		},
	})
	if u.InputTokens != 11 || u.OutputTokens != 22 || u.CacheReadTokens != 103 || u.CacheWriteTokens != 9 {
		t.Errorf("ledgers = %d/%d/%d/%d", u.InputTokens, u.OutputTokens, u.CacheReadTokens, u.CacheWriteTokens)
	}
	if u.ByDayTokens[t1.Format("2006-01-02")] != 135 {
		t.Errorf("day1 tokens = %d, want 135", u.ByDayTokens[t1.Format("2006-01-02")])
	}
	if u.ByDayModelTok[t2.Format("2006-01-02")]["composer-2"] != 10 {
		t.Errorf("composer-2 day tokens = %v", u.ByDayModelTok[t2.Format("2006-01-02")])
	}
}

func TestMain(m *testing.M) {
	// Keep ambient Cursor session tokens from poisoning local-parser tests.
	os.Unsetenv("NX_CURSOR_SESSION_TOKEN")
	os.Unsetenv("CURSOR_SESSION_TOKEN")
	os.Exit(m.Run())
}
