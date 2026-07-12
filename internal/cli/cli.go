package cli

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/o1x3/nx/internal/gitstat"
	"github.com/o1x3/nx/internal/render"
)

type BuildInfo struct {
	Version string
	Commit  string
	Date    string
}

type App struct {
	info BuildInfo
}

func New(info BuildInfo) App {
	return App{info: info}
}

func (a App) Run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		fmt.Fprint(stdout, helpText())
		return nil
	}

	switch args[0] {
	case "help", "-h", "--help":
		fmt.Fprint(stdout, helpText())
		return nil
	case "version", "-v", "--version":
		fmt.Fprintf(stdout, "nx %s (%s, %s)\n", a.info.Version, a.info.Commit, a.info.Date)
		return nil
	case "git":
		return a.runGit(ctx, args[1:], stdout)
	case "token", "tokens":
		return a.runToken(ctx, args[1:], stdout)
	default:
		return fmt.Errorf("unknown command %q\n\n%s", args[0], strings.TrimSpace(helpText()))
	}
}

func (a App) runGit(ctx context.Context, args []string, stdout io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("missing git subcommand\n\n%s", strings.TrimSpace(gitHelpText()))
	}

	switch args[0] {
	case "help", "-h", "--help":
		fmt.Fprint(stdout, gitHelpText())
		return nil
	case "stat", "stats":
		opts, folders, err := parseGitStatArgs(args[1:])
		if err != nil {
			return err
		}

		stats, err := gitstat.Collect(ctx, folders, opts)
		if err != nil {
			return err
		}

		fmt.Fprint(stdout, render.GitStats(stats))
		return nil
	default:
		return fmt.Errorf("unknown git subcommand %q\n\n%s", args[0], strings.TrimSpace(gitHelpText()))
	}
}

func parseGitStatArgs(args []string) (gitstat.CollectOptions, []string, error) {
	var opts gitstat.CollectOptions
	var folders []string
	parseOptions := true

	for idx := 0; idx < len(args); idx++ {
		arg := args[idx]
		switch {
		case parseOptions && arg == "--":
			parseOptions = false
		case parseOptions && arg == "--jobs":
			idx++
			if idx >= len(args) {
				return gitstat.CollectOptions{}, nil, fmt.Errorf("--jobs requires a value")
			}
			jobs, err := parsePositiveInt(args[idx], "--jobs")
			if err != nil {
				return gitstat.CollectOptions{}, nil, err
			}
			opts.Jobs = jobs
		case parseOptions && strings.HasPrefix(arg, "--jobs="):
			jobs, err := parsePositiveInt(strings.TrimPrefix(arg, "--jobs="), "--jobs")
			if err != nil {
				return gitstat.CollectOptions{}, nil, err
			}
			opts.Jobs = jobs
		case parseOptions && strings.HasPrefix(arg, "-"):
			return gitstat.CollectOptions{}, nil, fmt.Errorf("unknown option %q", arg)
		default:
			folders = append(folders, arg)
		}
	}

	if len(folders) == 0 {
		return gitstat.CollectOptions{}, nil, fmt.Errorf("usage: nx git stat [--jobs <n>] <folder> [folder...]")
	}

	return opts, folders, nil
}

func parsePositiveInt(raw, name string) (int, error) {
	parsed, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || parsed < 1 {
		return 0, fmt.Errorf("%s must be a positive integer", name)
	}
	return parsed, nil
}

func helpText() string {
	return `nx is a personal development CLI.

Usage:
  nx <command> [args]

Commands:
  git stat [--jobs <n>] <folder> [folder...]   Show branch diff stats against the repo default branch
  token [harness] [range] [view]  Token stats across AI coding harnesses (see nx token --help)
  version                         Show build version
  help                            Show help

`
}

func gitHelpText() string {
	return `Usage:
  nx git stat [--jobs <n>] <folder> [folder...]

`
}
