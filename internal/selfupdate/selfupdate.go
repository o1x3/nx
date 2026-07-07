package selfupdate

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const (
	checkInterval = 24 * time.Hour
	updateTimeout = 5 * time.Second
)

type Options struct {
	CurrentVersion string
	Repo           string
	Stderr         io.Writer
}

type release struct {
	TagName string  `json:"tag_name"`
	Assets  []asset `json:"assets"`
}

type asset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
}

type state struct {
	LastChecked time.Time `json:"last_checked"`
}

func Check(ctx context.Context, opts Options) {
	if os.Getenv("NX_NO_UPDATE") == "1" || opts.CurrentVersion == "" || opts.CurrentVersion == "dev" {
		return
	}
	if opts.Repo == "" {
		opts.Repo = "o1x3/nx"
	}
	if opts.Stderr == nil {
		opts.Stderr = io.Discard
	}

	statePath, err := stateFile()
	if err != nil {
		return
	}
	if recentlyChecked(statePath) {
		return
	}
	writeState(statePath)

	updateCtx, cancel := context.WithTimeout(ctx, updateTimeout)
	defer cancel()

	if err := update(updateCtx, opts); err != nil {
		fmt.Fprintf(opts.Stderr, "nx: update check skipped: %v\n", err)
	}
}

func update(ctx context.Context, opts Options) error {
	rel, err := latestRelease(ctx, opts.Repo)
	if err != nil {
		return err
	}
	if !newer(rel.TagName, opts.CurrentVersion) {
		return nil
	}

	asset, ok := matchingAsset(rel.Assets)
	if !ok {
		return fmt.Errorf("no release asset for %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	checksums, ok := checksumAsset(rel.Assets)
	if !ok {
		return errors.New("release has no checksums.txt asset")
	}

	exe, err := os.Executable()
	if err != nil {
		return err
	}
	archivePath, err := downloadFile(ctx, asset.URL, os.TempDir(), ".nx-archive-*")
	if err != nil {
		return err
	}
	defer os.Remove(archivePath)

	if err := verifyChecksum(ctx, checksums.URL, asset.Name, archivePath); err != nil {
		return err
	}

	next, err := extractBinary(archivePath, filepath.Dir(exe))
	if err != nil {
		return err
	}
	defer os.Remove(next)

	mode := os.FileMode(0o755)
	if info, err := os.Stat(exe); err == nil {
		mode = info.Mode()
	}
	if err := os.Chmod(next, mode); err != nil {
		return err
	}
	if err := os.Rename(next, exe); err != nil {
		return fmt.Errorf("replace %s: %w", exe, err)
	}

	fmt.Fprintf(opts.Stderr, "nx: updated %s -> %s\n", opts.CurrentVersion, rel.TagName)
	return nil
}

func latestRelease(ctx context.Context, repo string) (release, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/repos/"+repo+"/releases/latest", nil)
	if err != nil {
		return release{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "nx")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return release{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return release{}, fmt.Errorf("GitHub returned %s", resp.Status)
	}

	var rel release
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return release{}, err
	}
	if rel.TagName == "" {
		return release{}, errors.New("latest release has no tag")
	}
	return rel, nil
}

func matchingAsset(assets []asset) (asset, bool) {
	osPart := strings.ToLower(runtime.GOOS)
	archPart := strings.ToLower(runtime.GOARCH)
	for _, asset := range assets {
		name := strings.ToLower(asset.Name)
		if strings.Contains(name, osPart) && strings.Contains(name, archPart) && strings.HasSuffix(name, ".tar.gz") {
			return asset, true
		}
	}
	return asset{}, false
}

func checksumAsset(assets []asset) (asset, bool) {
	for _, asset := range assets {
		if strings.EqualFold(asset.Name, "checksums.txt") {
			return asset, true
		}
	}
	return asset{}, false
}

func downloadFile(ctx context.Context, url, targetDir, pattern string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "nx")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download returned %s", resp.Status)
	}

	tmp, err := os.CreateTemp(targetDir, pattern)
	if err != nil {
		tmp, err = os.CreateTemp("", pattern)
		if err != nil {
			return "", err
		}
	}
	defer tmp.Close()

	if _, err := io.Copy(tmp, resp.Body); err != nil {
		os.Remove(tmp.Name())
		return "", err
	}
	return tmp.Name(), nil
}

func verifyChecksum(ctx context.Context, checksumURL, archiveName, archivePath string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, checksumURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "nx")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("checksums download returned %s", resp.Status)
	}

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	expected, ok := checksumForArchive(string(raw), archiveName)
	if !ok {
		return fmt.Errorf("checksums.txt has no entry for %s", archiveName)
	}

	file, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer file.Close()

	sum := sha256.New()
	if _, err := io.Copy(sum, file); err != nil {
		return err
	}
	actual := fmt.Sprintf("%x", sum.Sum(nil))
	if !strings.EqualFold(actual, expected) {
		return fmt.Errorf("checksum mismatch for %s", archiveName)
	}
	return nil
}

func checksumForArchive(checksums, archiveName string) (string, bool) {
	for _, line := range strings.Split(checksums, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		name := strings.TrimPrefix(fields[1], "*")
		if name == archiveName {
			return fields[0], true
		}
	}
	return "", false
}

func extractBinary(archivePath, targetDir string) (string, error) {
	file, err := os.Open(archivePath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	gzr, err := gzip.NewReader(file)
	if err != nil {
		return "", err
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)
	for {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", err
		}
		if header.Typeflag != tar.TypeReg || filepath.Base(header.Name) != "nx" {
			continue
		}

		tmp, err := os.CreateTemp(targetDir, ".nx-update-*")
		if err != nil {
			return "", err
		}
		defer tmp.Close()

		if _, err := io.Copy(tmp, tr); err != nil {
			os.Remove(tmp.Name())
			return "", err
		}
		return tmp.Name(), nil
	}

	return "", errors.New("archive did not contain nx binary")
}

func stateFile() (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "nx", "update.json"), nil
}

func recentlyChecked(path string) bool {
	raw, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var s state
	if err := json.Unmarshal(raw, &s); err != nil {
		return false
	}
	return time.Since(s.LastChecked) < checkInterval
}

func writeState(path string) {
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	raw, _ := json.Marshal(state{LastChecked: time.Now()})
	_ = os.WriteFile(path, raw, 0o644)
}

func newer(latest, current string) bool {
	l := parseVersion(latest)
	c := parseVersion(current)
	for i := range l {
		if l[i] > c[i] {
			return true
		}
		if l[i] < c[i] {
			return false
		}
	}
	return false
}

func parseVersion(v string) [3]int {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	v = strings.Split(v, "-")[0]
	parts := strings.Split(v, ".")
	var out [3]int
	for i := 0; i < len(out) && i < len(parts); i++ {
		n, _ := strconv.Atoi(parts[i])
		out[i] = n
	}
	return out
}
