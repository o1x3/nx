package core

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func nonneg(n int64) int64 {
	if n < 0 {
		return 0
	}
	return n
}

// homeGlob expands a glob pattern rooted at the user's home directory.
func homeGlob(pattern string) []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	matches, _ := filepath.Glob(filepath.Join(home, pattern))
	return matches
}

func parseTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t
	}
	return time.Time{}
}

// scanLines streams a JSONL file line by line, calling fn for each non-empty
// line. It uses a bufio.Reader rather than a Scanner so a single huge line
// (e.g. a pasted blob or base64 image in one assistant turn) doesn't silently
// truncate the rest of the file the way Scanner's token-size cap would.
func scanLines(path string, fn func([]byte)) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	r := bufio.NewReaderSize(f, 1<<20)
	for {
		line, err := r.ReadBytes('\n')
		if len(line) > 0 {
			line = bytes.TrimRight(line, "\r\n")
			if len(line) > 0 {
				fn(line)
			}
		}
		if err != nil {
			return // io.EOF or read error: stop after the final line
		}
	}
}

// collectJSONL walks root recursively and returns every *.jsonl file path.
func collectJSONL(root string) []string {
	var out []string
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if strings.HasSuffix(strings.ToLower(d.Name()), ".jsonl") {
			out = append(out, path)
		}
		return nil
	})
	return out
}

func isSubagentPath(path string) bool {
	for _, p := range strings.Split(filepath.ToSlash(path), "/") {
		if p == "subagents" {
			return true
		}
	}
	return false
}

func splitEnvPaths(v string) []string {
	var out []string
	for _, p := range strings.Split(v, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// ---- Claude Code ----

type claudeLine struct {
	Type      string `json:"type"`
	Timestamp string `json:"timestamp"`
	RequestID string `json:"requestId"`
	SessionID string `json:"sessionId"`
	Message   struct {
		ID         string          `json:"id"`
		Role       string          `json:"role"`
		Model      string          `json:"model"`
		StopReason *string         `json:"stop_reason"`
		Content    json.RawMessage `json:"content"`
		Usage      json.RawMessage `json:"usage"`
	} `json:"message"`
}

type claudeUsage struct {
	InputTokens              int64 `json:"input_tokens"`
	OutputTokens             int64 `json:"output_tokens"`
	CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
	CacheCreation            *struct {
		Ephemeral5mInputTokens int64 `json:"ephemeral_5m_input_tokens"`
		Ephemeral1hInputTokens int64 `json:"ephemeral_1h_input_tokens"`
	} `json:"cache_creation"`
}

func (u claudeUsage) cacheWrite() int64 {
	if u.CacheCreation != nil {
		sum := u.CacheCreation.Ephemeral5mInputTokens + u.CacheCreation.Ephemeral1hInputTokens
		if sum > 0 {
			return sum
		}
	}
	return u.CacheCreationInputTokens
}

// isToolResult reports whether a user message's content is a tool_result
// (the synthetic turn Claude Code writes back after a tool call), not a real
// human prompt.
func isToolResult(content json.RawMessage) bool {
	var blocks []struct {
		Type string `json:"type"`
	}
	if unmarshalJSON(content, &blocks) != nil {
		return false // string content => a genuine prompt
	}
	for _, b := range blocks {
		if b.Type == "tool_result" {
			return true
		}
	}
	return false
}

// claudeProjectRoots returns Claude Code projects directories to scan.
// Honors CLAUDE_CONFIG_DIR (comma-separated config roots or …/projects paths),
// then ~/.config/claude and ~/.claude (ccusage dual-root discovery).
func claudeProjectRoots() []string {
	var roots []string
	seen := map[string]bool{}
	add := func(p string) {
		if p == "" || seen[p] {
			return
		}
		if st, err := os.Stat(p); err == nil && st.IsDir() {
			seen[p] = true
			roots = append(roots, p)
		}
	}
	normalize := func(raw string) string {
		p := raw
		if filepath.Base(p) == "projects" {
			return p
		}
		return filepath.Join(p, "projects")
	}
	if env := os.Getenv("CLAUDE_CONFIG_DIR"); env != "" {
		for _, raw := range splitEnvPaths(env) {
			add(normalize(raw))
		}
		if len(roots) > 0 {
			return roots
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	xdg := os.Getenv("XDG_CONFIG_HOME")
	if xdg == "" {
		xdg = filepath.Join(home, ".config")
	}
	add(filepath.Join(xdg, "claude", "projects"))
	add(filepath.Join(home, ".claude", "projects"))
	return roots
}

func claudePaths() []string {
	var files []string
	for _, root := range claudeProjectRoots() {
		files = append(files, collectJSONL(root)...)
	}
	return files
}

func loadClaude() *Aggregate {
	files := claudePaths()
	return loadCached(Claude, files, func() *Aggregate {
		part := loadParts(files, parseClaudeFile)
		a := newAggregate(Claude)
		sessions := 0
		for _, f := range files {
			if !isSubagentPath(f) {
				sessions++
			}
		}
		a.Sessions = sessions
		if part != nil {
			a.Merge(part)
		}
		return a
	})
}

// claudeChunk is one streaming assistant line candidate for a request.
type claudeChunk struct {
	t          time.Time
	model      string
	in, out    int64
	cacheRead  int64
	cacheWrite int64
	hasStop    bool
	line       int
}

func (c claudeChunk) tokens() int64 {
	return c.in + c.out + c.cacheRead + c.cacheWrite
}

// parseClaudeFile reads one session JSONL. Streaming assistant turns repeat
// usage across content-block lines; we dedupe by requestId (else message id)
// and keep the final chunk (stop_reason set, else last line) — ccmetrics style.
func parseClaudeFile(path string) *Aggregate {
	a := newAggregate(Claude)
	best := map[string]claudeChunk{}
	lineNo := 0
	scanLines(path, func(b []byte) {
		var l claudeLine
		if unmarshalJSON(b, &l) != nil {
			return
		}
		t := parseTime(l.Timestamp)
		switch l.Type {
		case "user":
			if isToolResult(l.Message.Content) {
				return
			}
			a.noteMessage(t, 0)
		case "assistant":
			lineNo++
			var u claudeUsage
			if len(l.Message.Usage) > 0 {
				_ = unmarshalJSON(l.Message.Usage, &u)
			}
			cw := u.cacheWrite()
			cand := claudeChunk{
				t:          t,
				model:      l.Message.Model,
				in:         u.InputTokens,
				out:        u.OutputTokens,
				cacheRead:  u.CacheReadInputTokens,
				cacheWrite: cw,
				hasStop:    l.Message.StopReason != nil && *l.Message.StopReason != "",
				line:       lineNo,
			}
			key := l.RequestID
			if key == "" {
				key = l.Message.ID
			}
			if key == "" {
				// No id: count once immediately (can't dedupe).
				applyClaudeChunk(a, cand)
				return
			}
			if prev, ok := best[key]; ok {
				if !shouldReplaceClaudeChunk(prev, cand) {
					return
				}
			}
			best[key] = cand
		}
	})
	for _, c := range best {
		applyClaudeChunk(a, c)
	}
	return a
}

func shouldReplaceClaudeChunk(prev, cand claudeChunk) bool {
	if cand.hasStop && !prev.hasStop {
		return true
	}
	if prev.hasStop && !cand.hasStop {
		return false
	}
	return cand.line >= prev.line
}

func applyClaudeChunk(a *Aggregate, c claudeChunk) {
	tok := c.tokens()
	a.noteMessage(c.t, tok)
	a.InputTokens += c.in
	a.OutputTokens += c.out
	a.CacheReadTokens += c.cacheRead
	a.CacheWriteTokens += c.cacheWrite
	if m := c.model; m != "" && m != "<synthetic>" {
		a.addModelOnDay(dayOf(c.t), m, tok)
	}
}

// ---- Codex ----

type codexLine struct {
	Timestamp string          `json:"timestamp"`
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload"`
}

type codexTokenUsage struct {
	InputTokens         int64 `json:"input_tokens"`
	CachedInputTokens   int64 `json:"cached_input_tokens"`
	OutputTokens        int64 `json:"output_tokens"`
	ReasoningOutputToks int64 `json:"reasoning_output_tokens"`
	TotalTokens         int64 `json:"total_tokens"`
}

type codexEventMsg struct {
	Type  string `json:"type"`
	Model string `json:"model"`
	Info  *struct {
		TotalTokenUsage *codexTokenUsage `json:"total_token_usage"`
		LastTokenUsage  *codexTokenUsage `json:"last_token_usage"`
	} `json:"info"`
}

// codexHomes returns Codex home directories (CODEX_HOME comma list, else ~/.codex).
func codexHomes() []string {
	if env := os.Getenv("CODEX_HOME"); env != "" {
		return splitEnvPaths(env)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	return []string{filepath.Join(home, ".codex")}
}

// codexPaths discovers session JSONL under sessions/ and archived_sessions/.
// When the same relative path exists in both, the active sessions/ copy wins
// (ccusage/codeburn behaviour).
func codexPaths() []string {
	type scoped struct {
		home string
		rel  string
		path string
	}
	var ordered []scoped
	seen := map[string]bool{} // home\0rel
	addDir := func(home, sub string) {
		dir := filepath.Join(home, sub)
		st, err := os.Stat(dir)
		if err != nil || !st.IsDir() {
			return
		}
		for _, f := range collectJSONL(dir) {
			rel, err := filepath.Rel(dir, f)
			if err != nil {
				rel = filepath.Base(f)
			}
			key := home + "\x00" + filepath.ToSlash(rel)
			if seen[key] {
				continue // sessions/ already claimed this relative path
			}
			seen[key] = true
			ordered = append(ordered, scoped{home: home, rel: rel, path: f})
		}
	}
	for _, home := range codexHomes() {
		addDir(home, "sessions")
		addDir(home, "archived_sessions")
		// Direct home fallback when neither subdir exists (saved exec JSONL).
		sess := filepath.Join(home, "sessions")
		arch := filepath.Join(home, "archived_sessions")
		_, e1 := os.Stat(sess)
		_, e2 := os.Stat(arch)
		if e1 != nil && e2 != nil {
			addDir(home, ".")
		}
	}
	out := make([]string, len(ordered))
	for i, s := range ordered {
		out[i] = s.path
	}
	return out
}

func loadCodex() *Aggregate {
	files := codexPaths()
	return loadCached(Codex, files, func() *Aggregate {
		part := loadParts(files, parseCodexFile)
		a := newAggregate(Codex)
		a.Sessions = len(files)
		if part != nil {
			a.Merge(part)
		}
		return a
	})
}

// parseCodexFile reads one rollout JSONL. Prefer last_token_usage when present;
// else attribute the delta of total_token_usage. Always advance prev from the
// cumulative total so mixed last/total streams don't double-count (codeburn).
func parseCodexFile(path string) *Aggregate {
	a := newAggregate(Codex)
	var (
		model                                        string
		prevIn, prevCach, prevOut, prevReason, prevT int64
		havePrev                                     bool
	)
	scanLines(path, func(b []byte) {
		var l codexLine
		if unmarshalJSON(b, &l) != nil {
			return
		}
		ts := parseTime(l.Timestamp)
		switch l.Type {
		case "turn_context":
			var p struct {
				Model string `json:"model"`
			}
			if unmarshalJSON(l.Payload, &p) == nil && p.Model != "" {
				model = p.Model
			}
		case "event_msg":
			var p codexEventMsg
			if unmarshalJSON(l.Payload, &p) != nil {
				return
			}
			switch p.Type {
			case "user_message", "agent_message":
				a.noteMessage(ts, 0)
			case "token_count":
				if p.Info == nil {
					return
				}
				total := p.Info.TotalTokenUsage
				last := p.Info.LastTokenUsage

				// Skip duplicate cumulative snapshots (same total_tokens).
				if total != nil && havePrev && total.TotalTokens == prevT {
					return
				}

				var in, cached, out, reason int64
				if last != nil {
					in = last.InputTokens
					cached = last.CachedInputTokens
					out = last.OutputTokens
					reason = last.ReasoningOutputToks
				} else if total != nil {
					in = nonneg(total.InputTokens - prevIn)
					cached = nonneg(total.CachedInputTokens - prevCach)
					out = nonneg(total.OutputTokens - prevOut)
					reason = nonneg(total.ReasoningOutputToks - prevReason)
				} else {
					return
				}

				// Always mirror cumulative state when present.
				if total != nil {
					prevIn = total.InputTokens
					prevCach = total.CachedInputTokens
					prevOut = total.OutputTokens
					prevReason = total.ReasoningOutputToks
					prevT = total.TotalTokens
					havePrev = true
				}

				if cached > in {
					cached = in
				}
				uncached := nonneg(in - cached)
				outTotal := out + reason // reasoning is billed as output
				tok := uncached + cached + outTotal
				if tok == 0 {
					return
				}
				a.InputTokens += uncached
				a.CacheReadTokens += cached
				a.OutputTokens += outTotal
				a.addTokensOnDay(ts, tok)
				a.addModelTokensOnDay(dayOf(ts), model, tok)
			}
		}
	})
	return a
}

// ---- pi.dev ----

type piLine struct {
	Type      string `json:"type"`
	Timestamp string `json:"timestamp"`
	ModelID   string `json:"modelId"`
	Message   struct {
		Role  string `json:"role"`
		Model string `json:"model"`
		Usage struct {
			Input       int64 `json:"input"`
			Output      int64 `json:"output"`
			CacheRead   int64 `json:"cacheRead"`
			CacheWrite  int64 `json:"cacheWrite"`
			TotalTokens int64 `json:"totalTokens"`
		} `json:"usage"`
	} `json:"message"`
}

// piSessionRoots returns pi-agent session directories (PI_AGENT_DIR or ~/.pi/…).
func piSessionRoots() []string {
	if env := os.Getenv("PI_AGENT_DIR"); env != "" {
		return splitEnvPaths(env)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	return []string{filepath.Join(home, ".pi", "agent", "sessions")}
}

func piPaths() []string {
	var files []string
	for _, root := range piSessionRoots() {
		files = append(files, collectJSONL(root)...)
	}
	return files
}

func loadPi() *Aggregate {
	files := piPaths()
	return loadCached(Pi, files, func() *Aggregate {
		part := loadParts(files, parsePiFile)
		a := newAggregate(Pi)
		a.Sessions = len(files)
		if part != nil {
			a.Merge(part)
		}
		return a
	})
}

func parsePiFile(path string) *Aggregate {
	a := newAggregate(Pi)
	var lastModel string
	scanLines(path, func(b []byte) {
		var l piLine
		if unmarshalJSON(b, &l) != nil {
			return
		}
		switch l.Type {
		case "model_change":
			if l.ModelID != "" {
				lastModel = l.ModelID
			}
		case "message":
			role := l.Message.Role
			if role != "user" && role != "assistant" {
				return
			}
			u := l.Message.Usage
			tok := u.Input + u.Output + u.CacheRead + u.CacheWrite
			if tok == 0 {
				tok = u.TotalTokens
			}
			t := parseTime(l.Timestamp)
			a.noteMessage(t, tok)
			a.InputTokens += u.Input
			a.OutputTokens += u.Output
			a.CacheReadTokens += u.CacheRead
			a.CacheWriteTokens += u.CacheWrite
			if role == "assistant" {
				m := l.Message.Model
				if m == "" {
					m = lastModel
				}
				if m != "" {
					a.addModelOnDay(dayOf(t), m, tok)
				}
			}
		}
	})
	return a
}
