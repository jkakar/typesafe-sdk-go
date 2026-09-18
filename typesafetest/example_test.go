package typesafetest_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/jkakar/typesafe-sdk-go"
	"github.com/jkakar/typesafe-sdk-go/typesafetest"
)

// routeTicket stands in for the code a program written against this SDK owns:
// it asks a question and decides what to do with the answer. Its tests are
// what typesafetest exists for.
func routeTicket(ctx context.Context, client *typesafe.Client, ticket string) (string, error) {
	resp, err := client.SystemOne(ctx, typesafe.Request{
		State: ticket,
		Questions: typesafe.Questions{
			"department": typesafe.ChoiceQuestion{
				Instructions: "Which team should handle this?",
				Criteria:     typesafe.ChoiceCriteria{"billing": nil, "technical": nil},
			},
		},
	})
	if err != nil {
		return "", err
	}
	department, err := resp.Answers.Choice("department")
	if err != nil {
		return "", err
	}
	if department.Confidence < 0.8 {
		return "human-review", nil
	}
	return department.Choice, nil
}

// Test your own code against a fake API, with no key, no network, and no
// account.
func Example() {
	srv := typesafetest.NewServer()
	defer srv.Close()
	srv.HandleSystemOne(func(context.Context, typesafe.Request) (typesafe.Response, error) {
		return typesafe.Response{Answers: typesafe.Answers{
			"department": typesafe.ChoiceAnswer{Choice: "billing", Confidence: 0.91},
		}}, nil
	})

	queue, err := routeTicket(context.Background(), srv.Client(), "I was charged twice.")
	if err != nil {
		fmt.Println("error:", err)
		return
	}

	fmt.Println(queue)
	// Output: billing
}

// A server without a handler answers every question, which is enough when the
// test cares that the call happened and not what came back.
func ExampleNewServer() {
	srv := typesafetest.NewServer()
	defer srv.Close()

	queue, err := routeTicket(context.Background(), srv.Client(), "I was charged twice.")
	if err != nil {
		fmt.Println("error:", err)
		return
	}

	// The default answer carries no confidence, so this code escalates.
	fmt.Println(queue)
	// Output: human-review
}

// Return any status the API can, and prove the code handles it. This is how
// you test rate-limit handling without a rate limit.
func ExampleFailure() {
	srv := typesafetest.NewServer()
	defer srv.Close()
	srv.HandleSystemOne(func(context.Context, typesafe.Request) (typesafe.Response, error) {
		return typesafe.Response{}, &typesafetest.Failure{
			StatusCode: http.StatusTooManyRequests,
			Body:       `{"error":"slow down"}`,
			Header:     http.Header{"Retry-After": []string{"30"}},
		}
	})
	// Ask for no retries, so the failure reaches the caller instead of
	// being retried away.
	noRetries := typesafe.DefaultRetryPolicy()
	noRetries.MaxRetries = 0

	_, err := routeTicket(context.Background(), srv.Client(typesafe.WithRetryPolicy(noRetries)), "ticket")

	var apiErr *typesafe.APIError
	if errors.As(err, &apiErr) {
		fmt.Println(errors.Is(err, typesafe.ErrRateLimit), apiErr.Message, apiErr.RetryAfter)
	}
	// Output: true slow down 30s
}

// Assert on what the SDK put on the wire.
func ExampleServer_Recorded() {
	srv := typesafetest.NewServer()
	defer srv.Close()

	if _, err := routeTicket(context.Background(), srv.Client(), "I was charged twice."); err != nil {
		fmt.Println("error:", err)
		return
	}

	sent := srv.Recorded()[0]
	fmt.Println(sent.Method, sent.Path)
	fmt.Println(string(sent.Body))
	// Output:
	// POST /v1/systemone
	// {"state":"I was charged twice.","model":"jev-latest","questions":{"department":{"type":"choice","instructions":"Which team should handle this?","criteria":{"billing":null,"technical":null}}}}
}
