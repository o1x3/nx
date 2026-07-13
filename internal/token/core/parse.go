package core

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
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

// ---- Claude Code: ~/.claude/projects/<proj>/<session>.jsonl ----

type claudeLine struct {
	Type      string `json:"type"`
	Timestamp string `json:"timestamp"`
	RequestID string `json:"requestId"`
	Message   struct {
		ID      string          `json:"id"`
		Role    string          `json:"role"`
		Model   string          `json:"model"`
		Content json.RawMessage `json:"content"`
		Usage   struct {
			InputTokens              int64 `json:"input_tokens"`
			OutputTokens             int64 `json:"output_tokens"`
			CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
			CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
		} `json:"usage"`
	} `json:"message"`
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

func claudePaths() []string {
	return homeGlob(".claude/projects/*/*.jsonl")
}

func loadClaude() *Aggregate {
	files := claudePaths()
	return loadCached(Claude, files, func() *Aggregate {
		part := loadParts(files, parseClaudeFile)
		a := newAggregate(Claude)
		a.Sessions = len(files)
		if part != nil {
			a.Merge(part)
		}
		return a
	})
}

// parseClaudeFile reads one session JSONL. One assistant turn is written as
// several lines (one per content block), each repeating the same cumulative
// message.usage. Dedupe by message id + request id so usage is tallied once.
func parseClaudeFile(path string) *Aggregate {
	a := newAggregate(Claude)
	seen := map[string]bool{}
	scanLines(path, func(b []byte) {
		var l claudeLine
		if unmarshalJSON(b, &l) != nil {
			return
		}
		t := parseTime(l.Timestamp)
		switch l.Type {
		case "user":
			if isToolResult(l.Message.Content) {
				return // tool output, not a human message
			}
			a.noteMessage(t, 0)
		case "assistant":
			if id := l.Message.ID; id != "" {
				key := id + "|" + l.RequestID
				if seen[key] {
					return // duplicate content-block line of the same turn
				}
				seen[key] = true
			}
			u := l.Message.Usage
			tok := u.InputTokens + u.OutputTokens + u.CacheReadInputTokens + u.CacheCreationInputTokens
			a.noteMessage(t, tok)
			a.InputTokens += u.InputTokens
			a.OutputTokens += u.OutputTokens
			a.CacheReadTokens += u.CacheReadInputTokens
			a.CacheWriteTokens += u.CacheCreationInputTokens
			if m := l.Message.Model; m != "" && m != "<synthetic>" {
				a.addModelOnDay(dayOf(t), m, tok)
			}
		}
	})
	return a
}

// ---- Codex: ~/.codex/sessions/YYYY/MM/DD/rollout-*.jsonl ----

type codexLine struct {
	Timestamp string          `json:"timestamp"`
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload"`
}

type codexEventMsg struct {
	Type  string `json:"type"`
	Model string `json:"model"`
	Info  *struct {
		TotalTokenUsage struct {
			InputTokens         int64 `json:"input_tokens"`
			CachedInputTokens   int64 `json:"cached_input_tokens"`
			OutputTokens        int64 `json:"output_tokens"`
			ReasoningOutputToks int64 `json:"reasoning_output_tokens"`
			TotalTokens         int64 `json:"total_tokens"`
		} `json:"total_token_usage"`
	} `json:"info"`
}

func codexPaths() []string {
	return homeGlob(".codex/sessions/*/*/*/*.jsonl")
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

// parseCodexFile reads one rollout JSONL. Codex token_count events carry the
// session-cumulative totals. We attribute the *delta* of each event to that
// event's day and the model active at the time.
func parseCodexFile(path string) *Aggregate {
	a := newAggregate(Codex)
	var (
		model                            string
		prevIn, prevCach, prevOut, prevT int64
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
				tt := p.Info.TotalTokenUsage
				// deltas vs the running cumulative; clamp negatives (a
				// rolled-back thread can lower the totals).
				dIn := nonneg(tt.InputTokens - prevIn)
				dCach := nonneg(tt.CachedInputTokens - prevCach)
				dOut := nonneg(tt.OutputTokens - prevOut)
				dTot := nonneg(tt.TotalTokens - prevT)
				prevIn, prevCach, prevOut, prevT = tt.InputTokens, tt.CachedInputTokens, tt.OutputTokens, tt.TotalTokens

				a.InputTokens += nonneg(dIn - dCach)
				a.CacheReadTokens += dCach
				a.OutputTokens += dOut
				a.addTokensOnDay(ts, dTot)
				a.addModelTokensOnDay(dayOf(ts), model, dTot)
			}
		}
	})
	return a
}

// ---- pi.dev: ~/.pi/agent/sessions/<proj>/<ts>_<uuid>.jsonl ----

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

func piPaths() []string {
	return homeGlob(".pi/agent/sessions/*/*.jsonl")
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
			// Count only real conversational turns; skip tool results.
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
