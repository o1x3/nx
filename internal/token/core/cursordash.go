package core

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// cursorDashBaseURL is overridable in tests (httptest).
var cursorDashBaseURL = "https://cursor.com"

// cursorDashHTTP is overridable in tests.
var cursorDashHTTP = http.DefaultClient

// cursorDashTTL is how long a successful dashboard fetch is reused.
const cursorDashTTL = 15 * time.Minute

// cursorDashPageSize and cursorDashMaxPages bound event pagination.
const (
	cursorDashPageSize = 1000
	cursorDashMaxPages = 1000
)

// cursorDashUsage is the token rollup pulled from Cursor's dashboard API.
type cursorDashUsage struct {
	InputTokens      int64
	OutputTokens     int64
	CacheReadTokens  int64
	CacheWriteTokens int64
	ByDayTokens      map[string]int64
	ByDayModelTok    map[string]map[string]int64
	First            time.Time
	Last             time.Time
}

func cursorDashDisabled() bool {
	switch strings.ToLower(os.Getenv("NX_TOKEN_CURSOR_LOCAL")) {
	case "1", "true", "yes":
		return true
	default:
		return false
	}
}

// applyCursorDashboard replaces a's token ledgers with Cursor dashboard
// billed counts when a session is available. Sessions/messages stay local.
// Soft-fails (no auth, network, API error) leave a unchanged.
func applyCursorDashboard(a *Aggregate) {
	if a == nil || cursorDashDisabled() {
		return
	}
	sess, ok := resolveCursorSession()
	if !ok {
		return
	}
	usage, err := fetchCursorDashboard(sess)
	if err != nil || usage == nil {
		return
	}
	applyCursorDashUsage(a, usage)
}

func applyCursorDashUsage(a *Aggregate, u *cursorDashUsage) {
	a.InputTokens = u.InputTokens
	a.OutputTokens = u.OutputTokens
	a.CacheReadTokens = u.CacheReadTokens
	a.CacheWriteTokens = u.CacheWriteTokens
	a.TokensEstimated = false
	a.ByDayTokens = u.ByDayTokens
	if a.ByDayTokens == nil {
		a.ByDayTokens = map[string]int64{}
	}
	a.ByDayModelTok = u.ByDayModelTok
	if a.ByDayModelTok == nil {
		a.ByDayModelTok = map[string]map[string]int64{}
	}
	if !u.First.IsZero() && (a.First.IsZero() || u.First.Before(a.First)) {
		a.First = u.First
	}
	if u.Last.After(a.Last) {
		a.Last = u.Last
	}
}

func fetchCursorDashboard(sess cursorSession) (*cursorDashUsage, error) {
	if cached, ok := readCursorDashCache(sess.Sub); ok {
		return cached, nil
	}
	client := &cursorDashClient{sess: sess}
	me, err := client.me()
	if err != nil {
		return nil, err
	}
	events, err := client.allEvents(me.ID, 0, time.Now().UnixMilli()+int64(24*time.Hour/time.Millisecond))
	if err != nil {
		return nil, err
	}
	usage := rollupCursorEvents(events)
	_ = writeCursorDashCache(sess.Sub, usage)
	return usage, nil
}

type cursorDashClient struct {
	sess cursorSession
}

type cursorMe struct {
	ID    int64  `json:"id"`
	Email string `json:"email"`
	Sub   string `json:"sub"`
}

func (c *cursorDashClient) me() (cursorMe, error) {
	var out cursorMe
	if err := c.get("/api/auth/me", &out); err != nil {
		return cursorMe{}, err
	}
	if out.ID == 0 {
		return cursorMe{}, fmt.Errorf("cursor dashboard: empty user id")
	}
	return out, nil
}

type cursorUsageEvent struct {
	Timestamp  string `json:"timestamp"`
	Model      string `json:"model"`
	IsHeadless bool   `json:"isHeadless"`
	TokenUsage struct {
		InputTokens      int64 `json:"inputTokens"`
		OutputTokens     int64 `json:"outputTokens"`
		CacheReadTokens  int64 `json:"cacheReadTokens"`
		CacheWriteTokens int64 `json:"cacheWriteTokens"`
	} `json:"tokenUsage"`
}

type cursorEventsPage struct {
	TotalUsageEventsCount int                `json:"totalUsageEventsCount"`
	UsageEventsDisplay    []cursorUsageEvent `json:"usageEventsDisplay"`
}

func (c *cursorDashClient) allEvents(userID, startMS, endMS int64) ([]cursorUsageEvent, error) {
	var all []cursorUsageEvent
	total := -1
	for page := 1; page <= cursorDashMaxPages; page++ {
		var resp cursorEventsPage
		body := map[string]any{
			"teamId":    0,
			"startDate": strconv.FormatInt(startMS, 10),
			"endDate":   strconv.FormatInt(endMS, 10),
			"userId":    userID,
			"page":      page,
			"pageSize":  cursorDashPageSize,
		}
		if err := c.post("/api/dashboard/get-filtered-usage-events", body, &resp); err != nil {
			return nil, err
		}
		if total < 0 {
			total = resp.TotalUsageEventsCount
		}
		if len(resp.UsageEventsDisplay) == 0 {
			break
		}
		all = append(all, resp.UsageEventsDisplay...)
		if total > 0 && len(all) >= total {
			break
		}
	}
	return all, nil
}

func (c *cursorDashClient) get(path string, out any) error {
	return c.do(http.MethodGet, path, nil, out)
}

func (c *cursorDashClient) post(path string, body any, out any) error {
	return c.do(http.MethodPost, path, body, out)
}

func (c *cursorDashClient) do(method, path string, body any, out any) error {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, cursorDashBaseURL+path, rdr)
	if err != nil {
		return err
	}
	req.Header.Set("Cookie", "WorkosCursorSessionToken="+c.sess.cookieValue())
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "nx-token/cursor-dashboard")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", "https://cursor.com")
	}
	resp, err := cursorDashHTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("cursor dashboard %s: HTTP %d", path, resp.StatusCode)
	}
	if len(raw) == 0 {
		return nil
	}
	return json.Unmarshal(raw, out)
}

func rollupCursorEvents(events []cursorUsageEvent) *cursorDashUsage {
	u := &cursorDashUsage{
		ByDayTokens:   map[string]int64{},
		ByDayModelTok: map[string]map[string]int64{},
	}
	for _, ev := range events {
		in := nonneg(ev.TokenUsage.InputTokens)
		out := nonneg(ev.TokenUsage.OutputTokens)
		cr := nonneg(ev.TokenUsage.CacheReadTokens)
		cw := nonneg(ev.TokenUsage.CacheWriteTokens)
		tok := in + out + cr + cw
		u.InputTokens += in
		u.OutputTokens += out
		u.CacheReadTokens += cr
		u.CacheWriteTokens += cw

		ms, _ := strconv.ParseInt(ev.Timestamp, 10, 64)
		if ms <= 0 {
			continue
		}
		t := time.UnixMilli(ms).Local()
		day := t.Format("2006-01-02")
		if tok > 0 {
			u.ByDayTokens[day] += tok
		}
		model := strings.TrimSpace(ev.Model)
		if model == "" {
			model = "auto"
		}
		if tok > 0 {
			if u.ByDayModelTok[day] == nil {
				u.ByDayModelTok[day] = map[string]int64{}
			}
			u.ByDayModelTok[day][model] += tok
		}
		if u.First.IsZero() || t.Before(u.First) {
			u.First = t
		}
		if t.After(u.Last) {
			u.Last = t
		}
	}
	return u
}

// ---- short-TTL dashboard cache ----

type cursorDashCacheFile struct {
	Sub       string          `json:"sub"`
	FetchedAt time.Time       `json:"fetched_at"`
	Usage     cursorDashUsage `json:"usage"`
}

func cursorDashCachePath(sub string) string {
	dir := tokenCacheDir()
	if dir == "" {
		return ""
	}
	// Sanitize sub for a filename.
	safe := strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			return r
		}
		return '_'
	}, sub)
	return filepath.Join(dir, "cursor-dash-"+safe+".json")
}

func readCursorDashCache(sub string) (*cursorDashUsage, bool) {
	if cacheDisabled() {
		return nil, false
	}
	path := cursorDashCachePath(sub)
	if path == "" {
		return nil, false
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	var f cursorDashCacheFile
	if json.Unmarshal(raw, &f) != nil || f.Sub != sub {
		return nil, false
	}
	if time.Since(f.FetchedAt) > cursorDashTTL {
		return nil, false
	}
	u := f.Usage
	if u.ByDayTokens == nil {
		u.ByDayTokens = map[string]int64{}
	}
	if u.ByDayModelTok == nil {
		u.ByDayModelTok = map[string]map[string]int64{}
	}
	return &u, true
}

func writeCursorDashCache(sub string, u *cursorDashUsage) error {
	if cacheDisabled() || u == nil {
		return nil
	}
	path := cursorDashCachePath(sub)
	if path == "" {
		return nil
	}
	dir := tokenCacheDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	f := cursorDashCacheFile{Sub: sub, FetchedAt: time.Now(), Usage: *u}
	raw, err := json.Marshal(f)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "cursor-dash-*.json")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	ok := false
	defer func() {
		tmp.Close()
		if !ok {
			os.Remove(tmpPath)
		}
	}()
	if _, err := tmp.Write(raw); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	ok = true
	return os.Rename(tmpPath, path)
}
