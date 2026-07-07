package gitstat

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/sync/errgroup"
)

const defaultCollectJobs = 8

type Stat struct {
	Path      string
	Name      string
	Head      string
	Base      string
	Added     int
	Removed   int
	Files     int
	Fetched   bool
	FetchNote string
}

type CollectOptions struct {
	Jobs int
}

func Collect(ctx context.Context, folders []string, opts CollectOptions) ([]Stat, error) {
	if len(folders) == 0 {
		return nil, nil
	}

	stats := make([]Stat, len(folders))
	group, ctx := errgroup.WithContext(ctx)
	group.SetLimit(collectJobs(len(folders), opts.Jobs))

	for idx, folder := range folders {
		idx, folder := idx, folder
		group.Go(func() error {
			stat, err := CollectOne(ctx, folder)
			if err != nil {
				return err
			}
			stats[idx] = stat
			return nil
		})
	}

	if err := group.Wait(); err != nil {
		return nil, err
	}

	return stats, nil
}

func collectJobs(folderCount, requested int) int {
	if folderCount < 1 {
		return 0
	}

	jobs := defaultCollectJobs
	if requested > 0 {
		jobs = requested
	}
	if jobs > folderCount {
		return folderCount
	}
	return jobs
}

func CollectOne(ctx context.Context, folder string) (Stat, error) {
	if strings.TrimSpace(folder) == "" {
		return Stat{}, errors.New("folder cannot be empty")
	}

	clean := filepath.Clean(folder)
	info, err := os.Stat(clean)
	if err != nil {
		return Stat{}, fmt.Errorf("%s: %w", clean, err)
	}
	if !info.IsDir() {
		return Stat{}, fmt.Errorf("%s: not a directory", clean)
	}

	if err := run(ctx, clean, "rev-parse", "--is-inside-work-tree"); err != nil {
		return Stat{}, fmt.Errorf("%s: not a git repository", clean)
	}

	base := defaultBranch(ctx, clean)
	fetchNote := ""
	fetched := true
	if err := fetchBase(ctx, clean, base); err != nil {
		fetched = false
		fetchNote = "fetch failed; using local refs"
	}

	head := output(ctx, clean, "rev-parse", "--abbrev-ref", "HEAD")
	if head == "" {
		head = "HEAD"
	}

	added, removed, files, err := diffNumstat(ctx, clean, base)
	if err != nil {
		return Stat{}, fmt.Errorf("%s: diff against %s failed: %w", clean, base, err)
	}

	return Stat{
		Path:      clean,
		Name:      filepath.Base(clean),
		Head:      head,
		Base:      base,
		Added:     added,
		Removed:   removed,
		Files:     files,
		Fetched:   fetched,
		FetchNote: fetchNote,
	}, nil
}

func defaultBranch(ctx context.Context, dir string) string {
	if ref := output(ctx, dir, "symbolic-ref", "--quiet", "--short", "refs/remotes/origin/HEAD"); ref != "" {
		return ref
	}

	if branch := output(ctx, dir, "remote", "show", "-n", "origin"); branch != "" {
		for _, line := range strings.Split(branch, "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "HEAD branch:") {
				name := strings.TrimSpace(strings.TrimPrefix(line, "HEAD branch:"))
				if name != "" && name != "(unknown)" {
					return "origin/" + name
				}
			}
		}
	}

	return "origin/main"
}

func fetchBase(ctx context.Context, dir, base string) error {
	remote, branch, ok := strings.Cut(base, "/")
	if !ok || remote == "" || branch == "" {
		return run(ctx, dir, "fetch", "--quiet", "--no-tags", "origin")
	}

	refspec := fmt.Sprintf("+refs/heads/%s:refs/remotes/%s/%s", branch, remote, branch)
	return run(ctx, dir, "fetch", "--quiet", "--no-tags", remote, refspec)
}

func diffNumstat(ctx context.Context, dir, base string) (int, int, int, error) {
	raw, err := outputErr(ctx, dir, "diff", "--no-ext-diff", "--numstat", base+"...HEAD")
	if err != nil {
		return 0, 0, 0, err
	}

	var added, removed, files int
	scanner := bufio.NewScanner(bytes.NewReader(raw))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 3 {
			continue
		}

		a, aErr := strconv.Atoi(fields[0])
		r, rErr := strconv.Atoi(fields[1])
		if aErr == nil {
			added += a
		}
		if rErr == nil {
			removed += r
		}
		files++
	}
	if err := scanner.Err(); err != nil {
		return 0, 0, 0, err
	}

	return added, removed, files, nil
}

func run(ctx context.Context, dir string, args ...string) error {
	_, err := outputErr(ctx, dir, args...)
	return err
}

func output(ctx context.Context, dir string, args ...string) string {
	raw, err := outputErr(ctx, dir, args...)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(raw))
}

func outputErr(ctx context.Context, dir string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	return cmd.CombinedOutput()
}
