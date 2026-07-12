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
	if json.Unmarshal(content, &blocks) != nil {
		return false // string content => a genuine prompt
	}
	for _, b := range blocks {
		if b.Type == "tool_result" {
			return true
		}
	}
	return false
}

func loadClaude() *Aggregate {
	a := newAggregate(Claude)
	files := homeGlob(".claude/projects/*/*.jsonl")
	a.Sessions = len(files)
	// One assistant turn is written as several JSONL lines (one per content
	// block), each repeating the same cumulative message.usage. Dedupe by
	// message id + request id so usage and counts are tallied once per turn.
	seen := map[string]bool{}
	for _, f := range files {
		scanLines(f, func(b []byte) {
			var l claudeLine
			if json.Unmarshal(b, &l) != nil {
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
	}
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

func loadCodex() *Aggregate {
	a := newAggregate(Codex)
	files := homeGlob(".codex/sessions/*/*/*/*.jsonl")
	a.Sessions = len(files)
	for _, f := range files {
		// Codex token_count events carry the session-cumulative totals. We
		// attribute the *delta* of each event to that event's day and the model
		// active at the time, so windowed totals and the heatmap reflect when
		// the work actually happened (sessions can span multiple days/models).
		var (
			model                            string
			prevIn, prevCach, prevOut, prevT int64
		)
		scanLines(f, func(b []byte) {
			var l codexLine
			if json.Unmarshal(b, &l) != nil {
				return
			}
			ts := parseTime(l.Timestamp)
			switch l.Type {
			case "turn_context":
				var p struct {
					Model string `json:"model"`
				}
				if json.Unmarshal(l.Payload, &p) == nil && p.Model != "" {
					model = p.Model
				}
			case "event_msg":
				var p codexEventMsg
				if json.Unmarshal(l.Payload, &p) != nil {
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
	}
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

func loadPi() *Aggregate {
	a := newAggregate(Pi)
	files := homeGlob(".pi/agent/sessions/*/*.jsonl")
	a.Sessions = len(files)
	for _, f := range files {
		var lastModel string
		scanLines(f, func(b []byte) {
			var l piLine
			if json.Unmarshal(b, &l) != nil {
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
	}
	return a
}

// Load returns the aggregate for a single harness key.
func Load(harness string) *Aggregate {
	switch harness {
	case Claude:
		return loadClaude()
	case Codex:
		return loadCodex()
	case Pi:
		return loadPi()
	case Cursor:
		return loadCursor()
	default:
		return LoadAll()
	}
}

// LoadAll loads and merges every harness.
func LoadAll() *Aggregate {
	a := newAggregate(Combined)
	for _, h := range Harnesses {
		a.Merge(Load(h))
	}
	return a
}

// LoadEach loads every concrete harness once and returns them keyed by
// harness name, with the merged Combined aggregate under Combined.
func LoadEach() map[string]*Aggregate {
	m := make(map[string]*Aggregate, len(Harnesses)+1)
	all := newAggregate(Combined)
	for _, h := range Harnesses {
		a := Load(h)
		m[h] = a
		all.Merge(a)
	}
	m[Combined] = all
	return m
}
