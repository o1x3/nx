package selfupdate

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewer(t *testing.T) {
	tests := []struct {
		latest  string
		current string
		want    bool
	}{
		{"v1.2.0", "v1.1.9", true},
		{"v1.2.0", "1.2.0", false},
		{"v1.2.0", "v1.3.0", false},
		{"v2.0.0", "v1.99.99", true},
	}

	for _, tt := range tests {
		if got := newer(tt.latest, tt.current); got != tt.want {
			t.Fatalf("newer(%q, %q) = %v, want %v", tt.latest, tt.current, got, tt.want)
		}
	}
}

func TestChecksumForArchive(t *testing.T) {
	checksums := `
abc123  nx_darwin_arm64.tar.gz
def456 *nx_linux_amd64.tar.gz
`

	got, ok := checksumForArchive(checksums, "nx_linux_amd64.tar.gz")
	if !ok {
		t.Fatal("checksum not found")
	}
	if got != "def456" {
		t.Fatalf("checksum = %q, want def456", got)
	}
}

func TestCanWriteDir(t *testing.T) {
	writable := t.TempDir()
	if !canWriteDir(writable) {
		t.Fatalf("canWriteDir(%q) = false, want true", writable)
	}

	readonly := t.TempDir()
	if err := os.Chmod(readonly, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Chmod(readonly, 0o755)
	})
	if canWriteDir(readonly) {
		t.Fatalf("canWriteDir(%q) = true, want false", readonly)
	}
}

func TestExtractBinaryFallsBackOutsideTargetDir(t *testing.T) {
	archivePath := writeTestArchive(t, []byte("fake-nx-binary"))
	readonly := t.TempDir()
	if err := os.Chmod(readonly, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Chmod(readonly, 0o755)
	})

	got, err := extractBinary(archivePath, readonly)
	if err != nil {
		t.Fatalf("extractBinary: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(got) })

	if filepath.Dir(got) == readonly {
		t.Fatalf("extracted into readonly dir: %s", got)
	}
	raw, err := os.ReadFile(got)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "fake-nx-binary" {
		t.Fatalf("extracted content = %q", raw)
	}
}

func TestTagFromReleaseURL(t *testing.T) {
	tests := []struct {
		raw  string
		want string
	}{
		{"https://github.com/o1x3/nx/releases/tag/v0.2.0", "v0.2.0"},
		{"https://github.com/o1x3/nx/releases/tag/v1.0.0?foo=1", "v1.0.0"},
		{"https://github.com/o1x3/nx/releases/tag/v1.2.3/#section", "v1.2.3"},
	}
	for _, tt := range tests {
		got, err := tagFromReleaseURL(tt.raw)
		if err != nil {
			t.Fatalf("tagFromReleaseURL(%q): %v", tt.raw, err)
		}
		if got != tt.want {
			t.Fatalf("tagFromReleaseURL(%q) = %q, want %q", tt.raw, got, tt.want)
		}
	}

	if _, err := tagFromReleaseURL("https://github.com/o1x3/nx/releases"); err == nil {
		t.Fatal("expected error for URL without tag")
	}
}

func TestLatestReleaseUsesRedirectNotAPI(t *testing.T) {
	var sawAPI bool
	var sawHead bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/api/") || strings.HasPrefix(r.Host, "api.") {
			sawAPI = true
			http.Error(w, "no api", http.StatusTooManyRequests)
			return
		}
		switch r.URL.Path {
		case "/o1x3/nx/releases/latest":
			if r.Method != http.MethodHead {
				t.Errorf("method = %s, want HEAD", r.Method)
			}
			sawHead = true
			http.Redirect(w, r, "/o1x3/nx/releases/tag/v9.9.9", http.StatusFound)
		case "/o1x3/nx/releases/tag/v9.9.9":
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	tag, err := resolveLatestTag(t.Context(), srv.Client(), srv.URL+"/o1x3/nx/releases/latest")
	if err != nil {
		t.Fatal(err)
	}
	if tag != "v9.9.9" {
		t.Fatalf("tag = %q, want v9.9.9", tag)
	}
	if !sawHead {
		t.Fatal("expected HEAD to releases/latest")
	}
	if sawAPI {
		t.Fatal("must not touch GitHub API")
	}
}

func resolveLatestTag(ctx context.Context, client *http.Client, latestURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, latestURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "nx")
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 400 {
		return "", fmt.Errorf("unexpected status %s", resp.Status)
	}
	finalURL := latestURL
	if resp.Request != nil && resp.Request.URL != nil {
		finalURL = resp.Request.URL.String()
	}
	return tagFromReleaseURL(finalURL)
}

func TestUpdateRejectsDev(t *testing.T) {
	_, err := Update(t.Context(), Options{CurrentVersion: "dev"})
	if err == nil {
		t.Fatal("expected error for dev builds")
	}
	if !strings.Contains(err.Error(), "development builds") {
		t.Fatalf("error = %v", err)
	}
}

func writeTestArchive(t *testing.T, payload []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "nx.tar.gz")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	gzw := gzip.NewWriter(file)
	tw := tar.NewWriter(gzw)
	hdr := &tar.Header{
		Name: "nx",
		Mode: 0o755,
		Size: int64(len(payload)),
	}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(payload); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzw.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}
