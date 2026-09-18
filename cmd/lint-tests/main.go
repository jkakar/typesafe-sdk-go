// Command lint-tests reports Go source that breaks the mechanical rules in
// docs/testing.md. It reads every Go file under the directories named on the
// command line, or under the working directory when given none.
package main

import (
	"fmt"
	"io"
	"os"

	"github.com/jkakar/typesafe-sdk-go/internal/testpolicy"
)

func main() {
	roots := os.Args[1:]
	if len(roots) == 0 {
		roots = []string{"."}
	}
	os.Exit(run(roots, os.Stdout, os.Stderr))
}

// run reports the violations under each root, returning the process exit code.
// Diagnostics are best effort: a command that cannot write its own report has
// nowhere left to say so.
//
//nolint:errcheck // Reporting a failed write would need the same writer.
func run(roots []string, report, errors io.Writer) int {
	found := 0
	for _, root := range roots {
		findings, err := testpolicy.CheckDir(root)
		if err != nil {
			fmt.Fprintf(errors, "lint-tests: %s\n", err)
			return 2
		}
		for _, finding := range findings {
			fmt.Fprintln(report, finding)
		}
		found += len(findings)
	}
	if found > 0 {
		fmt.Fprintf(errors, "lint-tests: %d test policy violations\n", found)
		return 1
	}
	return 0
}
