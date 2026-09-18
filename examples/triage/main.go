// Command triage routes a support ticket with one TypeSafe call.
//
// It asks four questions about the ticket at once. The model reads the ticket
// once and answers them in parallel, so asking four costs little more than
// asking one. The answers come back as numbers, and the routing decision is
// ordinary Go code that this program owns.
//
// Usage:
//
//	export TYPESAFE_API_KEY=...
//	go run ./examples/triage "I was charged twice. Please fix this ASAP."
//	echo "The checkout page is down." | go run ./examples/triage
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"

	"github.com/jkakar/typesafe-sdk-go"
)

// confident is the confidence below which a person decides instead.
const confident = 0.8

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	if err := run(ctx, os.Args[1:], os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "triage:", describe(err))
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, in io.Reader, out io.Writer) error {
	ticket, err := readTicket(args, in)
	if err != nil {
		return err
	}

	client, err := typesafe.NewClient(typesafe.FromEnv())
	if err != nil {
		return err
	}

	resp, err := client.SystemOne(ctx, typesafe.Request{
		State: ticket,
		Questions: typesafe.Questions{
			"department": typesafe.ChoiceQuestion{
				Instructions: "Which team should handle this ticket?",
				Criteria: typesafe.ChoiceCriteria{
					"billing":   "Payments, invoicing, refunds, subscriptions",
					"technical": "Bugs, outages, integrations, performance",
					"account":   "Sign-in, permissions, team membership",
					"other":     nil,
				},
			},
			"urgent": typesafe.NoulQuestion{
				Instructions: "Does this ticket need attention today?",
				Criteria: &typesafe.NoulCriteria{
					True:  "Money or data is at risk, or the customer is blocked",
					False: "The customer can wait for the normal queue",
				},
			},
			"frustration": typesafe.ScoreQuestion{
				Instructions: "How frustrated is the customer?",
				Criteria:     typesafe.ScoreCriteria{"Calm", "Frustrated", "Very angry"},
			},
			"refund": typesafe.NoulQuestion{
				Instructions: "Is the customer asking for money back?",
			},
		},
	})
	if err != nil {
		return err
	}

	return report(out, resp)
}

// report prints each answer and the decision they add up to.
//
// real write below is checked.
//
//nolint:errcheck // Formatting into a strings.Builder cannot fail; the one
func report(out io.Writer, resp *typesafe.Response) error {
	department, err := resp.Answers.Choice("department")
	if err != nil {
		return err
	}
	urgent, err := resp.Answers.Noul("urgent")
	if err != nil {
		return err
	}
	frustration, err := resp.Answers.Score("frustration")
	if err != nil {
		return err
	}
	refund, err := resp.Answers.Noul("refund")
	if err != nil {
		return err
	}

	// Build the report, then write it once, so a broken pipe is one error
	// to report rather than eight to ignore.
	var report strings.Builder
	fmt.Fprintf(&report, "model:       %s\n", resp.Model)
	fmt.Fprintf(&report, "department:  %s (confidence %.2f)\n", department.Choice, department.Confidence)
	fmt.Fprintf(&report, "urgent:      %.2f\n", urgent.Noul)
	fmt.Fprintf(&report, "frustration: %.1f %s (confidence %.2f)\n",
		frustration.Score, level(frustration), frustration.Confidence)
	fmt.Fprintf(&report, "refund:      %.2f\n", refund.Noul)
	fmt.Fprintf(&report, "tokens:      %d in, %d out\n\n", resp.Usage.InputTokens, resp.Usage.OutputTokens)
	fmt.Fprintf(&report, "decision:    %s\n", decide(department, urgent, frustration, refund))

	_, err = io.WriteString(out, report.String())
	return err
}

// decide turns the answers into an action. Every threshold here belongs to
// this program, not to the model: the model reports how likely something is,
// and the program decides what that is worth.
func decide(
	department typesafe.ChoiceAnswer,
	urgent typesafe.NoulAnswer,
	frustration typesafe.ScoreAnswer,
	refund typesafe.NoulAnswer,
) string {
	switch {
	case department.Confidence < confident:
		return "send to human review: no team is a clear fit"
	case frustration.Score >= 1.5 && urgent.Noul > confident:
		return fmt.Sprintf("page the %s on-call: an angry customer is blocked", department.Choice)
	case refund.Noul > confident:
		return fmt.Sprintf("route to %s and attach the refund workflow", department.Choice)
	case urgent.Noul > confident:
		return fmt.Sprintf("route to %s at the top of the queue", department.Choice)
	default:
		return fmt.Sprintf("route to %s", department.Choice)
	}
}

// level names the rubric entry nearest the score, which the answer carries so
// a reader does not have to remember the request.
func level(answer typesafe.ScoreAnswer) string {
	description, ok := answer.Legend[int(answer.Score+0.5)]
	if !ok {
		return ""
	}
	return fmt.Sprintf("(%v)", description)
}

// readTicket takes the ticket from the command line, or from in when the
// command line is empty.
func readTicket(args []string, in io.Reader) (string, error) {
	if len(args) > 0 {
		return strings.Join(args, " "), nil
	}
	piped, err := io.ReadAll(in)
	if err != nil {
		return "", fmt.Errorf("read the ticket: %w", err)
	}
	ticket := strings.TrimSpace(string(piped))
	if ticket == "" {
		return "", errors.New("no ticket: pass one as an argument or pipe it in")
	}
	return ticket, nil
}

// describe adds what an operator needs to act on a failure.
func describe(err error) string {
	switch {
	case errors.Is(err, typesafe.ErrNoAPIKey):
		return fmt.Sprintf("%s: set %s", err, typesafe.EnvAPIKey)
	case errors.Is(err, typesafe.ErrAuthentication):
		return fmt.Sprintf("%s: check %s", err, typesafe.EnvAPIKey)
	}
	var apiErr *typesafe.APIError
	if errors.As(err, &apiErr) && apiErr.RequestID != "" {
		return fmt.Sprintf("%s: quote request %s to support", err, apiErr.RequestID)
	}
	return err.Error()
}
