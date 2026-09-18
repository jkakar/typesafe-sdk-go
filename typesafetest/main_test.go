package typesafetest_test

import (
	"os"
	"testing"

	"github.com/jkakar/typesafe-sdk-go/internal/leak"
)

// TestMain fails the package when a test leaves a goroutine behind. See
// docs/testing.md rule 22.
func TestMain(m *testing.M) {
	os.Exit(leak.CheckAfter(m.Run(), os.Stderr))
}
