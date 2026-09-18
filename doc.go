// Package typesafe is a client for the TypeSafe AI API.
//
// TypeSafe serves System One models, which answer typed questions about a
// piece of state and return calibrated probabilities instead of prose. A
// request carries the state under evaluation and a map of named questions.
// The response carries one answer per question, keyed by the same names.
//
// # Getting started
//
// Create a client, then ask questions about some state:
//
//	client, err := typesafe.NewClient(typesafe.FromEnv())
//	if err != nil {
//		return err
//	}
//	resp, err := client.SystemOne(ctx, typesafe.Request{
//		State: "I was charged twice. Please fix this ASAP.",
//		Questions: typesafe.Questions{
//			"urgent": typesafe.NoulQuestion{
//				Instructions: "Does this convey urgency?",
//			},
//			"department": typesafe.ChoiceQuestion{
//				Instructions: "Which team should handle this?",
//				Criteria: typesafe.ChoiceCriteria{
//					"billing":   "Payments, invoicing, refunds",
//					"technical": "Bugs, outages, integrations",
//				},
//			},
//		},
//	})
//	if err != nil {
//		return err
//	}
//	urgent, err := resp.Answers.Noul("urgent")
//	if err != nil {
//		return err
//	}
//	fmt.Println(urgent.Noul)
//
// # Question and answer types
//
// [NoulQuestion] asks a yes/no question and returns a [NoulAnswer] holding the
// probability of yes. [ChoiceQuestion] selects one option and returns a
// [ChoiceAnswer]. [ScoreQuestion] rates the state against an ordered rubric
// and returns a [ScoreAnswer]. Choice and score answers also carry a
// confidence, which reports how certain the model is.
//
// [Answers] holds every answer in a response. Read one with [Answers.Noul],
// [Answers.Choice], or [Answers.Score], which report a typed error when the
// name is absent or its answer has another type. A type switch over the
// [Answer] interface handles answers whose type the caller does not know in
// advance.
//
// # Errors
//
// An unsuccessful HTTP response becomes an [APIError] that unwraps to a
// sentinel naming the failure kind, so callers match either the kind or the
// detail:
//
//	if errors.Is(err, typesafe.ErrRateLimit) {
//		// back off
//	}
//	var apiErr *typesafe.APIError
//	if errors.As(err, &apiErr) {
//		log.Printf("request %s failed: %s", apiErr.RequestID, apiErr.Message)
//	}
//
// # Retries, timeouts, and cancellation
//
// The client retries rate limits, overload responses, server errors, and
// transport failures with exponential backoff. [DefaultRetryPolicy] describes
// the defaults and [WithRetryPolicy] replaces them.
//
// The context passed to a call bounds the whole operation, including retries
// and the waits between them. Bound a single attempt with the Timeout field of
// the [net/http.Client] passed to [WithHTTPClient].
//
// # Concurrency
//
// A [Client] is safe for concurrent use by multiple goroutines.
package typesafe
