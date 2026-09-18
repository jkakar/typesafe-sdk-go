package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/alecthomas/assert/v2"

	"github.com/jkakar/typesafe-sdk-go/internal/leak"
)

// TestMain fails the package when a test leaves a goroutine behind. See
// docs/testing.md rule 22.
func TestMain(m *testing.M) {
	os.Exit(leak.CheckAfter(m.Run(), os.Stderr))
}

// run is command wiring the package keeps private, so its test is internal.
// See docs/testing.md rule 6.
func TestRun(t *testing.T) {
	t.Parallel()

	t.Run("passes a tree that follows the rules", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		var report, problems bytes.Buffer

		code := run([]string{root}, &report, &problems)

		assert.Zero(t, code)
		assert.Zero(t, report.Len())
		assert.Zero(t, problems.Len())
	})

	t.Run("reports each violation and fails", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		source := "package x\n\nimport \"testing\"\n\nfunc TestThing(t *testing.T) { t.Fatal(\"boom\") }\n"
		assert.NoError(t, os.WriteFile(filepath.Join(root, "bad_test.go"), []byte(source), 0o600))
		var report, problems bytes.Buffer

		code := run([]string{root}, &report, &problems)

		assert.Equal(t, 1, code)
		assert.Contains(t, report.String(), "rule 13")
		assert.Contains(t, problems.String(), "1 test policy violations")
	})

	t.Run("fails when a root cannot be read", func(t *testing.T) {
		t.Parallel()
		var report, problems bytes.Buffer

		code := run([]string{filepath.Join(t.TempDir(), "missing")}, &report, &problems)

		assert.Equal(t, 2, code)
		assert.Contains(t, problems.String(), "lint-tests:")
	})
}
