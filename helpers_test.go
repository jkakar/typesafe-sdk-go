package typesafe_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/alecthomas/assert/v2"

	"github.com/jkakar/typesafe-sdk-go"
)

// newRawServer starts a server running handler and returns a client that
// reaches it through the server's own HTTP client, which owns a connection
// pool no other test shares.
func newRawServer(t *testing.T, handler http.HandlerFunc) *typesafe.Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	client, err := typesafe.NewClient(
		typesafe.WithAPIKey("secret"),
		typesafe.WithBaseURL(srv.URL),
		typesafe.WithHTTPClient(srv.Client()),
	)
	assert.NoError(t, err)
	return client
}

// writeJSON writes body as a JSON response with the given status.
func writeJSON(t *testing.T, w http.ResponseWriter, status int, body string) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, err := w.Write([]byte(body))
	assert.NoError(t, err)
}

// ticketQuestions asks one question of each type about a support ticket.
func ticketQuestions() typesafe.Questions {
	return typesafe.Questions{
		"urgent": typesafe.NoulQuestion{
			Instructions: "Does this convey urgency?",
			Criteria:     &typesafe.NoulCriteria{True: "Time sensitive", False: "No urgency"},
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
	}
}

// newStubClient returns a client whose transport answers without a network, so
// a test can drive the fake clock of testing/synctest. Real I/O does not
// belong inside a bubble, and no other seam reaches the retry timer.
func newStubClient(t *testing.T, transport http.RoundTripper, policy typesafe.RetryPolicy) *typesafe.Client {
	t.Helper()
	client, err := typesafe.NewClient(
		typesafe.WithAPIKey("secret"),
		typesafe.WithBaseURL("https://api.example.test"),
		typesafe.WithHTTPClient(&http.Client{Transport: transport}),
		typesafe.WithRetryPolicy(policy),
	)
	assert.NoError(t, err)
	return client
}
