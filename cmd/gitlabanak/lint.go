package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/leejianrong/gitlabanak/internal/ci"
)

// lintCommand implements `gitlabanak lint [--root dir] [--json] <file>`. It needs no server.
// Exit status: 0 valid, 1 findings, 2 could not run.
func lintCommand(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("lint", flag.ContinueOnError)
	fs.SetOutput(stderr)
	root := fs.String("root", "", "repository root for include:local (default: the file's directory)")
	asJSON := fs.Bool("json", false, "print the REST lint response shape")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		_, _ = fmt.Fprintln(stderr, "usage: gitlabanak lint [--root dir] [--json] <file>")
		return 2
	}
	content, err := os.ReadFile(fs.Arg(0))
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		return 2
	}
	if *root == "" {
		*root = filepath.Dir(fs.Arg(0))
	}
	res := ci.Lint(string(content), func(p string) (string, bool) {
		b, err := os.ReadFile(filepath.Join(*root, p))
		return string(b), err == nil
	})
	if *asJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(res)
	} else {
		for _, e := range res.Errors {
			_, _ = fmt.Fprintln(stdout, "error:", e)
		}
		for _, w := range res.Warnings {
			_, _ = fmt.Fprintln(stdout, "warning:", w)
		}
		if res.Valid {
			_, _ = fmt.Fprintln(stdout, "OK")
		}
	}
	if !res.Valid {
		return 1
	}
	return 0
}
