# Testing

This is the canonical rule set for Go tests in this repository. Read it
completely before writing or reviewing a test. When a rule and existing code
disagree, fix the code or record the narrow exception here.

The rules are scoped to what an SDK needs: there is no database, no deployed
service, and one external system, the TypeSafe API.

## Enforcement

These rules are enforced by review. `make check` runs gofmt, `go vet`,
`golangci-lint`, and the tests with the race detector, shuffling, and
coverage, which catches some of what follows: `bodyclose` finds an unclosed
response body and `errcheck` finds an ignored error. The rest is a reading.

Four are worth a second look in any review, because breaking them fails later
and elsewhere, and the failure looks like flakiness rather than like a broken
rule:

| Rule | What to look for |
|------|------------------|
| 8. Use real local infrastructure | `http.DefaultClient`, `http.DefaultTransport`, `http.Get`, `http.Head`, `http.Post`, or `http.PostForm` in a test |
| 12. Do not depend on environment variables | `Setenv` or `Unsetenv` in a test |
| 13. Use `assert/v2` | `t.Error`, `t.Fatal`, `t.Fail`, or their variants, in any Go file |
| 22. Detect goroutine leaks | a `TestMain` that does not end the package with `leak.CheckAfter` |

## 1. Name tests after the surface under test

Use exactly one top-level `TestXxx` for a function or method. Put behaviors,
conditions, error paths, and input variations in `t.Run` subtests.

The surface under test is the function a caller reaches for, not a private
helper that happens to implement one branch. Prefer driving private code
through exported behavior.

Name package functions `TestNewClient` and receiver methods
`TestClient_SystemOne`. Preserve nested call structure with another underscore,
such as `TestAnswers_UnmarshalJSON`.

A type whose whole job is to configure behavior is also a surface.
`TestRetryPolicy` drives `Client.SystemOne`, because a retry only happens
during a call, but the behavior under test belongs to the policy. Name the
test for the policy and keep it in `retry_test.go`.

```go
func TestAnswers_Noul(t *testing.T) {
	t.Parallel()

	t.Run("returns the answer for a yes/no question", func(t *testing.T) {
		t.Parallel()
		// ...
	})

	t.Run("reports an unanswered question", func(t *testing.T) {
		t.Parallel()
		// ...
	})
}
```

## 2. Describe behavior in prose

Subtest descriptions use lowercase, present-tense prose:

- `<verb>s <object>` when the verb carries the condition.
- `<verb>s <object> when <condition>` when the condition needs stating.

Use `"returns one answer per question"` and `"rejects a request with no
questions"`. Do not use `"should ..."`, title case, identifier syntax, or a
condition with no stated behavior. Apply the same rule to `name` fields and
map keys in table tests.

## 3. Use Arrange-Act-Assert

Test bodies follow Arrange-Act-Assert. Separate the three phases with blank
lines; do not add comments that merely name them.

```go
func TestNewClient(t *testing.T) {
	t.Parallel()
	lookup := env(map[string]string{typesafe.EnvAPIKey: "secret"})

	client, err := typesafe.NewClient(typesafe.FromEnvFunc(lookup))

	assert.NoError(t, err)
	assert.Equal(t, typesafe.DefaultModel, client.Model())
}
```

One subtest verifies one logical concept, not literally one assertion. Split a
test when its assertion phase starts verifying an unrelated behavior.

## 4. Choose table tests deliberately

Use a table when every case has the same setup and assertion shape. Use named
subtests when cases require different setup, side effects, or assertions.

A `map[string]case` table reads well when the key is the behavior and the
value is the input. Iterate it inside `t.Run` so each case is a named subtest
with its own `t.Parallel()`.

Do not use `wantErr bool`. Assert a sentinel with `assert.IsError`, inspect a
typed error with `errors.As`, or name the expected error explicitly.

## 5. Follow red-green-refactor

For a behavior change:

1. Write or change the test first.
2. Run it and observe the expected assertion failure.
3. Add the minimum production change that makes it pass.
4. Refactor with the suite green.

A pure refactor does not change behavioral expectations. It may move or
reshape tests when package boundaries change, but it must preserve what they
prove.

## 6. Use external test packages by default

Test files use `package typesafe_test` and exercise the package through its
exported API. This keeps tests coupled to the contract a caller depends on,
which for a published SDK is the whole point.

Use an internal test package only for:

1. A private invariant whose meaning would be lost if exported, such as the
   backoff schedule in `retry_internal_test.go`.
2. A package under `internal/` whose whole surface is private to this module,
   such as `internal/leak`.

Name an internal test file `*_internal_test.go` so the exception is visible,
and say in a comment why the surface stays private.

## 7. Exercise real internal code

Tests use the real packages of this module. Do not stub one package of this
module to make another package's test smaller.

Fakes and narrow stubs are allowed for:

- The TypeSafe API itself, through the canonical fake in `typesafetest`.
- Failures that cannot be produced any other way, such as a connection that
  drops mid-request.
- The network inside a `testing/synctest` bubble. See rule 11.

Do not use mocking frameworks or generated mocks.

## 8. Use real local infrastructure

Use the closest deterministic implementation:

| Resource | Test implementation |
|----------|---------------------|
| TypeSafe API | `typesafetest.NewServer` |
| HTTP server | `httptest.NewServer` running a real handler |
| HTTP client | `srv.Client()`, `typesafetest.Server.HTTPClient`, or a client with its own transport |
| Filesystem | `t.TempDir()` |
| Time | `testing/synctest` |

Do not use `http.DefaultClient`, `http.Get`, `http.Post`, or `http.Head`.
Parallel `httptest.Server` cleanup closes idle connections on the shared
default transport, so a test riding a pooled connection at that moment fails
with `http: CloseIdleConnections called`. A client that still uses
`http.DefaultTransport` isolates nothing.

`typesafetest` is the canonical fake for the API this SDK calls. Reach for it
when a test needs a working API, and for a raw `httptest.Server` when a test
needs a response the fake will not produce, such as a truncated body or an
answer type the SDK does not model.

## 9. Run tests in parallel by default

Every test calls `t.Parallel()` unless it mutates process-global state or has
another documented reason it cannot. Independently isolated subtests call it
too.

A test that cannot run in parallel often exposes hidden coupling: global
state, fixed filenames, shared identifiers, or ordering assumptions. Remove
the coupling instead of silently serializing the test.

## 10. Use `t.Context()`

Use `t.Context()` for operations under test. It is canceled during test
cleanup and follows the test's lifetime.

Do not construct `context.WithCancel(context.Background())` just to obtain a
test context. Derive a narrower deadline or cancellation from `t.Context()`
when the behavior itself requires one.

## 11. Make time deterministic

Never use wall-clock `time.Sleep` to synchronize a test. Use channels,
`sync.WaitGroup`, condition variables, or explicit callbacks.

Use `testing/synctest` for behavior driven by timers, deadlines, retry
backoff, or TTLs. Advance virtual time inside `synctest.Test` and call
`synctest.Wait()` before asserting on work performed by another goroutine.

Real network I/O does not belong inside a bubble: the goroutines that serve it
live outside, and the timers inside run on a clock the network does not
observe. A test that asserts on a retry delay therefore gives the client an
`http.RoundTripper` that answers in process. That stub is the seam rule 7
allows, and `newStubClient` in `helpers_test.go` is its only home.

Do not shrink a production duration to hide a wait. Configuring a short
backoff through `RetryPolicy` is different: the duration is the caller's to
set, and a test that sets it is exercising the real contract.

## 12. Do not depend on environment variables

Tests construct typed configuration directly. Do not use `os.Setenv` or
`t.Setenv`. Environment mutation serializes tests and lets ambient process
state leak into behavior.

The SDK reads the environment in exactly one place, `FromEnv`, which delegates
to `FromEnvFunc`. Tests pass their own lookup function, so no test needs the
process environment to be anything in particular.

## 13. Use `assert/v2`

Use `github.com/alecthomas/assert/v2` for every assertion. Do not use another
assertion library, custom wrappers, or raw `t.Error`, `t.Errorf`, `t.Fatal`,
`t.Fatalf`, `t.Fail`, or `t.FailNow`.

This includes test helpers that receive `*testing.T`. Use `t.Helper()` and an
`assert` call so failures retain useful source attribution.

`assert.Equal` renders both arguments with `repr` on every call, including the
passing path, and `repr` walks unexported fields. Never deep-compare a value
that reaches shared process state, such as an `*http.Client` or an
`*http.Response`: a parallel sibling writes that state and the race detector
reports the collision. Assert the one field the test cares about, or use
`assert.True`, which never reflects.

## 14. Assert specific errors

Use `assert.IsError(t, err, typesafe.ErrRateLimit)` for sentinels. Use
`errors.As` for typed errors and assert their fields. When an unexpected error
has no stable type, assert the operation context without coupling the test to
the entire message.

Do not use string matching when a structured error contract exists. Every
failure this SDK reports has one: an `*APIError` unwraps to the sentinel for
its status, and a `*DecodeError` unwraps to the decoding failure.

## 15. Avoid golden and snapshot output

Assert explicit fields and semantic fragments. Golden files make it easy to
regenerate a changed contract without reviewing what changed.

A request body is the contract between this SDK and the API, so a test that
proves what goes on the wire compares the encoded JSON to a literal in the
test. Keep the literal visible; do not move it to a file and do not add a flag
that rewrites it.

## 16. Keep fixtures exceptional

Prefer self-contained input built in the test. Use `testdata/` only when an
input is too large to read inline or must remain a standalone protocol sample.
This repository has no `testdata/` directory, and adding one needs a reason.

Shared constructors live in `helpers_test.go` as ordinary functions:
`ticketQuestions` builds one question of each type, and `oneQuestion` builds
the smallest valid request.

## 17. Fix flaky tests

A flaky test is a correctness bug. Find and fix the race, shared state, timing
assumption, or uncontrolled dependency.

Do not add retries, longer arbitrary sleeps, quarantine annotations, or a
temporary skip. If an external system is inherently unstable, move that check
out of the deterministic suite.

## 18. Measure meaningful coverage

New and materially changed reachable behavior is completely covered. Run
`make cover`, which prints every function a test does not fully reach, and
report any remaining statements with the reason they stay.

Coverage is measured across every package but `examples/`, so a package
exercised through another package's test counts. The programs under
`examples/` are documentation: the compiler and the linter keep them honest,
and a test that ran them would prove nothing a reader cares about.

Coverage must not fall without an explicit reason. Do not add impossible
branches or brittle tests solely to improve the number.

These statements are uncovered on purpose:

| Statement | Reason |
|---|---|
| `seal` on each answer type | A marker method that seals the `Answer` union has no call site by design. |
| `newRequest`'s request-build failure | `WithBaseURL` rejects a URL `http.NewRequestWithContext` would refuse, so the branch is defensive plumbing. |
| `profileLeaks`'s profile-write failure | The profile is written to a `bytes.Buffer`, which does not fail. |

## 18a. Keep the examples runnable

`example_test.go` and `typesafetest/example_test.go` hold the examples
pkg.go.dev renders. They run as tests, so an example that stops compiling or
stops producing its stated output fails the suite.

An example runs against `typesafetest` and never reaches the network. An
example that builds a client with `typesafe.NewClient(typesafe.WithAPIKey(...))`
and then calls it would send a request to the real API from the test suite;
point it at a fake instead.

Every Go snippet in `docs/usage.md` type-checks against the real API. When the
public surface changes, check the guide too.

## 19. Add benchmarks only for measured questions

Check in a benchmark only when it protects a measured hot path or answers a
named performance question. State the workload and the decision the benchmark
supports. Do not add speculative microbenchmarks as routine coverage.

## 20. Keep tests and helpers clean

Each `_test.go` file covers one domain, and its name matches the production
file it exercises. Split mixed-domain files even when they are short; keep one
coherent domain together even when its file is long.

When a helper is renamed or removed, update every caller and remove the old
helper in the same change.

## 21. Size concurrency explicitly

Concurrency tests state the invariant they protect and control their own
resources. Use explicit channels or barriers to reach the competing state.
Avoid relying on scheduler luck.

`make test` runs with `-race` and `-shuffle=on`, so an ordering assumption
between tests surfaces quickly.

## 22. Detect goroutine leaks at package exit

Every package with tests has a `TestMain` that ends the run with
`leak.CheckAfter`, which reads Go's `goroutineleak` profile and fails the
package when a goroutine outlived the suite.

Check once per package, not once per test. Tests run in parallel, so a
per-test check can observe a goroutine a sibling still owns. The package-exit
check observes only work that survived the whole suite and its cleanups.

The order inside the check matters. The runtime computes the leak profile
during a garbage collection, so the check forces one; and `Count` reports the
last computed total, so it is only accurate after `WriteTo`.

The profile finds goroutines permanently blocked on channels, `select`, and
sync primitives. It does not find goroutines blocked on file or network I/O.
Keep explicit lifecycle and cancellation tests; leak detection complements
them.

## 23. Keep the suite at the correct layer

Unit tests prove local behavior. Tests that drive `Client.SystemOne` against a
server prove the user-facing boundary. Nothing in this repository talks to the
real API, because a test must not need a key, a network, or an account.

Do not make a package test imitate an entire external system when
`typesafetest` already does, and do not reach for an end-to-end test for a
branch a fast deterministic test can cover.
