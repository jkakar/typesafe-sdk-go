# TypeSafe SDK for Go

Go client for the [TypeSafe AI](https://typesafe.ai) API.

TypeSafe serves System One models, which answer typed questions about a piece
of state and return calibrated probabilities instead of prose. Ask a question,
get a number your code can act on.

```sh
go get github.com/jkakar/typesafe-sdk-go
```

[Usage guide](docs/usage.md) · [Examples](examples) ·
[Reference](https://pkg.go.dev/github.com/jkakar/typesafe-sdk-go)

## Quickstart

Set `TYPESAFE_API_KEY` in your environment, then ask a question:

```go
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/jkakar/typesafe-sdk-go"
)

func main() {
	client, err := typesafe.NewClient(typesafe.FromEnv())
	if err != nil {
		log.Fatal(err)
	}

	resp, err := client.SystemOne(context.Background(), typesafe.Request{
		State: "I was charged twice. Please fix this ASAP.",
		Questions: typesafe.Questions{
			"department": typesafe.ChoiceQuestion{
				Instructions: "Which team should handle this?",
				Criteria: typesafe.ChoiceCriteria{
					"billing":   "Payments, invoicing, refunds",
					"technical": "Bugs, outages, integrations",
					"other":     nil,
				},
			},
		},
	})
	if err != nil {
		log.Fatal(err)
	}

	department, err := resp.Answers.Choice("department")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(department.Choice, department.Confidence)
}
```

## Overview

### Questions

Three question types cover everything the API does. See
[Primitives](https://docs.typesafe.ai/primitives).

```go
// Yes/no. The answer is the probability of yes.
typesafe.NoulQuestion{
	Instructions: "Does this convey urgency?",
	Criteria:     &typesafe.NoulCriteria{True: "Time sensitive", False: "No urgency"},
}

// One option from a set. The answer names the most probable option.
typesafe.ChoiceQuestion{
	Instructions: "Which team should handle this?",
	Criteria:     typesafe.ChoiceCriteria{"billing": "Payments", "technical": "Bugs"},
}

// A rating against an ordered rubric, counting from zero.
typesafe.ScoreQuestion{
	Instructions: "How frustrated is the customer?",
	Criteria:     typesafe.ScoreCriteria{"Calm", "Frustrated", "Very angry"},
}
```

`State`, `Instructions`, and every criterion take text, a JSON object, or a
JSON array, so a string, a map, a slice, or a struct with JSON tags all work.

Ask as many questions as you like in one call. The model reads the state once
and evaluates every question against it, which is much cheaper and faster than
one call per question. See
[Speculative fan-out](https://docs.typesafe.ai/patterns/fan-out).

### Answers

Read an answer by name and type:

```go
urgent, err := resp.Answers.Noul("urgent")             // urgent.Noul
department, err := resp.Answers.Choice("department")   // department.Choice
frustration, err := resp.Answers.Score("frustration")  // frustration.Score
```

Each returns `ErrNoAnswer` when the question went unanswered and an
`*AnswerTypeError` when the answer has another type. Choice and score answers
carry a `Confidence`, which is how you decide whether to act or escalate. See
[Confidence](https://docs.typesafe.ai/confidence).

```go
if department.Confidence < 0.8 {
	return escalate(ticket)
}
return route(ticket, department.Choice)
```

A type switch handles answers whose type you do not know in advance, including
`UnknownAnswer` for a type this SDK version does not model.

### Errors

Every unsuccessful response becomes an `*APIError` that unwraps to a sentinel
naming the kind of failure:

```go
if errors.Is(err, typesafe.ErrRateLimit) {
	// The client already retried with backoff.
}

var apiErr *typesafe.APIError
if errors.As(err, &apiErr) {
	log.Printf("request %s failed: %d %s", apiErr.RequestID, apiErr.StatusCode, apiErr.Message)
}
```

### Configuration

```go
client, err := typesafe.NewClient(
	typesafe.FromEnv(),                          // TYPESAFE_API_KEY, _BASE_URL, _DEFAULT_MODEL
	typesafe.WithModel("jev-1.13.0"),
	typesafe.WithHTTPClient(&http.Client{Timeout: 30 * time.Second}),
	typesafe.WithLogger(slog.Default()),
)
```

The same options override a single call:

```go
resp, err := client.SystemOne(ctx, req, typesafe.WithModel("jev-preview"))
```

Unlike the Python and JavaScript SDKs, `NewClient` does not read the
environment on its own. `FromEnv` makes that dependency visible, and
`FromEnvFunc` lets a test or a secret store supply the values instead.

### Retries, timeouts, and cancellation

The client retries rate limits, overload responses, server errors, and
transport failures with exponential backoff, honoring `retry-after`.
`DefaultRetryPolicy` describes the defaults and `WithRetryPolicy` replaces
them:

```go
policy := typesafe.DefaultRetryPolicy()
policy.MaxRetries = 5
client, err := typesafe.NewClient(typesafe.FromEnv(), typesafe.WithRetryPolicy(policy))
```

The context bounds the whole call, including retries and the waits between
them. The `Timeout` of the HTTP client bounds one attempt.

### Testing your code

`typesafetest` runs a fake TypeSafe API, so your tests never need a key, a
network, or an account:

```go
srv := typesafetest.NewServer()
t.Cleanup(srv.Close)
srv.HandleSystemOne(func(_ context.Context, req typesafe.Request) (typesafe.Response, error) {
	return typesafe.Response{
		Answers: typesafe.Answers{"department": typesafe.ChoiceAnswer{
			Choice:     "billing",
			Confidence: 0.42,
		}},
	}, nil
})

client := srv.Client()
```

Without a handler it answers every question, so a test that only needs the
call to succeed writes nothing. `srv.Recorded()` returns the requests it
received, and `typesafetest.Failure` makes it return any status.

## Documentation

- [Usage guide](docs/usage.md) — the whole SDK, in the order you meet it:
  state, the three question types, fan-out, confidence, errors, retries,
  logging, and testing.
- [Examples](examples) — runnable programs that call the real API.
- [Reference](https://pkg.go.dev/github.com/jkakar/typesafe-sdk-go) — the
  generated package documentation, including
  [runnable examples](https://pkg.go.dev/github.com/jkakar/typesafe-sdk-go#pkg-examples)
  that need no key.
- [TypeSafe documentation](https://docs.typesafe.ai/) — what the model does
  and how to write good questions.

## Getting started with development

```sh
make check   # everything CI runs
make fix     # format and tidy
make cover   # the statements no test reaches
```

Go 1.27 and [golangci-lint](https://golangci-lint.run) are the only tools you
need. The module itself depends on nothing outside the standard library except
its test assertion library.

## References

- [TypeSafe documentation](https://docs.typesafe.ai/)
- [HTTP API reference](https://docs.typesafe.ai/api)
- [Usage guide](docs/usage.md)
- [Design and trade-offs](docs/design/sdk.md)
- [Testing doctrine](docs/testing.md)
- [Changelog](CHANGELOG.md)
- [Agent guidelines](AGENTS.md)
