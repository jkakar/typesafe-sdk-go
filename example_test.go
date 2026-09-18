package typesafe_test

import (
	"context"
	"errors"
	"fmt"
	"log"

	"github.com/jkakar/typesafe-sdk-go"
	"github.com/jkakar/typesafe-sdk-go/typesafetest"
)

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

// Route a decision by confidence: act on a confident answer and send an
// uncertain one to a person. See https://docs.typesafe.ai/confidence.
func ExampleChoiceAnswer() {
	srv := typesafetest.NewServer()
	defer srv.Close()
	srv.HandleSystemOne(func(context.Context, typesafe.Request) (typesafe.Response, error) {
		return typesafe.Response{Answers: typesafe.Answers{
			"department": typesafe.ChoiceAnswer{Choice: "billing", Confidence: 0.42},
		}}, nil
	})

	resp, err := srv.Client().SystemOne(context.Background(), typesafe.Request{
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

// List the models the account can name in a request.
func ExampleClient_ListModels() {
	srv := typesafetest.NewServer()
	defer srv.Close()
	srv.HandleModels(func(context.Context) ([]typesafe.Model, error) {
		return []typesafe.Model{
			{Name: "jev-latest", Description: "The most recent stable release.", ReleaseDate: "2026-09-15"},
		}, nil
	})

	models, err := srv.Client().ListModels(context.Background())
	if err != nil {
		log.Fatal(err)
	}

	for _, model := range models {
		fmt.Printf("%s (%s): %s\n", model.Name, model.ReleaseDate, model.Description)
	}
	// Output: jev-latest (2026-09-15): The most recent stable release.
}
