# The TypeSafe SDK for Go

## Purpose

This document describes the Go client for the TypeSafe AI API and why it has
the shape it has. It is the reference for anyone extending the SDK, and the
reasoning behind the places where it does not look like the Python or
JavaScript SDK. Update it in the same commit as the behavior it describes.

The SDK has parity of capability with those SDKs, not parity of form. A Go
program does everything a Python or JavaScript program does, using the idioms
a Go reader already knows: a context first, an explicit struct, two return
values, `errors.Is`, and a type switch.

## The API

The whole API is two endpoints, documented at <https://docs.typesafe.ai/api>.

`POST /v1/systemone` evaluates one `state` against a map of named `questions`
and returns one `answer` per question, keyed by the same names, plus the model
that answered and the tokens it used. `GET /v1/models` lists the model names
and aliases the account can send.

A question is one of three types, chosen by its `type` field. A **noul** asks
a yes/no question and its answer is the probability of yes. A **choice**
selects one option from a set and its answer names the most probable option,
the probability of every option, and a confidence. A **score** rates the state
against an ordered rubric and its answer is the probability-weighted average
of the levels, which may fall between them, with the rubric and a confidence.

`state`, `instructions`, and every criterion accept text, a JSON object, or a
JSON array. Answers are tagged unions: each carries the `type` of the question
it answers.

Failures use HTTP status codes: 401 for a bad key, 422 for a body that failed
validation, 429 for a rate limit, and 529 for an overloaded service. The
documentation asks clients to retry 429 and 529 with exponential backoff.

There is no streaming, no pagination, and no webhook. The API surface is
small, and so is the SDK's.

## What the other SDKs do

Both official SDKs wrap those two endpoints and add the same eight things:
configuration from the environment, a default model, question constructors,
typed answers, a class per HTTP failure, retries with backoff that honor
`retry-after`, logging with credential redaction, and access to the raw HTTP
response and the request ID.

They differ where their languages differ. Python ships a synchronous and an
asynchronous client, accepts questions as objects or as plain dictionaries,
and groups answers by type on the response (`response.choices["category"]`).
JavaScript infers each answer's type from the question that produced it, so
`response.answers.category.choice` is typed without a cast, and returns an
`APIPromise` that also yields the raw `Response`. Both read
`TYPESAFE_API_KEY`, `TYPESAFE_BASE_URL`, `TYPESAFE_DEFAULT_MODEL`, and
`TYPESAFE_LOG_LEVEL` from the environment.

## The Go SDK

### Package and construction

One package at the repository root, imported as
`github.com/jkakar/typesafe-sdk-go` and named `typesafe`, matching
`anthropic-sdk-go` and `openai-go`.

```go
func NewClient(opts ...Option) (*Client, error)

func WithAPIKey(key string) Option
func WithBaseURL(rawURL string) Option
func WithModel(name string) Option
func WithHTTPClient(client *http.Client) Option
func WithHeader(name, value string) Option
func WithRetryPolicy(policy RetryPolicy) Option
func WithLogger(logger *slog.Logger) Option
func FromEnv() Option
func FromEnvFunc(lookup func(name string) (value string, ok bool)) Option
```

One `Option` type configures the client and a single call, so a caller learns
one vocabulary:

```go
resp, err := client.SystemOne(ctx, req, typesafe.WithModel("jev-1.13.0"))
```

A per-call option applies to a copy of the client's configuration and never
reaches the client, which keeps a `*Client` safe for concurrent use.

### Asking questions

```go
resp, err := client.SystemOne(ctx, typesafe.Request{
	State: "I was charged twice. Please fix this ASAP.",
	Questions: typesafe.Questions{
		"urgent": typesafe.NoulQuestion{
			Instructions: "Does this convey urgency?",
			Criteria:     &typesafe.NoulCriteria{True: "Time sensitive"},
		},
		"department": typesafe.ChoiceQuestion{
			Instructions: "Which team should handle this?",
			Criteria: typesafe.ChoiceCriteria{
				"billing":   "Payments, invoicing, refunds",
				"technical": "Bugs, outages, integrations",
			},
		},
		"frustration": typesafe.ScoreQuestion{
			Instructions: "How frustrated is the customer?",
			Criteria:     typesafe.ScoreCriteria{"Calm", "Frustrated", "Very angry"},
		},
	},
})
```

Questions are struct literals, not constructor calls. Go's named fields
already give the readability that `choice(instructions, criteria)` gives
JavaScript, and a struct extends without a new positional argument.
`ChoiceCriteria` and `ScoreCriteria` are named map and slice types, so a
caller writes the shape the API documents instead of `map[string]any`.

`Question` is a sealed interface: it has one unexported method, so only this
package implements it. The compiler therefore knows the set of question types
is closed, which is what makes a type switch over them honest.

### Reading answers

```go
type Response struct {
	Model        string
	Answers      Answers
	Usage        Usage
	RequestID    string
	HTTPResponse *http.Response
}

func (a Answers) Noul(name string) (NoulAnswer, error)
func (a Answers) Choice(name string) (ChoiceAnswer, error)
func (a Answers) Score(name string) (ScoreAnswer, error)
```

JavaScript's typed answers come from mapped types over the question literal,
which Go's type system cannot express. Two Go answers replace it.

The accessors are the common path. `resp.Answers.Choice("department")` returns
a `ChoiceAnswer` or an error: `ErrNoAnswer` when the name is absent and an
`*AnswerTypeError` naming both types when the answer is something else. That
is one line and one error check, not a cast and a comment.

A type switch is the other path, for code that routes on whatever came back:

```go
switch answer := resp.Answers["department"].(type) {
case typesafe.ChoiceAnswer:
	route(answer.Choice)
case typesafe.UnknownAnswer:
	log.Printf("unhandled answer type %q", answer.Type)
}
```

`UnknownAnswer` keeps an answer type this SDK version does not model, with its
JSON intact, so a newer API does not break an older client. `RawQuestion` is
the same idea in the other direction: it sends a question type this SDK does
not name yet, which is the capability Python gets from accepting raw
dictionaries.

Score answers key their legend and probabilities by `int`, not by the strings
the wire uses. A rubric level is a number, and a caller who has to write
`probabilities["2"]` is being made to remember an encoding detail.

### Errors

```go
var (
	ErrBadRequest, ErrAuthentication, ErrPermissionDenied error
	ErrNotFound, ErrUnprocessable, ErrRateLimit           error
	ErrOverloaded, ErrServer, ErrAPI                      error
)

type APIError struct {
	Method, URL string
	StatusCode  int
	Message     string
	RequestID   string
	RetryAfter  time.Duration
	Header      http.Header
	Body        []byte
}

func (e *APIError) Unwrap() error
```

Python and JavaScript define a class per status and ask callers to catch the
one they care about. Go has no exception hierarchy, and nine error types trade
one problem for a worse one.

One typed error carries the detail and unwraps to a sentinel naming the kind.
A caller who only wants to know what happened writes
`errors.Is(err, typesafe.ErrRateLimit)`. A caller who wants the request ID for
a support ticket writes `errors.As(err, &apiErr)`. `Unwrap` derives the
sentinel from `StatusCode`, so the struct has no hidden field and a test can
build one.

`*DecodeError` reports a successful response whose body the SDK could not
read, and unwraps to the decoding failure. `ErrNoAPIKey`, `ErrNoQuestions`,
`ErrNoCriteria`, `ErrInvalidState`, and `ErrInvalidRetryPolicy` report a
request or a client the API would reject, before a round trip.

### Retries, timeouts, and cancellation

```go
type RetryPolicy struct {
	MaxRetries        int
	InitialBackoff    time.Duration
	MaxBackoff        time.Duration
	Jitter            float64
	RespectRetryAfter bool
	MaxRetryAfter     time.Duration
	RetryStatus       func(statusCode int) bool
	RetryError        func(err error) bool
}

func DefaultRetryPolicy() RetryPolicy
```

The defaults match the other SDKs: two retries, 500ms doubling to five
seconds, a quarter of jitter, and a server delay honored up to a minute.

Python's policy has eight fields for deciding what to retry: a status set, two
booleans for connection and timeout errors, an exception set, and a predicate.
Two functions replace all of them. `RetryStatus` and `RetryError` default to
nil, which means "408, 429, and 5xx" and "everything except a canceled or
expired context"; a caller who wants something else writes it.

There is no timeout field. Go already has two clocks and they mean different
things: the context bounds the whole call, including retries and the waits
between them, and `http.Client.Timeout` bounds one attempt. A third knob would
only be ambiguous. `DefaultTimeout` is what the default HTTP client uses, and
`WithHTTPClient` replaces it.

### Logging

`WithLogger(*slog.Logger)` takes the standard library's logger. A client
without one logs nothing, which is the right default for a library. Request
summaries log at info and full wire detail at debug. Credential headers are
redacted in one place, so no call site can leak a key.

There is no `TYPESAFE_LOG_LEVEL`. A Go program configures its logger once, at
its edge, and a library that reconfigures logging from the environment is
taking a decision that belongs to the program.

### Testing support

`typesafetest` is a fake TypeSafe API backed by `httptest.Server`. Without a
handler it answers every question with a zero-information answer, which is
enough to exercise a program's own logic:

```go
srv := typesafetest.NewServer()
defer srv.Close()
srv.HandleSystemOne(func(_ context.Context, req typesafe.Request) (typesafe.Response, error) {
	return typesafe.Response{
		Answers: typesafe.Answers{"urgent": typesafe.NoulAnswer{Noul: 0.92}},
	}, nil
})

client := srv.Client()
```

Neither the Python nor the JavaScript SDK ships one. A program that calls an
AI API needs to test what it does with the answers, and today every such
program writes the same fake. `Server.Recorded` returns the requests it
received, and `Failure` makes it return any status, so a caller can prove
their rate-limit handling without a rate limit.

This is also how the SDK tests itself, which keeps the fake honest: it decodes
real request bodies with the SDK's own types and encodes real responses.

## Divergences, and why

| Decision | Python and JavaScript | Go | Reason |
|---|---|---|---|
| Environment | `TypeSafeClient()` reads `TYPESAFE_API_KEY` | `NewClient(typesafe.FromEnv())` | A library that reads ambient state is hard to test and hard to reason about. One option makes the dependency visible, and `FromEnvFunc` makes it injectable. |
| Async | A second `AsyncTypeSafeClient` | One client | A goroutine is already the concurrency primitive, so a second client is the same code twice. |
| Errors | A class per status | One typed error plus sentinels | `errors.Is` and `errors.As` cover both needs without nine types. |
| Timeouts | A client timeout and a retry budget | The context, and `http.Client.Timeout` | Go programs already bound work with a context. |
| Log level | `TYPESAFE_LOG_LEVEL` | `WithLogger(*slog.Logger)` | The program owns its logging. |
| Raw response | `withResponse()`, `raw_http_response` | `Response.HTTPResponse` | A field costs nothing and needs no wrapper type. |
| Answer access | Typed by inference, or grouped by type | Typed accessors and a sealed union | Go cannot infer the mapping, so it makes the check explicit and cheap. |
| Score keys | String keys, as on the wire | `map[int]...` | A rubric level is a number. |
| Test support | None | `typesafetest` | Every caller writes this fake otherwise. |

## What this SDK does not do

**No streaming or pagination.** The API has neither.

**No prompt or question helpers beyond the three primitives.** The patterns in
<https://docs.typesafe.ai/patterns> are code a caller writes. Speculative
fan-out is a map literal with more entries, and confidence-gated routing is an
`if`. Wrapping them would add API surface without adding capability.

**No generated code.** The API is small enough to model by hand, and a
handwritten type can carry the documentation and the `int` keys that a
generator would not.

**No dependencies beyond the test assertion library.** Retries, backoff, JSON,
and logging are all in the standard library. A dependency this SDK takes is a
dependency every program that imports it takes.

## Decisions worth revisiting

These three are settled, and each rests on an assumption that could change.

- **Score criteria.** The OpenAPI schema requires at least one level and the
  prose documentation asks for at least two. The SDK enforces the schema, so
  it never rejects a request the server accepts, and the doc comment for
  `ScoreQuestion.Criteria` says so. If the server starts rejecting one level,
  the SDK follows it.
- **Release date typing.** `Model.ReleaseDate` is the string the API sends.
  Parsing it into a `time.Time` needs a layout, and the format is not settled:
  the published schema documents `YYYY-MM-DD` while the API returns an RFC
  3339 timestamp. A layout picked from the schema would fail every call. A
  string also cannot fail the whole list for one malformed date, on an
  endpoint that is otherwise informational.
- **Retrying a POST.** `SystemOne` retries, as the other SDKs do. Evaluation
  has no side effect, so a repeated request costs tokens and nothing else. A
  future endpoint that does have a side effect needs its own answer.
