package core

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// cursorSession holds the WorkOS session cookie pieces used by the Cursor
// dashboard usage API (same auth the web UI uses — not a crsr_ API key).
type cursorSession struct {
	Sub string // JWT "sub" claim (e.g. user_01… or github|…)
	JWT string // raw access token from Cursor's local store
}

// cookieValue is WorkosCursorSessionToken=<sub>%3A%3A<jwt>.
func (s cursorSession) cookieValue() string {
	return s.Sub + "%3A%3A" + s.JWT
}

// resolveCursorSession finds a Cursor dashboard session.
// Priority: NX_CURSOR_SESSION_TOKEN / CURSOR_SESSION_TOKEN env → state.vscdb
// ItemTable cursorAuth/accessToken.
func resolveCursorSession() (cursorSession, bool) {
	for _, key := range []string{"NX_CURSOR_SESSION_TOKEN", "CURSOR_SESSION_TOKEN"} {
		if v := strings.TrimSpace(os.Getenv(key)); v != "" {
			if s, ok := parseCursorSessionOverride(v); ok {
				return s, true
			}
		}
	}
	return readCursorAccessToken()
}

// parseCursorSessionOverride accepts a raw JWT or a pre-formed "sub::jwt"
// (plain or with %3A%3A).
func parseCursorSessionOverride(v string) (cursorSession, bool) {
	v = strings.TrimSpace(v)
	if v == "" {
		return cursorSession{}, false
	}
	if strings.Contains(v, "%3A%3A") {
		parts := strings.SplitN(v, "%3A%3A", 2)
		if len(parts) == 2 && parts[0] != "" && parts[1] != "" {
			return cursorSession{Sub: parts[0], JWT: parts[1]}, true
		}
	}
	if i := strings.Index(v, "::"); i > 0 {
		sub, jwt := v[:i], v[i+2:]
		if sub != "" && jwt != "" {
			return cursorSession{Sub: sub, JWT: jwt}, true
		}
	}
	sub, ok := jwtSub(v)
	if !ok {
		return cursorSession{}, false
	}
	return cursorSession{Sub: sub, JWT: v}, true
}

func readCursorAccessToken() (cursorSession, bool) {
	home, err := os.UserHomeDir()
	if err != nil {
		return cursorSession{}, false
	}
	paths := []string{
		filepath.Join(home, "Library", "Application Support", "Cursor", "User", "globalStorage", "state.vscdb"),
		filepath.Join(home, ".config", "Cursor", "User", "globalStorage", "state.vscdb"),
	}
	for _, p := range paths {
		if _, err := os.Stat(p); err != nil {
			continue
		}
		if s, ok := readAccessTokenFromDB(p); ok {
			return s, true
		}
	}
	return cursorSession{}, false
}

func readAccessTokenFromDB(path string) (cursorSession, bool) {
	db, cleanup, err := openDB(path)
	if err != nil {
		return cursorSession{}, false
	}
	defer cleanup()

	var value string
	err = db.QueryRow(`SELECT value FROM ItemTable WHERE key = ?`, "cursorAuth/accessToken").Scan(&value)
	if err != nil || value == "" {
		return cursorSession{}, false
	}
	sub, ok := jwtSub(value)
	if !ok {
		return cursorSession{}, false
	}
	return cursorSession{Sub: sub, JWT: value}, true
}

// jwtSub decodes the payload of a JWT and returns the "sub" claim.
func jwtSub(token string) (string, bool) {
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return "", false
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		padded := parts[1]
		if m := len(padded) % 4; m != 0 {
			padded += strings.Repeat("=", 4-m)
		}
		payload, err = base64.URLEncoding.DecodeString(padded)
		if err != nil {
			return "", false
		}
	}
	var claims struct {
		Sub string `json:"sub"`
	}
	if json.Unmarshal(payload, &claims) != nil || claims.Sub == "" {
		return "", false
	}
	return claims.Sub, true
}
