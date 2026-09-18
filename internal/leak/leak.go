// Package leak fails a test binary that leaves a goroutine behind.
package leak

import (
	"bytes"
	"fmt"
	"io"
	"runtime"
	"runtime/pprof"
)

// profileName is the Go runtime profile that reports goroutines blocked with
// no way to be woken.
const profileName = "goroutineleak"

// CheckAfter returns code when the test run already failed or left no
// goroutine behind, and a failing code otherwise. Call it once per package,
// after m.Run, so the check sees only work that outlived the whole suite:
//
//	func TestMain(m *testing.M) {
//		os.Exit(leak.CheckAfter(m.Run(), os.Stderr))
//	}
//
// Checking once per package, rather than once per test, is what makes the
// result trustworthy: tests run in parallel, so a per-test check can see a
// goroutine a sibling still owns.
func CheckAfter(code int, report io.Writer) int {
	return check(code, report, profileName)
}

// check runs the named profile and reports its leaks to report.
func check(code int, report io.Writer, name string) int {
	if code != 0 {
		return code
	}
	if err := profileLeaks(report, name); err != nil {
		//nolint:errcheck // A diagnostic has nowhere left to report a write failure.
		fmt.Fprintf(report, "goroutine leak check failed: %s\n", err)
		return 1
	}
	return 0
}

// profileLeaks reports the goroutines the named profile holds, writing their
// stacks to report.
//
// The order matters. The runtime computes the leak profile during a garbage
// collection, so a run that collected nothing since the leak appeared reports
// none, and Count returns the last computed total, so it is only accurate
// after WriteTo.
func profileLeaks(report io.Writer, name string) error {
	profile := pprof.Lookup(name)
	if profile == nil {
		return fmt.Errorf("the %s profile is unavailable", name)
	}
	runtime.GC()
	var stacks bytes.Buffer
	if err := profile.WriteTo(&stacks, 1); err != nil {
		return fmt.Errorf("write the %s profile: %w", name, err)
	}
	count := profile.Count()
	if count == 0 {
		return nil
	}
	//nolint:errcheck // A diagnostic has nowhere left to report a write failure.
	fmt.Fprint(report, stacks.String())
	return fmt.Errorf("%d goroutines leaked", count)
}
