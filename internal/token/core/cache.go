package core

import (
	"crypto/sha256"
	"encoding/gob"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func tokenCacheDir() string {
	if d := os.Getenv("NX_CACHE_DIR"); d != "" {
		return filepath.Join(d, "token")
	}
	if d := os.Getenv("XDG_CACHE_HOME"); d != "" {
		return filepath.Join(d, "nx", "token")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".cache", "nx", "token")
}

func cacheDisabled() bool {
	switch strings.ToLower(os.Getenv("NX_TOKEN_NO_CACHE")) {
	case "1", "true", "yes":
		return true
	default:
		return false
	}
}

// statFingerprint hashes sorted path/size/mtime tuples. SQLite sidecars
// (-wal/-shm) are included when present so WAL-mode writers invalidate the cache.
func statFingerprint(paths []string) string {
	sorted := append([]string(nil), paths...)
	sort.Strings(sorted)
	h := sha256.New()
	for _, p := range sorted {
		writeStat(h, p)
		for _, sfx := range []string{"-wal", "-shm"} {
			if _, err := os.Stat(p + sfx); err == nil {
				writeStat(h, p+sfx)
			}
		}
	}
	return hex.EncodeToString(h.Sum(nil))
}

func writeStat(w io.Writer, path string) {
	fi, err := os.Stat(path)
	if err != nil {
		fmt.Fprintf(w, "%s:missing\n", path)
		return
	}
	fmt.Fprintf(w, "%s:%d:%d\n", path, fi.Size(), fi.ModTime().UnixNano())
}

// aggregateCacheVersion is prefixed onto fingerprints so parser semantic
// changes (dedup, meters, discovery) invalidate stale ~/.cache/nx/token gobes.
const aggregateCacheVersion = "3"

func loadCached(harness string, paths []string, load func() *Aggregate) *Aggregate {
	if cacheDisabled() {
		return load()
	}
	dir := tokenCacheDir()
	if dir == "" {
		return load()
	}
	fp := aggregateCacheVersion + ":" + statFingerprint(paths)
	aggPath := filepath.Join(dir, harness+".gob")
	if a, ok := readAggregateCache(aggPath, fp); ok {
		return a
	}
	a := load()
	_ = writeAggregateCache(dir, aggPath, fp, a)
	return a
}

func readAggregateCache(path, wantFP string) (*Aggregate, bool) {
	f, err := os.Open(path)
	if err != nil {
		return nil, false
	}
	defer f.Close()
	var (
		gotFP string
		a     Aggregate
	)
	dec := gob.NewDecoder(f)
	if dec.Decode(&gotFP) != nil || gotFP != wantFP || dec.Decode(&a) != nil {
		return nil, false
	}
	out := a
	return &out, true
}

func writeAggregateCache(dir, path, fp string, a *Aggregate) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "agg-*.gob")
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
	enc := gob.NewEncoder(tmp)
	if enc.Encode(fp) != nil || enc.Encode(a) != nil {
		return fmt.Errorf("encode aggregate cache")
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	ok = true
	return os.Rename(tmpPath, path)
}
