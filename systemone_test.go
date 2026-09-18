package typesafe_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/alecthomas/assert/v2"

	"github.com/jkakar/typesafe-sdk-go"
	"github.com/jkakar/typesafe-sdk-go/typesafetest"
)

func TestClient_SystemOne(t *testing.T) {
	t.Parallel()

	t.Run("returns one answer per question", func(t *testing.T) {
		t.Parallel()
		srv := typesafetest.NewServer()
		t.Cleanup(srv.Close)
		srv.HandleSystemOne(func(_ context.Context, _ typesafe.Request) (typesafe.Response, error) {
			return typesafe.Response{
				Model: "jev-1.13.0",
				Answers: typesafe.Answers{
					"urgent": typesafe.NoulAnswer{Noul: 0.92},
					"department": typesafe.ChoiceAnswer{
						Choice:        "technical",
						Confidence:    0.82,
						Probabilities: map[string]float64{"billing": 0.15, "technical": 0.85},
					},
					"frustration": typesafe.ScoreAnswer{
						Score:         1.6,
						Confidence:    0.78,
						Legend:        map[int]any{0: "Calm", 1: "Frustrated", 2: "Very angry"},
						Probabilities: map[int]float64{0: 0.05, 1: 0.3, 2: 0.65},
					},
				},
				Usage: typesafe.Usage{InputTokens: 312, OutputTokens: 48},
			}, nil
		})
		client := srv.Client()

		resp, err := client.SystemOne(t.Context(), typesafe.Request{
			State:     "I was charged twice. Please fix this ASAP.",
			Questions: ticketQuestions(),
		})

		assert.NoError(t, err)
		assert.Equal(t, "jev-1.13.0", resp.Model)
		assert.Equal(t, typesafe.Usage{InputTokens: 312, OutputTokens: 48}, resp.Usage)
		urgent, err := resp.Answers.Noul("urgent")
		assert.NoError(t, err)
		assert.Equal(t, 0.92, urgent.Noul)
		department, err := resp.Answers.Choice("department")
		assert.NoError(t, err)
		assert.Equal(t, "technical", department.Choice)
		assert.Equal(t, 0.85, department.Probabilities["technical"])
		frustration, err := resp.Answers.Score("frustration")
		assert.NoError(t, err)
		assert.Equal(t, 1.6, frustration.Score)
		assert.Equal(t, "Very angry", frustration.Legend[2])
		assert.Equal(t, 0.65, frustration.Probabilities[2])
	})

	t.Run("sends the state, model and questions", func(t *testing.T) {
		t.Parallel()
		srv := typesafetest.NewServer()
		t.Cleanup(srv.Close)
		client := srv.Client(typesafe.WithModel("jev-preview"))

		_, err := client.SystemOne(t.Context(), typesafe.Request{
			State: map[string]any{"subject": "Duplicate charge"},
			Questions: typesafe.Questions{
				"urgent": typesafe.NoulQuestion{Instructions: "Urgent?"},
			},
		})

		assert.NoError(t, err)
		recorded := srv.Recorded()
		assert.Equal(t, 1, len(recorded))
		assert.Equal(t, "/v1/systemone", recorded[0].Path)
		assert.Equal(t, `{"state":{"subject":"Duplicate charge"},"model":"jev-preview",`+
			`"questions":{"urgent":{"type":"noul","instructions":"Urgent?"}}}`, string(recorded[0].Body))
	})

	t.Run("sends a question type it does not model", func(t *testing.T) {
		t.Parallel()
		srv := typesafetest.NewServer()
		t.Cleanup(srv.Close)
		srv.HandleSystemOne(func(_ context.Context, req typesafe.Request) (typesafe.Response, error) {
			return typesafe.Response{Answers: typesafe.Answers{"rank": typesafe.NoulAnswer{}}}, nil
		})
		client := srv.Client()

		_, err := client.SystemOne(t.Context(), typesafe.Request{
			State: "ticket",
			Questions: typesafe.Questions{
				"rank": typesafe.RawQuestion{Type: "rank", JSON: json.RawMessage(`{"type":"rank","by":"date"}`)},
			},
		})

		assert.NoError(t, err)
		assert.Contains(t, string(srv.Recorded()[0].Body), `"rank":{"type":"rank","by":"date"}`)
	})

	t.Run("sends the request model over the client model", func(t *testing.T) {
		t.Parallel()
		srv := typesafetest.NewServer()
		t.Cleanup(srv.Close)
		client := srv.Client(typesafe.WithModel("jev-latest"))

		_, err := client.SystemOne(t.Context(), typesafe.Request{
			State:     "ticket",
			Model:     "jev-1.13.0",
			Questions: typesafe.Questions{"urgent": typesafe.NoulQuestion{}},
		})

		assert.NoError(t, err)
		assert.Contains(t, string(srv.Recorded()[0].Body), `"model":"jev-1.13.0"`)
	})

	t.Run("identifies the sdk and authenticates the request", func(t *testing.T) {
		t.Parallel()
		srv := typesafetest.NewServer()
		t.Cleanup(srv.Close)
		client := srv.Client(typesafe.WithHeader("X-Trace", "abc"))

		_, err := client.SystemOne(t.Context(), typesafe.Request{
			State:     "ticket",
			Questions: typesafe.Questions{"urgent": typesafe.NoulQuestion{}},
		})

		assert.NoError(t, err)
		header := srv.Recorded()[0].Header
		assert.Equal(t, "Bearer "+typesafetest.APIKey, header.Get("Authorization"))
		assert.Equal(t, "application/json", header.Get("Accept"))
		assert.Equal(t, "application/json", header.Get("Content-Type"))
		assert.Equal(t, "typesafe-sdk-go/"+typesafe.Version, header.Get("User-Agent"))
		assert.Equal(t, "typesafe-sdk-go/"+typesafe.Version, header.Get("X-TypeSafe-SDK"))
		assert.HasPrefix(t, header.Get("X-TypeSafe-Runtime"), "go/")
		assert.Equal(t, "abc", header.Get("X-Trace"))
		assert.Equal(t, "", header.Get("X-TypeSafe-Retry-Count"))
	})

	t.Run("applies a call option to one call only", func(t *testing.T) {
		t.Parallel()
		srv := typesafetest.NewServer()
		t.Cleanup(srv.Close)
		client := srv.Client()
		req := typesafe.Request{State: "ticket", Questions: typesafe.Questions{"urgent": typesafe.NoulQuestion{}}}

		_, err := client.SystemOne(t.Context(), req, typesafe.WithHeader("X-Trace", "once"))
		assert.NoError(t, err)
		_, err = client.SystemOne(t.Context(), req)
		assert.NoError(t, err)

		recorded := srv.Recorded()
		assert.Equal(t, "once", recorded[0].Header.Get("X-Trace"))
		assert.Equal(t, "", recorded[1].Header.Get("X-Trace"))
	})

	t.Run("returns the error from an invalid call option", func(t *testing.T) {
		t.Parallel()
		srv := typesafetest.NewServer()
		t.Cleanup(srv.Close)
		client := srv.Client()

		_, err := client.SystemOne(t.Context(), typesafe.Request{
			State:     "ticket",
			Questions: typesafe.Questions{"urgent": typesafe.NoulQuestion{}},
		}, typesafe.WithModel(""))

		assert.Error(t, err)
		assert.Zero(t, len(srv.Recorded()))
	})

	t.Run("rejects a request with no questions", func(t *testing.T) {
		t.Parallel()
		client := newRawServer(t, func(http.ResponseWriter, *http.Request) {
			assert.True(t, false, "the client must not send a request with no questions")
		})

		_, err := client.SystemOne(t.Context(), typesafe.Request{State: "ticket"})

		assert.IsError(t, err, typesafe.ErrNoQuestions)
	})

	t.Run("rejects a question with no criteria", func(t *testing.T) {
		t.Parallel()
		questions := map[string]typesafe.Questions{
			"choice":           {"department": typesafe.ChoiceQuestion{Instructions: "Which team?"}},
			"score":            {"frustration": typesafe.ScoreQuestion{Instructions: "How frustrated?"}},
			"raw without type": {"custom": typesafe.RawQuestion{JSON: json.RawMessage(`{"type":"new"}`)}},
			"raw without json": {"custom": typesafe.RawQuestion{Type: "new"}},
		}
		for name, questions := range questions {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				client := newRawServer(t, func(http.ResponseWriter, *http.Request) {
					assert.True(t, false, "the client must not send an invalid question")
				})

				_, err := client.SystemOne(t.Context(), typesafe.Request{State: "ticket", Questions: questions})

				assert.Error(t, err)
			})
		}
	})

	t.Run("names the question the api would reject", func(t *testing.T) {
		t.Parallel()
		client := newRawServer(t, func(http.ResponseWriter, *http.Request) {})

		_, err := client.SystemOne(t.Context(), typesafe.Request{
			State:     "ticket",
			Questions: typesafe.Questions{"department": typesafe.ChoiceQuestion{}},
		})

		assert.IsError(t, err, typesafe.ErrNoCriteria)
		assert.Contains(t, err.Error(), `question "department"`)
	})

	t.Run("rejects state the api does not accept", func(t *testing.T) {
		t.Parallel()
		states := map[string]any{
			"nothing": nil,
			"number":  42,
			"boolean": true,
		}
		for name, state := range states {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				client := newRawServer(t, func(http.ResponseWriter, *http.Request) {
					assert.True(t, false, "the client must not send invalid state")
				})

				_, err := client.SystemOne(t.Context(), typesafe.Request{
					State:     state,
					Questions: typesafe.Questions{"urgent": typesafe.NoulQuestion{}},
				})

				assert.IsError(t, err, typesafe.ErrInvalidState)
			})
		}
	})

	t.Run("reports state that cannot be encoded as json", func(t *testing.T) {
		t.Parallel()
		client := newRawServer(t, func(http.ResponseWriter, *http.Request) {})

		_, err := client.SystemOne(t.Context(), typesafe.Request{
			State:     make(chan int),
			Questions: typesafe.Questions{"urgent": typesafe.NoulQuestion{}},
		})

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "encode state")
	})

	t.Run("reports a question that cannot be encoded as json", func(t *testing.T) {
		t.Parallel()
		client := newRawServer(t, func(http.ResponseWriter, *http.Request) {})

		_, err := client.SystemOne(t.Context(), typesafe.Request{
			State: "ticket",
			Questions: typesafe.Questions{
				"urgent": typesafe.NoulQuestion{Instructions: make(chan int)},
			},
		})

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "encode request")
	})

	t.Run("returns an api error for an unsuccessful response", func(t *testing.T) {
		t.Parallel()
		srv := typesafetest.NewServer()
		t.Cleanup(srv.Close)
		srv.HandleSystemOne(func(context.Context, typesafe.Request) (typesafe.Response, error) {
			return typesafe.Response{}, &typesafetest.Failure{
				StatusCode: http.StatusUnauthorized,
				Body:       `{"error":"invalid api key"}`,
				Header:     http.Header{"X-Typesafe-Request-Id": []string{"req_1"}},
			}
		})
		client := srv.Client()

		_, err := client.SystemOne(t.Context(), typesafe.Request{
			State:     "ticket",
			Questions: typesafe.Questions{"urgent": typesafe.NoulQuestion{}},
		})

		assert.IsError(t, err, typesafe.ErrAuthentication)
		var apiErr *typesafe.APIError
		assert.True(t, errors.As(err, &apiErr))
		assert.Equal(t, http.StatusUnauthorized, apiErr.StatusCode)
		assert.Equal(t, "invalid api key", apiErr.Message)
		assert.Equal(t, "req_1", apiErr.RequestID)
		assert.Equal(t, http.MethodPost, apiErr.Method)
		assert.Contains(t, err.Error(), "req_1")
	})

	t.Run("describes the failure the response body reports", func(t *testing.T) {
		t.Parallel()
		bodies := map[string]struct {
			body string
			want string
		}{
			"error string":     {`{"error":"boom"}`, "boom"},
			"error object":     {`{"error":{"message":"boom"}}`, "boom"},
			"message":          {`{"message":"boom"}`, "boom"},
			"detail string":    {`{"detail":"boom"}`, "boom"},
			"detail object":    {`{"detail":{"message":"boom"}}`, "boom"},
			"validation list":  {`{"detail":[{"loc":["body","questions"],"msg":"field required"}]}`, "questions: field required"},
			"validation twice": {`{"detail":[{"loc":["body"],"msg":"a"},{"loc":["body","x"],"msg":"b"}]}`, "a; x: b"},
			"validation gap":   {`{"detail":[{"loc":["body"]},{"msg":"b"}]}`, "b"},
			"bare string":      {`"boom"`, "boom"},
			"unrecognized":     {`{"oops":true}`, `{"oops":true}`},
			"not json":         {`boom`, "boom"},
			"empty":            {``, ""},
		}
		for name, tc := range bodies {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				client := newRawServer(t, func(w http.ResponseWriter, _ *http.Request) {
					writeJSON(t, w, http.StatusUnprocessableEntity, tc.body)
				})

				_, err := client.SystemOne(t.Context(), typesafe.Request{
					State:     "ticket",
					Questions: typesafe.Questions{"urgent": typesafe.NoulQuestion{}},
				})

				var apiErr *typesafe.APIError
				assert.True(t, errors.As(err, &apiErr))
				assert.Equal(t, tc.want, apiErr.Message)
			})
		}
	})

	t.Run("shortens a long undescribed error body", func(t *testing.T) {
		t.Parallel()
		client := newRawServer(t, func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(t, w, http.StatusBadRequest, strings.Repeat("x", 500))
		})

		_, err := client.SystemOne(t.Context(), typesafe.Request{
			State:     "ticket",
			Questions: typesafe.Questions{"urgent": typesafe.NoulQuestion{}},
		})

		var apiErr *typesafe.APIError
		assert.True(t, errors.As(err, &apiErr))
		assert.Equal(t, 201, len([]rune(apiErr.Message)))
		assert.HasSuffix(t, apiErr.Message, "…")
	})

	t.Run("reports a response the server cuts short", func(t *testing.T) {
		t.Parallel()
		client := newRawServer(t, func(w http.ResponseWriter, _ *http.Request) {
			// A longer Content-Length than the body makes net/http
			// close the connection, which is the read failure a
			// caller sees when a response is interrupted.
			w.Header().Set("Content-Length", "1024")
			w.Header().Set("Content-Type", "application/json")
			_, err := w.Write([]byte(`{"model":`))
			assert.NoError(t, err)
		})
		policy := typesafe.DefaultRetryPolicy()
		policy.MaxRetries = 0

		_, err := client.SystemOne(t.Context(), oneQuestion(), typesafe.WithRetryPolicy(policy))

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "read response body")
	})

	t.Run("returns a decode error for a body it cannot read", func(t *testing.T) {
		t.Parallel()
		client := newRawServer(t, func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(t, w, http.StatusOK, `{"model":`)
		})

		_, err := client.SystemOne(t.Context(), typesafe.Request{
			State:     "ticket",
			Questions: typesafe.Questions{"urgent": typesafe.NoulQuestion{}},
		})

		var decodeErr *typesafe.DecodeError
		assert.True(t, errors.As(err, &decodeErr))
		assert.Equal(t, http.StatusOK, decodeErr.StatusCode)
		assert.Contains(t, err.Error(), "decode response")
	})

	t.Run("returns a decode error when an answer is malformed", func(t *testing.T) {
		t.Parallel()
		client := newRawServer(t, func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(t, w, http.StatusOK,
				`{"model":"jev-latest","usage":{},"answers":{"urgent":{"type":"noul","noul":"high"}}}`)
		})

		_, err := client.SystemOne(t.Context(), typesafe.Request{
			State:     "ticket",
			Questions: typesafe.Questions{"urgent": typesafe.NoulQuestion{}},
		})

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "urgent")
	})

	t.Run("keeps an answer type it does not model", func(t *testing.T) {
		t.Parallel()
		client := newRawServer(t, func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(t, w, http.StatusOK,
				`{"model":"jev-latest","usage":{},"answers":{"mood":{"type":"vector","values":[1,2]}}}`)
		})

		resp, err := client.SystemOne(t.Context(), typesafe.Request{
			State:     "ticket",
			Questions: typesafe.Questions{"urgent": typesafe.NoulQuestion{}},
		})

		assert.NoError(t, err)
		unknown, ok := resp.Answers["mood"].(typesafe.UnknownAnswer)
		assert.True(t, ok)
		assert.Equal(t, "vector", unknown.Kind())
		assert.Equal(t, `{"type":"vector","values":[1,2]}`, string(unknown.Raw))
	})

	t.Run("exposes the request id and the http response", func(t *testing.T) {
		t.Parallel()
		client := newRawServer(t, func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("X-Typesafe-Request-Id", "req_7")
			writeJSON(t, w, http.StatusOK, `{"model":"jev-latest","usage":{},"answers":{}}`)
		})

		resp, err := client.SystemOne(t.Context(), typesafe.Request{
			State:     "ticket",
			Questions: typesafe.Questions{"urgent": typesafe.NoulQuestion{}},
		})

		assert.NoError(t, err)
		assert.Equal(t, "req_7", resp.RequestID)
		assert.Equal(t, http.StatusOK, resp.HTTPResponse.StatusCode)
		body, err := io.ReadAll(resp.HTTPResponse.Body)
		assert.NoError(t, err)
		assert.Contains(t, string(body), `"model":"jev-latest"`)
	})

	t.Run("logs the request and redacts credentials", func(t *testing.T) {
		t.Parallel()
		srv := typesafetest.NewServer()
		t.Cleanup(srv.Close)
		var logs bytes.Buffer
		logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
		client := srv.Client(typesafe.WithLogger(logger))

		_, err := client.SystemOne(t.Context(), typesafe.Request{
			State:     "ticket",
			Questions: typesafe.Questions{"urgent": typesafe.NoulQuestion{}},
		})

		assert.NoError(t, err)
		assert.Contains(t, logs.String(), "typesafe: sending request")
		assert.Contains(t, logs.String(), "typesafe: received response")
		assert.Contains(t, logs.String(), "headers.Authorization=***")
		assert.NotContains(t, logs.String(), typesafetest.APIKey)
	})

	t.Run("returns the context error when the caller cancels", func(t *testing.T) {
		t.Parallel()
		client := newRawServer(t, func(http.ResponseWriter, *http.Request) {})
		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		_, err := client.SystemOne(ctx, typesafe.Request{
			State:     "ticket",
			Questions: typesafe.Questions{"urgent": typesafe.NoulQuestion{}},
		})

		assert.IsError(t, err, context.Canceled)
	})
}
