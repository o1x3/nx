package selfupdate

import "testing"

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
