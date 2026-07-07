package main

import (
	"context"
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

	selfupdate.Check(ctx, selfupdate.Options{
		CurrentVersion: version,
		Repo:           "o1x3/nx",
		Stderr:         os.Stderr,
	})

	if err := cli.New(info).Run(ctx, os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
