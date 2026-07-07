package gitstat

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestCollectOneAgainstOriginHead(t *testing.T) {
	root := t.TempDir()
	remote := filepath.Join(root, "remote.git")
	work := filepath.Join(root, "work")
	clone := filepath.Join(root, "clone")

	git(t, root, "init", "--bare", remote)
	git(t, root, "clone", remote, work)
	git(t, work, "config", "user.email", "test@example.com")
	git(t, work, "config", "user.name", "Test")
	write(t, filepath.Join(work, "a.txt"), "one\n")
	git(t, work, "add", "a.txt")
	git(t, work, "commit", "-m", "initial")
	git(t, work, "branch", "-M", "trunk")
	git(t, work, "push", "-u", "origin", "trunk")
	git(t, remote, "symbolic-ref", "HEAD", "refs/heads/trunk")

	git(t, root, "clone", remote, clone)
	git(t, clone, "config", "user.email", "test@example.com")
	git(t, clone, "config", "user.name", "Test")
	git(t, clone, "checkout", "-b", "feature")
	write(t, filepath.Join(clone, "a.txt"), "one\ntwo\nthree\n")
	write(t, filepath.Join(clone, "b.txt"), "new\n")
	git(t, clone, "add", ".")
	git(t, clone, "commit", "-m", "change")

	stat, err := CollectOne(context.Background(), clone)
	if err != nil {
		t.Fatal(err)
	}

	if stat.Base != "origin/trunk" {
		t.Fatalf("base = %q, want origin/trunk", stat.Base)
	}
	if stat.Added != 3 {
		t.Fatalf("added = %d, want 3", stat.Added)
	}
	if stat.Removed != 0 {
		t.Fatalf("removed = %d, want 0", stat.Removed)
	}
	if stat.Files != 2 {
		t.Fatalf("files = %d, want 2", stat.Files)
	}
}

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, out)
	}
}

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
