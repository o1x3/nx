package gitstat

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

	write(t, filepath.Join(work, "remote.txt"), "remote\n")
	git(t, work, "add", "remote.txt")
	git(t, work, "commit", "-m", "remote change")
	git(t, work, "push", "origin", "trunk")
	remoteHead := gitOutput(t, work, "rev-parse", "HEAD")

	git(t, work, "checkout", "-b", "side")
	write(t, filepath.Join(work, "side.txt"), "side\n")
	git(t, work, "add", "side.txt")
	git(t, work, "commit", "-m", "side change")
	git(t, work, "push", "origin", "side")

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
	if got := gitOutput(t, clone, "rev-parse", "origin/trunk"); got != remoteHead {
		t.Fatalf("origin/trunk = %q, want %q", got, remoteHead)
	}
	if gitSucceeds(clone, "rev-parse", "--verify", "refs/remotes/origin/side") {
		t.Fatal("origin/side exists; fetch should update only the default branch")
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

func TestCollectRunsConcurrentlyInInputOrder(t *testing.T) {
	root := t.TempDir()
	bin := filepath.Join(root, "bin")
	if err := os.Mkdir(bin, 0o755); err != nil {
		t.Fatal(err)
	}

	fakeGit := filepath.Join(bin, "git")
	write(t, fakeGit, `#!/bin/sh
case "$1" in
	rev-parse)
		if [ "$2" = "--is-inside-work-tree" ]; then
			exit 0
		fi
		if [ "$2" = "--abbrev-ref" ]; then
			echo "feature"
			exit 0
		fi
		;;
	symbolic-ref)
		echo "origin/main"
		exit 0
		;;
	fetch)
		exit 0
		;;
	diff)
		sync_dir=$NX_TEST_SYNC_DIR
		name=$(basename "$PWD")
		: > "$sync_dir/$name.started"
		i=0
		while [ "$(find "$sync_dir" -name '*.started' | wc -l | tr -d ' ')" -lt 4 ]; do
			i=$((i + 1))
			if [ "$i" -gt 100 ]; then
				exit 2
			fi
			sleep 0.05
		done
		case "$name" in
			repo-a) printf '1\t0\ta.txt\n' ;;
			repo-b) printf '2\t0\tb.txt\n' ;;
			repo-c) printf '3\t0\tc.txt\n' ;;
			repo-d) printf '4\t0\td.txt\n' ;;
		esac
		exit 0
		;;
esac
exit 1
`)
	if err := os.Chmod(fakeGit, 0o755); err != nil {
		t.Fatal(err)
	}

	oldPath := os.Getenv("PATH")
	t.Setenv("PATH", bin+string(os.PathListSeparator)+oldPath)
	t.Setenv("NX_TEST_SYNC_DIR", root)

	folders := []string{
		filepath.Join(root, "repo-a"),
		filepath.Join(root, "repo-b"),
		filepath.Join(root, "repo-c"),
		filepath.Join(root, "repo-d"),
	}
	for _, folder := range folders {
		if err := os.Mkdir(folder, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	stats, err := Collect(ctx, folders, CollectOptions{Jobs: 4})
	if err != nil {
		t.Fatal(err)
	}

	for idx, stat := range stats {
		if stat.Path != folders[idx] {
			t.Fatalf("stats[%d].Path = %q, want %q", idx, stat.Path, folders[idx])
		}
		if stat.Added != idx+1 {
			t.Fatalf("stats[%d].Added = %d, want %d", idx, stat.Added, idx+1)
		}
	}
}

func TestValidRemoteHeadBranchRejectsGitPlaceholders(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{name: "main", want: true},
		{name: "trunk", want: true},
		{name: "", want: false},
		{name: "(unknown)", want: false},
		{name: "(not queried)", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := validRemoteHeadBranch(tt.name); got != tt.want {
				t.Fatalf("validRemoteHeadBranch(%q) = %t, want %t", tt.name, got, tt.want)
			}
		})
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

func gitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func gitSucceeds(dir string, args ...string) bool {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	return cmd.Run() == nil
}

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
