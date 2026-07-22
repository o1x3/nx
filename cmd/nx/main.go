package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/o1x3/nx/internal/cli"
	"github.com/o1x3/nx/internal/selfupdate"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	ctx := context.Background()
	info := cli.BuildInfo{
		Version: version,
		Commit:  commit,
		Date:    date,
	}

	// Explicit `nx update` does its own check; skip the background one so the
	// two paths do not race to replace the binary.
	updateDone := make(chan struct{})
	if !isUpdateCommand(os.Args[1:]) {
		go func() {
			defer close(updateDone)
			selfupdate.Check(ctx, selfupdate.Options{
				CurrentVersion: version,
				Repo:           "o1x3/nx",
				Stderr:         os.Stderr,
			})
		}()
	} else {
		close(updateDone)
	}

	err := cli.New(info).Run(ctx, os.Args[1:], os.Stdout, os.Stderr)
	<-updateDone

	if err != nil {
		code := 1
		var exitErr cli.ExitError
		if errors.As(err, &exitErr) {
			code = exitErr.Code
		}
		if msg := err.Error(); msg != "" {
			fmt.Fprintln(os.Stderr, msg)
		}
		os.Exit(code)
	}
}

func isUpdateCommand(args []string) bool {
	return len(args) > 0 && args[0] == "update"
}
