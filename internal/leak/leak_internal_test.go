package leak

import (
	"bytes"
	"io"
	"os"
	"testing"

	"github.com/alecthomas/assert/v2"
)

// TestMain runs this package's own leak check, the one CheckAfter installs
// everywhere else.
func TestMain(m *testing.M) {
	os.Exit(CheckAfter(m.Run(), os.Stderr))
}

func TestCheckAfter(t *testing.T) {
	t.Parallel()

	t.Run("returns the code of a run that already failed", func(t *testing.T) {
		t.Parallel()

		assert.Equal(t, 3, CheckAfter(3, io.Discard))
	})
}

// check is exercised against profiles with known contents, because a test
// cannot leak a goroutine to prove the check without leaking it into the
// package's own check. The block profile is always empty and the goroutine
// profile never is.
func TestCheck(t *testing.T) {
	t.Parallel()

	t.Run("returns the code of a run that already failed", func(t *testing.T) {
		t.Parallel()
		var report bytes.Buffer

		assert.Equal(t, 2, check(2, &report, "goroutine"))
		assert.Zero(t, report.Len())
	})

	t.Run("passes a run that left nothing behind", func(t *testing.T) {
		t.Parallel()
		var report bytes.Buffer

		assert.Zero(t, check(0, &report, "block"))
		assert.Zero(t, report.Len())
	})

	t.Run("fails a run whose profile holds goroutines", func(t *testing.T) {
		t.Parallel()
		var report bytes.Buffer

		assert.Equal(t, 1, check(0, &report, "goroutine"))
		assert.Contains(t, report.String(), "goroutines leaked")
		assert.Contains(t, report.String(), "goroutine profile")
	})

	t.Run("fails when the profile is unavailable", func(t *testing.T) {
		t.Parallel()
		var report bytes.Buffer

		assert.Equal(t, 1, check(0, &report, "no-such-profile"))
		assert.Contains(t, report.String(), "the no-such-profile profile is unavailable")
	})
}
