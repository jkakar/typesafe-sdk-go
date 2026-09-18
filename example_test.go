package typesafe_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"maps"
	"net/http"
	"slices"
	"time"

	"github.com/jkakar/typesafe-sdk-go"
	"github.com/jkakar/typesafe-sdk-go/typesafetest"
)

// These examples run against typesafetest, the fake API that ships with this
// SDK, so they need no key and no network. A real program builds its client
// with typesafe.NewClient(typesafe.FromEnv()) and everything else is the
// same. Example, below, shows the fake in full; the rest reach for
// exampleClient to keep the point of each example in view.

// exampleClient starts a fake API that returns answers, and returns a client
// pointed at it along with the function that shuts it down.
func exampleClient(answers typesafe.Answers) (client *typesafe.Client, stop func()) {
	srv := typesafetest.NewServer()
	srv.HandleSystemOne(func(context.Context, typesafe.Request) (typesafe.Response, error) {
		return typesafe.Response{Model: "jev-1.13.0", Answers: answers}, nil
	})
	return srv.Client(), srv.Close
}

// Ask several questions about one support ticket and read the answers.
func Example() {
	// A real program uses typesafe.NewClient(typesafe.FromEnv()).
	srv := typesafetest.NewServer()
	defer srv.Close()
	srv.HandleSystemOne(func(context.Context, typesafe.Request) (typesafe.Response, error) {
		return typesafe.Response{
			Model: "jev-1.13.0",
			Answers: typesafe.Answers{
				"urgent": typesafe.NoulAnswer{Noul: 0.92},
				"department": typesafe.ChoiceAnswer{
					Choice:        "billing",
					Confidence:    0.88,
					Probabilities: map[string]float64{"billing": 0.91, "technical": 0.09},
				},
			},
		}, nil
	})
	client := srv.Client()

	resp, err := client.SystemOne(context.Background(), typesafe.Request{
		State: "I was charged twice. Please fix this ASAP.",
		Questions: typesafe.Questions{
			"urgent": typesafe.NoulQuestion{Instructions: "Does this convey urgency?"},
			"department": typesafe.ChoiceQuestion{
				Instructions: "Which team should handle this?",
				Criteria: typesafe.ChoiceCriteria{
					"billing":   "Payments, invoicing, refunds",
					"technical": "Bugs, outages, integrations",
				},
			},
		},
	})
	if err != nil {
		log.Fatal(err)
	}

	urgent, err := resp.Answers.Noul("urgent")
	if err != nil {
		log.Fatal(err)
	}
	department, err := resp.Answers.Choice("department")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("model=%s urgent=%.2f department=%s\n", resp.Model, urgent.Noul, department.Choice)
	// Output: model=jev-1.13.0 urgent=0.92 department=billing
}

// Configure a client. Explicit options win over the environment, whatever
// order they appear in.
func ExampleNewClient() {
	lookup := func(name string) (string, bool) {
		return map[string]string{typesafe.EnvAPIKey: "sk-example"}[name], true
	}

	client, err := typesafe.NewClient(
		// A real program uses typesafe.FromEnv(), which reads the
		// process environment. FromEnvFunc takes the values from
		// anywhere else, including a test.
		typesafe.FromEnvFunc(lookup),
		typesafe.WithModel("jev-1.13.0"),
		typesafe.WithHTTPClient(&http.Client{Timeout: 30 * time.Second}),
		typesafe.WithHeader("X-Request-Source", "support-triage"),
		typesafe.WithLogger(slog.Default()),
	)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println(client.BaseURL(), client.Model())
	// Output: https://api.typesafe.ai jev-1.13.0
}

// Ask every question you might need in one call. The model reads the state
// once and evaluates the questions against it in parallel, so a question you
// end up ignoring costs far less than a second call would. See
// https://docs.typesafe.ai/patterns/fan-out.
func ExampleClient_SystemOne_fanOut() {
	client, stop := exampleClient(typesafe.Answers{
		"urgent":   typesafe.NoulAnswer{Noul: 0.92},
		"refund":   typesafe.NoulAnswer{Noul: 0.81},
		"security": typesafe.NoulAnswer{Noul: 0.04},
		"churn":    typesafe.NoulAnswer{Noul: 0.63},
	})
	defer stop()

	resp, err := client.SystemOne(context.Background(), typesafe.Request{
		State: "I was charged twice. Please fix this ASAP or I am leaving.",
		Questions: typesafe.Questions{
			"urgent":   typesafe.NoulQuestion{Instructions: "Does this convey urgency?"},
			"refund":   typesafe.NoulQuestion{Instructions: "Is the customer asking for money back?"},
			"security": typesafe.NoulQuestion{Instructions: "Does this report a security problem?"},
			"churn":    typesafe.NoulQuestion{Instructions: "Is the customer threatening to leave?"},
		},
	})
	if err != nil {
		log.Fatal(err)
	}

	for _, name := range slices.Sorted(maps.Keys(resp.Answers)) {
		answer, err := resp.Answers.Noul(name)
		if err != nil {
			log.Fatal(err)
		}
		if answer.Noul > 0.5 {
			fmt.Println(name)
		}
	}
	// Output:
	// churn
	// refund
	// urgent
}

// Instructions and criteria take JSON structure, not only text, which is the
// way to give a question rules and boundary cases. See
// https://docs.typesafe.ai/primitives/advanced.
func ExampleNoulQuestion() {
	client, stop := exampleClient(typesafe.Answers{"spam": typesafe.NoulAnswer{Noul: 0.97}})
	defer stop()

	resp, err := client.SystemOne(context.Background(), typesafe.Request{
		// State takes structure too: a map, a slice, or a struct
		// with JSON tags.
		State: map[string]any{
			"subject": "You have WON!!",
			"from":    "prizes@example.test",
		},
		Questions: typesafe.Questions{
			"spam": typesafe.NoulQuestion{
				Instructions: map[string]any{
					"task":   "Decide whether this message is unsolicited advertising.",
					"ignore": []string{"receipts", "password resets"},
				},
				Criteria: &typesafe.NoulCriteria{
					True:  "Unsolicited advertising",
					False: "A legitimate conversation",
				},
			},
		},
	})
	if err != nil {
		log.Fatal(err)
	}

	spam, err := resp.Answers.Noul("spam")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("%.2f\n", spam.Noul)
	// Output: 0.97
}

// Route a decision by confidence: act on a confident answer and send an
// uncertain one to a person. See https://docs.typesafe.ai/confidence.
func ExampleChoiceAnswer() {
	client, stop := exampleClient(typesafe.Answers{
		"department": typesafe.ChoiceAnswer{Choice: "billing", Confidence: 0.42},
	})
	defer stop()

	resp, err := client.SystemOne(context.Background(), typesafe.Request{
		State: "I was charged twice.",
		Questions: typesafe.Questions{
			"department": typesafe.ChoiceQuestion{
				Instructions: "Which team should handle this?",
				Criteria:     typesafe.ChoiceCriteria{"billing": nil, "technical": nil},
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
	if department.Confidence < 0.8 {
		fmt.Println("escalate to a person")
	} else {
		fmt.Println("route to", department.Choice)
	}
	// Output: escalate to a person
}

// Break a judgement into atomic scores and combine them with weights your
// code owns, so the reason for a decision stays auditable. See
// https://docs.typesafe.ai/patterns/composite-scoring.
func ExampleScoreAnswer() {
	client, stop := exampleClient(typesafe.Answers{
		"severity": typesafe.ScoreAnswer{
			Score:         2.0,
			Confidence:    0.81,
			Legend:        map[int]any{0: "Cosmetic", 1: "Degraded", 2: "Broken"},
			Probabilities: map[int]float64{0: 0.02, 1: 0.08, 2: 0.90},
		},
		"reach": typesafe.ScoreAnswer{Score: 1.0, Confidence: 0.74},
	})
	defer stop()

	resp, err := client.SystemOne(context.Background(), typesafe.Request{
		State: "Checkout fails for every customer in Canada.",
		Questions: typesafe.Questions{
			"severity": typesafe.ScoreQuestion{
				Instructions: "How badly is the product broken?",
				Criteria:     typesafe.ScoreCriteria{"Cosmetic", "Degraded", "Broken"},
			},
			"reach": typesafe.ScoreQuestion{
				Instructions: "How many customers does this reach?",
				Criteria:     typesafe.ScoreCriteria{"One", "Some", "All"},
			},
		},
	})
	if err != nil {
		log.Fatal(err)
	}

	severity, err := resp.Answers.Score("severity")
	if err != nil {
		log.Fatal(err)
	}
	reach, err := resp.Answers.Score("reach")
	if err != nil {
		log.Fatal(err)
	}
	priority := 0.7*severity.Score + 0.3*reach.Score

	fmt.Printf("severity=%s priority=%.2f\n", severity.Legend[int(severity.Score)], priority)
	// Output: severity=Broken priority=1.70
}

// Switch over the answers when the code handles whatever came back, rather
// than one question it named.
func ExampleAnswers() {
	client, stop := exampleClient(typesafe.Answers{
		"urgent":     typesafe.NoulAnswer{Noul: 0.92},
		"department": typesafe.ChoiceAnswer{Choice: "billing", Confidence: 0.88},
	})
	defer stop()

	resp, err := client.SystemOne(context.Background(), typesafe.Request{
		State: "I was charged twice.",
		Questions: typesafe.Questions{
			"urgent": typesafe.NoulQuestion{Instructions: "Urgent?"},
			"department": typesafe.ChoiceQuestion{
				Instructions: "Which team?",
				Criteria:     typesafe.ChoiceCriteria{"billing": nil},
			},
		},
	})
	if err != nil {
		log.Fatal(err)
	}

	for _, name := range slices.Sorted(maps.Keys(resp.Answers)) {
		switch answer := resp.Answers[name].(type) {
		case typesafe.NoulAnswer:
			fmt.Printf("%s: %.2f\n", name, answer.Noul)
		case typesafe.ChoiceAnswer:
			fmt.Printf("%s: %s\n", name, answer.Choice)
		case typesafe.ScoreAnswer:
			fmt.Printf("%s: %.1f\n", name, answer.Score)
		case typesafe.UnknownAnswer:
			fmt.Printf("%s: unhandled %q answer\n", name, answer.Type)
		}
	}
	// Output:
	// department: billing
	// urgent: 0.92
}

// Send a question type this SDK version does not name yet, and read back an
// answer type it does not model. Neither needs a new release.
func ExampleRawQuestion() {
	client, stop := exampleClient(typesafe.Answers{
		"ranking": typesafe.UnknownAnswer{
			Type: "rank",
			Raw:  json.RawMessage(`{"type":"rank","order":["b","a"]}`),
		},
	})
	defer stop()

	resp, err := client.SystemOne(context.Background(), typesafe.Request{
		State: "Two candidate answers.",
		Questions: typesafe.Questions{
			"ranking": typesafe.RawQuestion{
				Type: "rank",
				JSON: json.RawMessage(`{"type":"rank","instructions":"Order by relevance."}`),
			},
		},
	})
	if err != nil {
		log.Fatal(err)
	}

	unknown, ok := resp.Answers["ranking"].(typesafe.UnknownAnswer)
	if !ok {
		log.Fatal("expected an answer this SDK does not model")
	}
	fmt.Println(unknown.Type, string(unknown.Raw))
	// Output: rank {"type":"rank","order":["b","a"]}
}

// Match the kind of failure with errors.Is and read its detail with errors.As.
func ExampleAPIError() {
	srv := typesafetest.NewServer()
	defer srv.Close()
	srv.HandleModels(func(context.Context) ([]typesafe.Model, error) {
		return nil, &typesafetest.Failure{StatusCode: 401, Body: `{"error":"invalid api key"}`}
	})

	_, err := srv.Client().ListModels(context.Background())

	if errors.Is(err, typesafe.ErrAuthentication) {
		var apiErr *typesafe.APIError
		errors.As(err, &apiErr)
		fmt.Printf("%d: %s\n", apiErr.StatusCode, apiErr.Message)
	}
	// Output: 401: invalid api key
}

// Replace the retry behavior. The zero value of RetryPolicy retries nothing,
// so start from DefaultRetryPolicy and change what you need.
func ExampleWithRetryPolicy() {
	srv := typesafetest.NewServer()
	defer srv.Close()
	srv.HandleModels(func(context.Context) ([]typesafe.Model, error) {
		return nil, &typesafetest.Failure{StatusCode: http.StatusServiceUnavailable}
	})

	policy := typesafe.DefaultRetryPolicy()
	policy.MaxRetries = 5
	policy.MaxBackoff = 30 * time.Second
	// Retry a gateway timeout as well as the defaults.
	policy.RetryStatus = func(status int) bool {
		return status == http.StatusTooManyRequests ||
			status == http.StatusGatewayTimeout ||
			status >= http.StatusInternalServerError
	}
	client := srv.Client(typesafe.WithRetryPolicy(policy))

	// A single call overrides the client's policy. This one asks for no
	// retries at all, so the server sees one request.
	off := typesafe.DefaultRetryPolicy()
	off.MaxRetries = 0
	_, err := client.ListModels(context.Background(), typesafe.WithRetryPolicy(off))

	fmt.Println(len(srv.Recorded()), errors.Is(err, typesafe.ErrServer))
	// Output: 1 true
}
