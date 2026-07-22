package selfupdate

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
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
	checkTimeout  = 5 * time.Second
	updateTimeout = 60 * time.Second
)

// Options configures a background check or an explicit update.
type Options struct {
	CurrentVersion string
	Repo           string
	Stdout         io.Writer
	Stderr         io.Writer
}

// Result describes the outcome of an explicit Update.
type Result struct {
	Current string
	Latest  string
	Updated bool
}

type release struct {
	TagName string
	Assets  []asset
}

type asset struct {
	Name string
	URL  string
}

// Check looks for a newer GitHub release on every invocation and replaces the
// binary in place when possible. Failures stay quiet; a successful update
// prints a short stderr note. Uses github.com/.../releases/latest (not the
// GitHub API).
func Check(ctx context.Context, opts Options) {
	if os.Getenv("NX_NO_UPDATE") == "1" || opts.CurrentVersion == "" || opts.CurrentVersion == "dev" {
		return
	}
	opts = withDefaults(opts)

	checkCtx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()

	rel, err := latestRelease(checkCtx, opts.Repo)
	if err != nil || !newer(rel.TagName, opts.CurrentVersion) {
		return
	}
	cancel()

	// Discovery is time-boxed tightly; downloading a newer binary gets the
	// longer budget used by `nx update`.
	updateCtx, updateCancel := context.WithTimeout(ctx, updateTimeout)
	defer updateCancel()
	_, _ = applyRelease(updateCtx, opts, rel, false)
}

// Update forces a release check and applies it when newer. Unlike Check, it
// surfaces "already up to date", permission errors, and other failures.
func Update(ctx context.Context, opts Options) (Result, error) {
	if opts.CurrentVersion == "" || opts.CurrentVersion == "dev" {
		return Result{}, errors.New("development builds (version dev) cannot self-update; install a released binary")
	}
	opts = withDefaults(opts)

	updateCtx, cancel := context.WithTimeout(ctx, updateTimeout)
	defer cancel()

	return applyUpdate(updateCtx, opts, true)
}

func withDefaults(opts Options) Options {
	if opts.Repo == "" {
		opts.Repo = "o1x3/nx"
	}
	if opts.Stdout == nil {
		opts.Stdout = io.Discard
	}
	if opts.Stderr == nil {
		opts.Stderr = io.Discard
	}
	return opts
}

func applyUpdate(ctx context.Context, opts Options, explicit bool) (Result, error) {
	rel, err := latestRelease(ctx, opts.Repo)
	if err != nil {
		return Result{Current: opts.CurrentVersion}, err
	}
	return applyRelease(ctx, opts, rel, explicit)
}

func applyRelease(ctx context.Context, opts Options, rel release, explicit bool) (Result, error) {
	result := Result{Current: opts.CurrentVersion, Latest: rel.TagName}

	exe, err := os.Executable()
	if err != nil {
		return result, err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	targetDir := filepath.Dir(exe)
	// Default installs land in root-owned dirs like /usr/local/bin. Background
	// checks skip quietly; explicit update reports the problem.
	if !canWriteDir(targetDir) {
		if !explicit {
			return result, nil
		}
		return result, fmt.Errorf("install directory is not writable: %s\nre-run the installer to migrate onto ~/.local/bin", targetDir)
	}

	if !newer(rel.TagName, opts.CurrentVersion) {
		if explicit {
			fmt.Fprintf(opts.Stdout, "nx is up to date (%s)\n", strings.TrimPrefix(opts.CurrentVersion, "v"))
		}
		return result, nil
	}

	asset, ok := matchingAsset(rel.Assets)
	if !ok {
		return result, fmt.Errorf("no release asset for %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	checksums, ok := checksumAsset(rel.Assets)
	if !ok {
		return result, errors.New("release has no checksums.txt asset")
	}

	archivePath, err := downloadFile(ctx, asset.URL, os.TempDir(), ".nx-archive-*")
	if err != nil {
		return result, err
	}
	defer os.Remove(archivePath)

	if err := verifyChecksum(ctx, checksums.URL, asset.Name, archivePath); err != nil {
		return result, err
	}

	next, err := extractBinary(archivePath, targetDir)
	if err != nil {
		return result, err
	}
	defer os.Remove(next)

	mode := os.FileMode(0o755)
	if info, err := os.Stat(exe); err == nil {
		mode = info.Mode()
	}
	if err := os.Chmod(next, mode); err != nil {
		return result, err
	}
	if err := os.Rename(next, exe); err != nil {
		return result, fmt.Errorf("replace %s: %w", exe, err)
	}

	result.Updated = true
	msg := fmt.Sprintf("nx: updated %s -> %s\n", opts.CurrentVersion, rel.TagName)
	if explicit {
		fmt.Fprint(opts.Stdout, msg)
	} else {
		fmt.Fprint(opts.Stderr, msg)
	}
	return result, nil
}

func canWriteDir(dir string) bool {
	f, err := os.CreateTemp(dir, ".nx-write-*")
	if err != nil {
		return false
	}
	name := f.Name()
	_ = f.Close()
	_ = os.Remove(name)
	return true
}

// latestRelease resolves the newest tag via the public releases/latest redirect
// on github.com (same approach as scripts/install.sh). Avoids api.github.com,
// which rate-limits unauthenticated clients.
func latestRelease(ctx context.Context, repo string) (release, error) {
	latestURL := "https://github.com/" + repo + "/releases/latest"
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, latestURL, nil)
	if err != nil {
		return release{}, err
	}
	req.Header.Set("User-Agent", "nx")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return release{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 400 {
		return release{}, fmt.Errorf("GitHub returned %s", resp.Status)
	}

	finalURL := latestURL
	if resp.Request != nil && resp.Request.URL != nil {
		finalURL = resp.Request.URL.String()
	}
	tag, err := tagFromReleaseURL(finalURL)
	if err != nil {
		return release{}, err
	}

	osPart := strings.ToLower(runtime.GOOS)
	archPart := strings.ToLower(runtime.GOARCH)
	archiveName := "nx_" + osPart + "_" + archPart + ".tar.gz"
	base := "https://github.com/" + repo + "/releases/download/" + tag
	return release{
		TagName: tag,
		Assets: []asset{
			{Name: archiveName, URL: base + "/" + archiveName},
			{Name: "checksums.txt", URL: base + "/checksums.txt"},
		},
	}, nil
}

func tagFromReleaseURL(raw string) (string, error) {
	const marker = "/releases/tag/"
	idx := strings.Index(raw, marker)
	if idx < 0 {
		return "", fmt.Errorf("could not determine latest release from %s", raw)
	}
	tag := raw[idx+len(marker):]
	if cut := strings.IndexAny(tag, "/?#"); cut >= 0 {
		tag = tag[:cut]
	}
	tag = strings.TrimSpace(tag)
	if tag == "" {
		return "", fmt.Errorf("could not determine latest release from %s", raw)
	}
	return tag, nil
}

func matchingAsset(assets []asset) (asset, bool) {
	osPart := strings.ToLower(runtime.GOOS)
	archPart := strings.ToLower(runtime.GOARCH)
	for _, a := range assets {
		name := strings.ToLower(a.Name)
		if strings.Contains(name, osPart) && strings.Contains(name, archPart) && strings.HasSuffix(name, ".tar.gz") {
			return a, true
		}
	}
	return asset{}, false
}

func checksumAsset(assets []asset) (asset, bool) {
	for _, a := range assets {
		if strings.EqualFold(a.Name, "checksums.txt") {
			return a, true
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
			// Same-directory temp is preferred for atomic rename; fall back so
			// callers can still surface a clear replace error if needed.
			tmp, err = os.CreateTemp("", ".nx-update-*")
			if err != nil {
				return "", err
			}
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
