// Command watchdog-report-validate checks whether a JSON payload is a
// conformant v1 source report, using the same validator the daemon uses to
// ingest reports. It reads from a file argument or, if none, stdin.
// Exit codes: 0 = valid, 2 = invalid report, 1 = usage/IO error.
package main

import (
	"fmt"
	"io"
	"os"

	"watchdog/internal/adapters/module"
)

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	var data []byte
	var err error
	switch len(args) {
	case 0:
		data, err = io.ReadAll(stdin)
	case 1:
		data, err = os.ReadFile(args[0])
	default:
		fmt.Fprintln(stderr, "usage: watchdog-report-validate [file.json]  (or pipe JSON on stdin)")
		return 1
	}
	if err != nil {
		fmt.Fprintf(stderr, "read input: %v\n", err)
		return 1
	}
	if err := module.ValidateReport(data); err != nil {
		fmt.Fprintf(stderr, "invalid: %v\n", err)
		return 2
	}
	fmt.Fprintln(stdout, "valid: conformant v1 source report")
	return 0
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}
