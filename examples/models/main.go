// Command models lists the models the account can send in a request.
//
// The list holds the aliases. A versioned name such as jev-1.13.0 works
// whether or not it appears here. See https://docs.typesafe.ai/models.
//
// Usage:
//
//	export TYPESAFE_API_KEY=...
//	go run ./examples/models
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/jkakar/typesafe-sdk-go"
)

func main() {
	if err := run(context.Background(), os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "models:", err)
		os.Exit(1)
	}
}

// run lists the models and prints them as an aligned table.
//
// fail; Flush and the one real write below are both checked.
//
//nolint:errcheck // The tabwriter writes into a strings.Builder, which cannot
func run(ctx context.Context, out io.Writer) error {
	client, err := typesafe.NewClient(typesafe.FromEnv())
	if err != nil {
		return err
	}

	models, err := client.ListModels(ctx)
	if err != nil {
		return err
	}

	// Align the columns in memory, then write once, so a broken pipe is one
	// error to report rather than one per row.
	var aligned strings.Builder
	table := tabwriter.NewWriter(&aligned, 0, 0, 2, ' ', 0)
	fmt.Fprintln(table, "NAME\tRELEASED\tDESCRIPTION")
	for _, model := range models {
		fmt.Fprintf(table, "%s\t%s\t%s\n", model.Name, model.ReleaseDate, model.Description)
	}
	if err := table.Flush(); err != nil {
		return err
	}

	_, err = io.WriteString(out, aligned.String())
	return err
}
