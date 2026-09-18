# Using the TypeSafe SDK for Go

This guide covers the whole SDK, in the order you meet it. The
[README](../README.md) is the five-minute version, the [package
documentation](https://pkg.go.dev/github.com/jkakar/typesafe-sdk-go) is the
reference, and [`docs/design/sdk.md`](design/sdk.md) explains why the API has
the shape it has.

For what the model does and how to write good questions, read the [TypeSafe
documentation](https://docs.typesafe.ai/). This guide covers the Go side.

## Contents

- [Install and authenticate](#install-and-authenticate)
- [The shape of a call](#the-shape-of-a-call)
- [State](#state)
- [Questions](#questions)
- [Ask everything at once](#ask-everything-at-once)
- [Reading answers](#reading-answers)
- [Confidence](#confidence)
- [Errors](#errors)
- [Retries, timeouts, and cancellation](#retries-timeouts-and-cancellation)
- [Logging](#logging)
- [Testing your code](#testing-your-code)
- [Choosing a model](#choosing-a-model)
- [When the API adds something](#when-the-api-adds-something)

## Install and authenticate

```sh
go get github.com/jkakar/typesafe-sdk-go
```

A client needs an API key. `FromEnv` reads `TYPESAFE_API_KEY`,
`TYPESAFE_BASE_URL`, and `TYPESAFE_DEFAULT_MODEL` from the process
environment:

```go
client, err := typesafe.NewClient(typesafe.FromEnv())
```

Unlike the Python and JavaScript SDKs, `NewClient` does not read the
environment unless you ask it to. The option makes the dependency visible, so
a program that configures its client from a secret store, a config file, or a
flag has one obvious place to do it:

```go
client, err := typesafe.NewClient(typesafe.WithAPIKey(secret))
```

`FromEnvFunc` takes the same variables from anywhere:

```go
client, err := typesafe.NewClient(typesafe.FromEnvFunc(vault.Lookup))
```

Every other option is explicit:

```go
client, err := typesafe.NewClient(
	typesafe.FromEnv(),
	typesafe.WithModel("jev-1.13.0"),
	typesafe.WithHTTPClient(&http.Client{Timeout: 30 * time.Second}),
	typesafe.WithHeader("X-Request-Source", "support-triage"),
	typesafe.WithRetryPolicy(policy),
	typesafe.WithLogger(slog.Default()),
)
```

A `*Client` is safe for concurrent use. Build one for the life of the program.

The same options override a single call, applied to a copy of the client's
configuration:

```go
resp, err := client.SystemOne(ctx, req, typesafe.WithModel("jev-preview"))
```

## The shape of a call

Every call to `SystemOne` sends one `State` and a map of named `Questions`,
and returns one answer per question under the same names:

```go
resp, err := client.SystemOne(ctx, typesafe.Request{
	State: "I was charged twice. Please fix this ASAP.",
	Questions: typesafe.Questions{
		"urgent": typesafe.NoulQuestion{Instructions: "Does this convey urgency?"},
	},
})
```

You choose the names. They identify the answers and are not sent to the model,
so name them for your code, not for the model.

The response also carries the model that answered, the tokens the request
used, the request ID, and the underlying HTTP response:

```go
log.Printf("%s answered in %d tokens (request %s)",
	resp.Model, resp.Usage.InputTokens, resp.RequestID)
```

`resp.Model` is the versioned name, such as `jev-1.13.0`, even when the request
named an alias. Log it if you tune thresholds, because an alias moves.

## State

`State` is the content every question refers to. It takes text, a JSON object,
or a JSON array, so a string, a map, a slice, or a struct with JSON tags all
work:

```go
State: "I was charged twice."

State: map[string]any{"subject": "Duplicate charge", "body": "..."}

State: []Message{{From: "customer", Text: "..."}, {From: "agent", Text: "..."}}

State: Ticket{ID: 42, Subject: "Duplicate charge"}  // with json tags
```

Structure helps the model tell one field from another, so prefer it over
pasting fields into one string. See
[State](https://docs.typesafe.ai/concepts/state).

Anything else is a mistake the SDK catches before the round trip:

```go
_, err := client.SystemOne(ctx, typesafe.Request{State: 42, Questions: questions})
// err is ErrInvalidState: state must encode as a JSON string, object, or array
```

## Questions

Three question types cover everything the API does. See
[Primitives](https://docs.typesafe.ai/primitives).

### Noul: yes or no

The answer is the probability that the answer is yes, from zero to one. Values
near a half mean the model is unsure.

```go
typesafe.NoulQuestion{
	Instructions: "Does this ticket need attention today?",
	Criteria: &typesafe.NoulCriteria{
		True:  "Money or data is at risk, or the customer is blocked",
		False: "The customer can wait for the normal queue",
	},
}
```

`Criteria` is optional and is where boundary cases go. A question that keeps
surprising you usually needs its criteria written down, not a longer
instruction.

### Choice: one of a set

The answer names the most probable option, gives a probability for every
option, and reports a confidence.

```go
typesafe.ChoiceQuestion{
	Instructions: "Which team should handle this ticket?",
	Criteria: typesafe.ChoiceCriteria{
		"billing":   "Payments, invoicing, refunds, subscriptions",
		"technical": "Bugs, outages, integrations, performance",
		"other":     nil,
	},
}
```

A `nil` description leaves the option to be read from its name alone. Use it
when the name says everything, and write a description when it does not.

### Score: a rating on a rubric

The answer is the probability-weighted average of the levels, so it can fall
between them. `1.6` on a three-level rubric means the model is between
"Frustrated" and "Very angry", closer to angry.

```go
typesafe.ScoreQuestion{
	Instructions: "How frustrated is the customer?",
	Criteria:     typesafe.ScoreCriteria{"Calm", "Frustrated", "Very angry"},
}
```

Levels are ordered and count from zero. The answer carries the rubric back in
`Legend`, so code that formats the score does not have to remember the
request.

### Structure, not just text

`Instructions` and every criterion take the same shapes `State` does. Reach
for structure when a question has rules:

```go
typesafe.NoulQuestion{
	Instructions: map[string]any{
		"task":    "Decide whether this message is unsolicited advertising.",
		"ignore":  []string{"receipts", "password resets"},
		"examples": []map[string]string{
			{"text": "Claim your prize", "answer": "yes"},
		},
	},
}
```

See [Advanced: structure](https://docs.typesafe.ai/primitives/advanced).

### What the SDK rejects

The SDK checks what it can before spending a round trip:

| Mistake | Error |
|---|---|
| No questions | `ErrNoQuestions` |
| A choice or score question with empty criteria | `ErrNoCriteria` |
| State that is not text, an object, or an array | `ErrInvalidState` |

`ErrNoCriteria` names the question, so a request with twenty questions still
tells you which one is wrong.

## Ask everything at once

The model reads the state once and evaluates every question against it in
parallel. Four questions in one call cost far less and finish far sooner than
four calls, so ask everything you might want, including questions you will
probably ignore:

```go
resp, err := client.SystemOne(ctx, typesafe.Request{
	State: ticket,
	Questions: typesafe.Questions{
		"urgent":   typesafe.NoulQuestion{Instructions: "Does this convey urgency?"},
		"refund":   typesafe.NoulQuestion{Instructions: "Is the customer asking for money back?"},
		"security": typesafe.NoulQuestion{Instructions: "Does this report a security problem?"},
		"churn":    typesafe.NoulQuestion{Instructions: "Is the customer threatening to leave?"},
	},
})
```

One budget covers the state plus every question, so the ceiling is the
context length rather than a question count. See [Speculative
fan-out](https://docs.typesafe.ai/patterns/fan-out) and
[Models](https://docs.typesafe.ai/models) for the current limits.

## Reading answers

Read an answer by name and type. Each accessor returns the typed answer or an
error:

```go
urgent, err := resp.Answers.Noul("urgent")
department, err := resp.Answers.Choice("department")
frustration, err := resp.Answers.Score("frustration")
```

| Failure | Error |
|---|---|
| The response has no answer under that name | `ErrNoAnswer` |
| The answer has another type | `*AnswerTypeError`, naming both types |

An `*AnswerTypeError` almost always means the question and the accessor
drifted apart, so treat it as a bug rather than a condition to handle:

```go
department, err := resp.Answers.Choice("department")
if err != nil {
	return fmt.Errorf("read the department: %w", err)
}
```

When the code handles whatever came back rather than a question it named,
switch over the answers instead:

```go
switch answer := resp.Answers[name].(type) {
case typesafe.NoulAnswer:
	record(name, answer.Noul)
case typesafe.ChoiceAnswer:
	record(name, answer.Probabilities[answer.Choice])
case typesafe.ScoreAnswer:
	record(name, answer.Score)
case typesafe.UnknownAnswer:
	log.Printf("unhandled %q answer for %s", answer.Type, name)
}
```

The `Answer` interface is sealed: only this package implements it, so that
switch covers every case the SDK can produce.

Score legends and probabilities are keyed by `int`, not by the strings the
wire uses, because a rubric level is a number:

```go
fmt.Println(frustration.Legend[2])        // "Very angry"
fmt.Println(frustration.Probabilities[2]) // 0.65
```

## Confidence

Choice and score answers carry a `Confidence` from zero to one, derived from
the probability distribution. It answers a different question than the answer
does: the answer tells you what, and confidence tells you whether to act. See
[Confidence](https://docs.typesafe.ai/confidence).

```go
if department.Confidence < 0.8 {
	return escalate(ticket)
}
return route(ticket, department.Choice)
```

The threshold belongs to your program, not to the model. Pick it from what a
mistake costs: a misrouted ticket is cheap, an incorrect refund is not.

A noul reports no separate confidence, because its answer already is one. A
value near a half is the uncertain case:

```go
if math.Abs(urgent.Noul-0.5) < 0.2 {
	return escalate(ticket)
}
```

Thresholds are tuned against a specific model version. `resp.Model` reports
the version that answered, so log it, and pin the version instead of the alias
once the thresholds matter. See [Confidence-gated
routing](https://docs.typesafe.ai/patterns/confidence-routing).

## Errors

Every unsuccessful response becomes an `*APIError` that unwraps to a sentinel
naming the kind of failure. Match the kind with `errors.Is` and read the
detail with `errors.As`:

```go
if errors.Is(err, typesafe.ErrRateLimit) {
	// The client already retried with backoff and still failed.
}

var apiErr *typesafe.APIError
if errors.As(err, &apiErr) {
	log.Printf("request %s failed: %d %s",
		apiErr.RequestID, apiErr.StatusCode, apiErr.Message)
}
```

`APIError` carries the method and URL, the status, the server's message, the
request ID, the `RetryAfter` the server asked for, and the response headers
and body.

| Sentinel | Status | What to do |
|---|---|---|
| `ErrAuthentication` | 401 | Check the API key. Do not retry. |
| `ErrPermissionDenied` | 403 | The key lacks access. Do not retry. |
| `ErrNotFound` | 404 | Check the base URL. |
| `ErrUnprocessable` | 422 | The request is wrong. `Message` names the field. |
| `ErrRateLimit` | 429 | Already retried. Slow down or raise the limit. |
| `ErrOverloaded` | 529 | Already retried. Try again later. |
| `ErrServer` | 5xx | Already retried. Quote `RequestID` to support. |
| `ErrBadRequest` | 400 | The request is wrong. |
| `ErrAPI` | other | An unsuccessful status with no more specific kind. |

Three other failures are not `*APIError`:

- A `*DecodeError` reports a successful response whose body the SDK could not
  read. It unwraps to the decoding failure and carries the body.
- A transport failure is wrapped and keeps its cause, so
  `errors.Is(err, context.DeadlineExceeded)` and `errors.As` for a `*url.Error`
  both work.
- `ErrNoQuestions`, `ErrNoCriteria`, `ErrInvalidState`, `ErrNoAPIKey`,
  `ErrInvalidBaseURL`, and `ErrInvalidRetryPolicy` report a request or a client
  the SDK rejected before sending anything.

## Retries, timeouts, and cancellation

The client retries by default: twice, waiting 500ms and then a second, with a
quarter of jitter, honouring `retry-after` up to a minute. It retries 408, 429
and 5xx responses and transport failures, and never retries a canceled or
expired context.

Start from `DefaultRetryPolicy` and change what you need:

```go
policy := typesafe.DefaultRetryPolicy()
policy.MaxRetries = 5
policy.MaxBackoff = 30 * time.Second

client, err := typesafe.NewClient(typesafe.FromEnv(), typesafe.WithRetryPolicy(policy))
```

The zero value retries nothing, so build from `DefaultRetryPolicy` rather than
from `typesafe.RetryPolicy{}`.

Two functions decide what gets retried. Leave them nil for the defaults above:

```go
policy.RetryStatus = func(status int) bool {
	return status == http.StatusGatewayTimeout || status >= http.StatusInternalServerError
}
policy.RetryError = func(err error) bool {
	return !errors.Is(err, context.Canceled)
}
```

Turn retries off for one call when the caller is waiting and a slow failure is
worse than a fast one:

```go
off := typesafe.DefaultRetryPolicy()
off.MaxRetries = 0
resp, err := client.SystemOne(ctx, req, typesafe.WithRetryPolicy(off))
```

There is no timeout option, because Go already has two clocks and they mean
different things:

| Bound | How |
|---|---|
| The whole call, including retries and the waits between them | The context you pass |
| One HTTP attempt | The `Timeout` of the client given to `WithHTTPClient` |

```go
ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
defer cancel()
resp, err := client.SystemOne(ctx, req)
```

The default HTTP client allows ten seconds per attempt. Raise it for a large
state or a long question set.

## Logging

Pass a `*slog.Logger` and the client logs; pass none and it says nothing,
which is the right default for a library:

```go
client, err := typesafe.NewClient(typesafe.FromEnv(), typesafe.WithLogger(slog.Default()))
```

| Level | What it records |
|---|---|
| Info | One line per attempt: method, URL, status, duration, request ID. Retries, with the delay and the reason. |
| Debug | The request and response headers and bodies. |

Credential headers are redacted in one place inside the SDK, so debug logging
cannot leak a key. Request and response bodies are not redacted, so debug
logging a request whose state holds personal data writes that data to your
logs.

There is no log-level environment variable. Your program configures its logger
once, at its edge, and the SDK writes to whatever it is given.

## Testing your code

`typesafetest` runs a fake TypeSafe API, so your tests need no key, no
network, and no account:

```go
func TestRouteTicket(t *testing.T) {
	srv := typesafetest.NewServer()
	t.Cleanup(srv.Close)
	srv.HandleSystemOne(func(_ context.Context, req typesafe.Request) (typesafe.Response, error) {
		return typesafe.Response{Answers: typesafe.Answers{
			"department": typesafe.ChoiceAnswer{Choice: "billing", Confidence: 0.91},
		}}, nil
	})

	queue, err := routeTicket(t.Context(), srv.Client(), "I was charged twice.")

	assert.NoError(t, err)
	assert.Equal(t, "billing", queue)
}
```

`srv.Client()` returns a client pointed at the fake, using the server's own
HTTP client so the test owns its connection pool.

Without a handler the server answers every question with a zero-information
answer: a noul of one half, the first option of a choice, the middle level of
a score, each with a uniform distribution and no confidence. That is enough
when the test cares that the call happened.

The handler receives the decoded `typesafe.Request`, so a test can assert on
the questions the code asked:

```go
srv.HandleSystemOne(func(_ context.Context, req typesafe.Request) (typesafe.Response, error) {
	assert.Equal(t, 4, len(req.Questions))
	return typesafe.Response{Answers: typesafe.Answers{
		"department": typesafe.ChoiceAnswer{Choice: "billing", Confidence: 0.91},
	}}, nil
})
```

`srv.Recorded()` returns every request the server received, with its headers
and raw body, for assertions about what went on the wire.

Return a `Failure` to produce any status, which is how you test rate-limit or
outage handling without one:

```go
srv.HandleSystemOne(func(context.Context, typesafe.Request) (typesafe.Response, error) {
	return typesafe.Response{}, &typesafetest.Failure{
		StatusCode: http.StatusTooManyRequests,
		Header:     http.Header{"Retry-After": []string{"30"}},
	}
})
```

Remember that the client retries a 429 by default, so a test that wants the
failure to reach the caller passes a policy with `MaxRetries` set to zero.

## Choosing a model

`ListModels` returns the names the account can send:

```go
models, err := client.ListModels(ctx)
for _, model := range models {
	fmt.Println(model.Name, model.ReleaseDate, model.Description)
}
```

The list holds the aliases, `jev-latest` and `jev-preview`. A versioned name
such as `jev-1.13.0` works whether or not it appears there.

An alias moves when a release ships, so the answers behind it change without a
change on your side. If you have tuned confidence thresholds, pin the version
and move on your own schedule:

```go
client, err := typesafe.NewClient(typesafe.FromEnv(), typesafe.WithModel("jev-1.13.0"))
```

`Model.ReleaseDate` is the string the API sends, formatted as `YYYY-MM-DD`.
Parse it if you need to compare dates.

## When the API adds something

The SDK does not break when the API grows a question or answer type it does
not name.

An answer type it does not model arrives as an `UnknownAnswer`, with its JSON
intact:

```go
if unknown, ok := resp.Answers["ranking"].(typesafe.UnknownAnswer); ok {
	var ranking Ranking
	if err := json.Unmarshal(unknown.Raw, &ranking); err != nil {
		return err
	}
}
```

A `RawQuestion` sends a question type this version does not name, exactly as
you write it:

```go
Questions: typesafe.Questions{
	"ranking": typesafe.RawQuestion{
		Type: "rank",
		JSON: json.RawMessage(`{"type":"rank","instructions":"Order by relevance."}`),
	},
}
```

Both are escape hatches for the gap between an API release and an SDK release.
When the SDK names the type, move to it: the typed form validates its criteria
and decodes its answer for you.
