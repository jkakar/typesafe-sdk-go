# TypeSafe Go SDK - Agent Guidelines

This repository holds `github.com/jkakar/typesafe-sdk-go`, the Go client for
the [TypeSafe AI](https://typesafe.ai) API. It is a published library, so its
exported surface is a contract with people this repository never sees. Read
this file before changing it.

## Read before changing things

- [`docs/design/sdk.md`](docs/design/sdk.md) explains what the SDK exposes,
  why each shape was chosen, and where it diverges from the Python and
  JavaScript SDKs.
- [`docs/testing.md`](docs/testing.md) is the canonical rule set for Go tests.
  Read it completely before writing or reviewing a test.
- The API this SDK calls is documented at <https://docs.typesafe.ai/api>, and
  its OpenAPI schema is served at <https://api.typesafe.ai/openapi.json>.

## Repository layout

The SDK is one package at the repository root, `typesafe`, the way
`anthropic-sdk-go` and `openai-go` lay out theirs. A caller writes
`typesafe.NewClient`, not `sdk.New`.

```
client.go        Client, options, configuration
question.go      Question types and their validation
answer.go        Answer types and the accessors that read them
error.go         APIError, DecodeError, and the status sentinels
retry.go         RetryPolicy, backoff, retry-after parsing
transport.go     Request building, the retry loop, logging
systemone.go     POST /v1/systemone
model.go         GET /v1/models
json.go          The tagged-union encoding both endpoints share
typesafetest/    A fake TypeSafe API for tests, this SDK's and yours
cmd/lint-tests/  The mechanical guard for docs/testing.md
internal/leak/   Goroutine leak detection for TestMain
internal/testpolicy/  The checks lint-tests runs
```

Add a file when a new domain arrives, not a new layer. An SDK with two
endpoints does not need service, model, and integration packages.

## Public surface

**The exported API is the product.** Adding to it is cheap and removing from
it is not. Before exporting a type, function, or field, ask whether a caller
needs it to do something they cannot already do.

**Keep `Question` and `Answer` sealed.** Each interface has an unexported
method, so only this package implements it and a type switch over the concrete
types stays exhaustive. `RawQuestion` and `UnknownAnswer` are how a caller
reaches a type this SDK version does not model; they are the escape hatch, and
a new one is not needed.

**Accept `any` where the API accepts JSON.** State, instructions, and criteria
take text, a JSON object, or a JSON array. A caller passes a string, a map, a
slice, or a struct with JSON tags, and the SDK validates the encoded shape.

**Document every exported identifier.** Follow <https://go.dev/doc/comment>.
Link to the concept a type implements, such as
<https://docs.typesafe.ai/primitives/noul>, rather than restating it.

**Version the SDK in one place.** `Version` in `version.go` reaches the
`User-Agent` and `X-TypeSafe-SDK` headers. Bump it in the release commit.

## Code style

**Pass `context.Context` as the first parameter to every function that does
I/O.** Name the parameter `ctx`. Put variadic options last.

**Group imports in three blocks: standard library, external, this module.**
Separate each group with a blank line.

```go
import (
	"context"
	"net/http"

	"github.com/alecthomas/assert/v2"

	"github.com/jkakar/typesafe-sdk-go"
)
```

**Read the environment in `FromEnv` and nowhere else.** Every other function
takes typed configuration. This keeps the call graph testable, keeps
`NewClient` honest about what it depends on, and is the partner rule to
[`docs/testing.md`](docs/testing.md) rule 12.

**Use `log/slog` for logging, and never log a credential.** Pass key-value
pairs; do not interpolate values into the message. Headers reach a log through
`redacted`, which is the single redaction point.

**Use `//nolint:<linter>` with an explanation.** A suppressed linter needs a
reason a reader can check.

**Format Markdown to read at 80 characters.** Wrap prose and list items.
Headings, tables, URLs, and code may run longer when breaking them would hurt
readability.

## Error handling

**Expected failures are sentinels or typed errors. Unexpected failures are
wrapped plain errors.**

Use a sentinel when a caller only distinguishes the kind of failure, and a
typed error when they need structured data. This SDK does both at once:
`*APIError` carries the status, message, request ID, headers, and body, and
unwraps to the sentinel for its status. A caller writes
`errors.Is(err, typesafe.ErrRateLimit)` or `errors.As(err, &apiErr)`.

Wrap an unexpected failure with `fmt.Errorf("...: %w", err)` and keep the
cause reachable. A transport failure keeps its `*url.Error`, so
`errors.Is(err, context.DeadlineExceeded)` still works.

**Error messages are lowercase with no trailing punctuation.** Write
`errors.New("no api key")`, not `errors.New("No API key.")`.

**Never check errors by string matching.**

**Do not add error handling for a case that cannot happen.** When a constructor
already rejected the input, the later check is noise. When it is unavoidable
because the standard library returns an error, say so in a comment and record
it in [`docs/testing.md`](docs/testing.md) rule 18.

## Testing

**Canonical reference: [`docs/testing.md`](docs/testing.md).** Read it
completely before writing or reviewing a test.

Follow red-green-refactor: write the test first, observe the assertion
failure, then write the minimum code to pass. Run `make check` before opening
a pull request and `make cover` to see what no test reaches. Mechanical guards
live in `cmd/lint-tests`.

`typesafetest` is part of the product, not scaffolding. A program that calls
this SDK tests itself against `typesafetest.NewServer`, so changes there are
changes to the public surface.

## Tooling

The root Makefile is the stable developer interface:

| Target | What it does |
|---|---|
| `make check` | Everything CI runs: formatting, tidiness, test policy, vet, lint, tests |
| `make fix` | Format the source and tidy the module |
| `make test` | Tests with the race detector, shuffling, leak detection, and coverage |
| `make cover` | The statements no test reaches |
| `make lint-tests` | The mechanical rules in `docs/testing.md` |

Use Go 1.27 for every build and CI job. The module depends on
`github.com/alecthomas/assert/v2` and nothing else, and it stays that way: a
dependency this SDK takes is a dependency every program that imports it takes.
`golangci-lint` is a developer tool, installed separately, not a module
requirement.

CI runs `make check` less the lint step, which the golangci-lint action runs
with its own cache. Do not duplicate checks in workflow YAML.

## Documentation

- `README.md` covers installation, the first call, and the shape of the API.
  Update it when the public surface changes.
- `docs/design/sdk.md` records the design and its trade-offs. Update it in the
  same commit as the behavior it describes.
- `docs/testing.md` is the testing doctrine. A new mechanical rule goes there
  and in `internal/testpolicy` together.
- Package documentation lives in `doc.go`. Examples that a reader should be
  able to run live in `example_test.go`, where they run as tests.

Do not create a roadmap, progress log, or status file. Open an issue for
unfinished work.

## Git workflow and pull requests

- Never commit directly to `main`.
- Name branches `jkakar/<short-feature-name>`.
- Keep commits focused. Preserve unrelated user changes.
- Merge pull requests with GitHub's squash merge.
- Prefix commit and pull request titles with the area they change, such as
  `client:`, `retry:`, `typesafetest:`, `docs:`, or `deps:`.
- Write an imperative title of about 72 characters or fewer.
- Do not hard-wrap pull request descriptions. GitHub wraps them.
- Structure pull request descriptions with these sections:

  ```markdown
  ## Problem

  What is missing, broken, or motivating the change.

  ## Solution

  What changed and why this approach was chosen.

  ## Test plan

  What proves it works.
  ```

- Pipe commit messages and pull request bodies from a single-quoted heredoc
  into stdin. Never use a shared temporary file path: paths under `$TMPDIR`
  persist across sessions and have silently delivered stale content.

  ```sh
  git commit -F - <<'EOF'
  retry: Honor retry-after-ms over retry-after

  Body paragraph here.
  EOF
  ```
