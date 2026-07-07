package cli

import (
	"context"
	"fmt"
	"io"
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
		if len(args) == 1 {
			return fmt.Errorf("usage: nx git stat <folder> [folder...]")
		}

		stats, err := gitstat.Collect(ctx, args[1:])
		if err != nil {
			return err
		}

		fmt.Fprint(stdout, render.GitStats(stats))
		return nil
	default:
		return fmt.Errorf("unknown git subcommand %q\n\n%s", args[0], strings.TrimSpace(gitHelpText()))
	}
}

func helpText() string {
	return `nx is a personal development CLI.

Usage:
  nx <command> [args]

Commands:
  git stat <folder> [folder...]   Show branch diff stats against the repo default branch
  version                         Show build version
  help                            Show help

`
}

func gitHelpText() string {
	return `Usage:
  nx git stat <folder> [folder...]

`
}
