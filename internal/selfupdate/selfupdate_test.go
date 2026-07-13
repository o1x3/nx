package selfupdate

import (
	"archive/tar"
	"compress/gzip"
	"os"
	"path/filepath"
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
