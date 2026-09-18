package typesafetest_test

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"net"
	"net/http"
	"strings"
	"testing"

	"github.com/alecthomas/assert/v2"

	"github.com/jkakar/typesafe-sdk-go"
	"github.com/jkakar/typesafe-sdk-go/typesafetest"
)

// post sends a raw request to the fake server and returns its status, for
// cases the SDK cannot produce.
func post(t *testing.T, srv *typesafetest.Server, path, body string, header http.Header) int {
	t.Helper()
	method := http.MethodPost
	var reader io.Reader = strings.NewReader(body)
	if body == "" {
		method, reader = http.MethodGet, nil
	}
	req, err := http.NewRequestWithContext(t.Context(), method, srv.URL()+path, reader)
	assert.NoError(t, err)
	maps.Copy(req.Header, header)
	resp, err := srv.HTTPClient().Do(req)
	assert.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, resp.Body.Close()) })
	return resp.StatusCode
}

// ticketRequest asks one question of each type.
func ticketRequest() typesafe.Request {
	return typesafe.Request{
		State: "I was charged twice. Please fix this ASAP.",
		Questions: typesafe.Questions{
			"urgent": typesafe.NoulQuestion{Instructions: "Does this convey urgency?"},
			"department": typesafe.ChoiceQuestion{
				Instructions: "Which team should handle this?",
				Criteria:     typesafe.ChoiceCriteria{"technical": nil, "billing": nil},
			},
			"frustration": typesafe.ScoreQuestion{
				Instructions: "How frustrated is the customer?",
				Criteria:     typesafe.ScoreCriteria{"Calm", "Frustrated", "Very angry"},
			},
		},
	}
}

func TestNewServer(t *testing.T) {
	t.Parallel()

	t.Run("answers every question without a handler", func(t *testing.T) {
		t.Parallel()
		srv := typesafetest.NewServer()
		t.Cleanup(srv.Close)

		resp, err := srv.Client().SystemOne(t.Context(), ticketRequest())

		assert.NoError(t, err)
		assert.Equal(t, typesafe.DefaultModel, resp.Model)
		urgent, err := resp.Answers.Noul("urgent")
		assert.NoError(t, err)
		assert.Equal(t, 0.5, urgent.Noul)
		department, err := resp.Answers.Choice("department")
		assert.NoError(t, err)
		assert.Equal(t, "billing", department.Choice)
		assert.Equal(t, 0.5, department.Probabilities["technical"])
		frustration, err := resp.Answers.Score("frustration")
		assert.NoError(t, err)
		assert.Equal(t, 1.0, frustration.Score)
		assert.Equal(t, "Very angry", frustration.Legend[2])
	})

	t.Run("lists the documented aliases without a handler", func(t *testing.T) {
		t.Parallel()
		srv := typesafetest.NewServer()
		t.Cleanup(srv.Close)

		models, err := srv.Client().ListModels(t.Context())

		assert.NoError(t, err)
		assert.Equal(t, 2, len(models))
		assert.Equal(t, "jev-latest", models[0].Name)
	})

	t.Run("rejects a request with no api key", func(t *testing.T) {
		t.Parallel()
		srv := typesafetest.NewServer()
		t.Cleanup(srv.Close)

		assert.Equal(t, http.StatusUnauthorized, post(t, srv, "/v1/models", "", nil))
		assert.Equal(t, http.StatusUnauthorized, post(t, srv, "/v1/systemone", "{}", nil))
	})

	t.Run("reports a question type it cannot answer", func(t *testing.T) {
		t.Parallel()
		srv := typesafetest.NewServer()
		t.Cleanup(srv.Close)

		_, err := srv.Client().SystemOne(t.Context(), typesafe.Request{
			State: "ticket",
			Questions: typesafe.Questions{
				"rank": typesafe.RawQuestion{Type: "rank", JSON: []byte(`{"type":"rank"}`)},
			},
		})

		assert.IsError(t, err, typesafe.ErrUnprocessable)
	})

	t.Run("rejects a request the caller cuts short", func(t *testing.T) {
		t.Parallel()
		srv := typesafetest.NewServer()
		t.Cleanup(srv.Close)

		assert.Equal(t, http.StatusBadRequest, interrupt(t, srv))
	})

	t.Run("rejects a body it cannot decode", func(t *testing.T) {
		t.Parallel()
		srv := typesafetest.NewServer()
		t.Cleanup(srv.Close)
		bodies := map[string]string{
			"malformed request": `{"state":`,
			"malformed state":   `{"state":<>,"model":"jev-latest","questions":{}}`,
			"missing state":     `{"model":"jev-latest","questions":{}}`,
		}
		for name, body := range bodies {
			t.Run(name, func(t *testing.T) {
				t.Parallel()

				status := post(t, srv, "/v1/systemone", body, http.Header{
					"Authorization": {"Bearer " + typesafetest.APIKey},
				})

				assert.Equal(t, http.StatusUnprocessableEntity, status)
			})
		}
	})
}

// interrupt sends a request that stops short of its declared length, so the
// server sees the body end mid-read. Only a raw connection can produce it.
func interrupt(t *testing.T, srv *typesafetest.Server) int {
	t.Helper()
	conn, err := net.Dial("tcp", strings.TrimPrefix(srv.URL(), "http://"))
	assert.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, closeIgnoringClosed(conn)) })
	_, err = fmt.Fprint(conn, "POST /v1/systemone HTTP/1.1\r\nHost: fake\r\n"+
		"Content-Length: 1024\r\n\r\n{\"state\":\"short\"}")
	assert.NoError(t, err)
	assert.NoError(t, conn.(*net.TCPConn).CloseWrite())

	resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
	assert.NoError(t, err)
	assert.NoError(t, resp.Body.Close())
	return resp.StatusCode
}

// closeIgnoringClosed closes conn, tolerating a connection the server already
// closed.
func closeIgnoringClosed(conn net.Conn) error {
	if err := conn.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
		return err
	}
	return nil
}

func TestServer_Client(t *testing.T) {
	t.Parallel()

	t.Run("returns a client pointed at the server", func(t *testing.T) {
		t.Parallel()
		srv := typesafetest.NewServer()
		t.Cleanup(srv.Close)

		client := srv.Client(typesafe.WithModel("jev-preview"))

		assert.Equal(t, srv.URL(), client.BaseURL())
		assert.Equal(t, "jev-preview", client.Model())
	})

	t.Run("panics on an option the client rejects", func(t *testing.T) {
		t.Parallel()
		srv := typesafetest.NewServer()
		t.Cleanup(srv.Close)

		assert.Panics(t, func() { srv.Client(typesafe.WithModel("")) })
	})
}

func TestServer_HandleSystemOne(t *testing.T) {
	t.Parallel()

	t.Run("answers with the registered handler", func(t *testing.T) {
		t.Parallel()
		srv := typesafetest.NewServer()
		t.Cleanup(srv.Close)
		srv.HandleSystemOne(func(_ context.Context, req typesafe.Request) (typesafe.Response, error) {
			assert.Equal(t, "I was charged twice. Please fix this ASAP.", req.State)
			assert.Equal(t, 3, len(req.Questions))
			return typesafe.Response{
				Model:   "jev-1.13.0",
				Answers: typesafe.Answers{"urgent": typesafe.NoulAnswer{Noul: 0.92}},
				Usage:   typesafe.Usage{InputTokens: 10, OutputTokens: 2},
			}, nil
		})

		resp, err := srv.Client().SystemOne(t.Context(), ticketRequest())

		assert.NoError(t, err)
		assert.Equal(t, "jev-1.13.0", resp.Model)
		assert.Equal(t, typesafe.Usage{InputTokens: 10, OutputTokens: 2}, resp.Usage)
	})

	t.Run("writes the failure a handler returns", func(t *testing.T) {
		t.Parallel()
		srv := typesafetest.NewServer()
		t.Cleanup(srv.Close)
		srv.HandleSystemOne(func(context.Context, typesafe.Request) (typesafe.Response, error) {
			return typesafe.Response{}, &typesafetest.Failure{
				StatusCode: http.StatusTooManyRequests,
				Header:     http.Header{"Retry-After": []string{"2"}},
			}
		})
		policy := typesafe.DefaultRetryPolicy()
		policy.MaxRetries = 0

		_, err := srv.Client().SystemOne(t.Context(), ticketRequest(), typesafe.WithRetryPolicy(policy))

		assert.IsError(t, err, typesafe.ErrRateLimit)
		var apiErr *typesafe.APIError
		assert.True(t, errors.As(err, &apiErr))
		assert.Equal(t, "Too Many Requests", apiErr.Message)
	})

	t.Run("writes a server error for any other failure", func(t *testing.T) {
		t.Parallel()
		srv := typesafetest.NewServer()
		t.Cleanup(srv.Close)
		srv.HandleSystemOne(func(context.Context, typesafe.Request) (typesafe.Response, error) {
			return typesafe.Response{}, errors.New("boom")
		})
		policy := typesafe.DefaultRetryPolicy()
		policy.MaxRetries = 0

		_, err := srv.Client().SystemOne(t.Context(), ticketRequest(), typesafe.WithRetryPolicy(policy))

		assert.IsError(t, err, typesafe.ErrServer)
	})

	t.Run("reports an answer it cannot encode", func(t *testing.T) {
		t.Parallel()
		srv := typesafetest.NewServer()
		t.Cleanup(srv.Close)
		srv.HandleSystemOne(func(context.Context, typesafe.Request) (typesafe.Response, error) {
			return typesafe.Response{
				Answers: typesafe.Answers{"urgent": typesafe.ScoreAnswer{Legend: map[int]any{0: make(chan int)}}},
			}, nil
		})
		policy := typesafe.DefaultRetryPolicy()
		policy.MaxRetries = 0

		_, err := srv.Client().SystemOne(t.Context(), ticketRequest(), typesafe.WithRetryPolicy(policy))

		assert.IsError(t, err, typesafe.ErrServer)
	})
}

func TestServer_HandleModels(t *testing.T) {
	t.Parallel()

	t.Run("lists the models the registered handler returns", func(t *testing.T) {
		t.Parallel()
		srv := typesafetest.NewServer()
		t.Cleanup(srv.Close)
		srv.HandleModels(func(context.Context) ([]typesafe.Model, error) {
			return []typesafe.Model{{Name: "jev-1.13.0"}}, nil
		})

		models, err := srv.Client().ListModels(t.Context())

		assert.NoError(t, err)
		assert.Equal(t, []typesafe.Model{{Name: "jev-1.13.0"}}, models)
	})

	t.Run("writes the failure a handler returns", func(t *testing.T) {
		t.Parallel()
		srv := typesafetest.NewServer()
		t.Cleanup(srv.Close)
		srv.HandleModels(func(context.Context) ([]typesafe.Model, error) {
			return nil, &typesafetest.Failure{StatusCode: http.StatusNotFound, Body: `{"error":"gone"}`}
		})

		_, err := srv.Client().ListModels(t.Context())

		assert.IsError(t, err, typesafe.ErrNotFound)
	})
}

func TestServer_Recorded(t *testing.T) {
	t.Parallel()

	t.Run("returns each request the server received", func(t *testing.T) {
		t.Parallel()
		srv := typesafetest.NewServer()
		t.Cleanup(srv.Close)
		client := srv.Client()

		_, err := client.ListModels(t.Context())
		assert.NoError(t, err)
		_, err = client.SystemOne(t.Context(), ticketRequest())
		assert.NoError(t, err)

		recorded := srv.Recorded()
		assert.Equal(t, 2, len(recorded))
		assert.Equal(t, http.MethodGet, recorded[0].Method)
		assert.Equal(t, "/v1/models", recorded[0].Path)
		assert.Equal(t, "/v1/systemone", recorded[1].Path)
		assert.Contains(t, string(recorded[1].Body), `"state":"I was charged twice`)
		assert.Equal(t, "Bearer "+typesafetest.APIKey, recorded[1].Header.Get("Authorization"))
	})
}

func TestDefaultAnswer(t *testing.T) {
	t.Parallel()

	t.Run("answers a yes/no question with even odds", func(t *testing.T) {
		t.Parallel()

		answer, err := typesafetest.DefaultAnswer(typesafe.NoulQuestion{})

		assert.NoError(t, err)
		assert.Equal[typesafe.Answer](t, typesafe.NoulAnswer{Noul: 0.5}, answer)
	})

	t.Run("answers a choice question with a uniform distribution", func(t *testing.T) {
		t.Parallel()
		question := typesafe.ChoiceQuestion{Criteria: typesafe.ChoiceCriteria{"b": nil, "a": nil}}

		answer, err := typesafetest.DefaultAnswer(question)

		assert.NoError(t, err)
		assert.Equal[typesafe.Answer](t, typesafe.ChoiceAnswer{
			Choice:        "a",
			Probabilities: map[string]float64{"a": 0.5, "b": 0.5},
		}, answer)
	})

	t.Run("answers a score question with the middle level", func(t *testing.T) {
		t.Parallel()
		question := typesafe.ScoreQuestion{Criteria: typesafe.ScoreCriteria{"low", "high"}}

		answer, err := typesafetest.DefaultAnswer(question)

		assert.NoError(t, err)
		assert.Equal[typesafe.Answer](t, typesafe.ScoreAnswer{
			Score:         0.5,
			Legend:        map[int]any{0: "low", 1: "high"},
			Probabilities: map[int]float64{0: 0.5, 1: 0.5},
		}, answer)
	})

	t.Run("reports a question type it cannot answer", func(t *testing.T) {
		t.Parallel()

		_, err := typesafetest.DefaultAnswer(typesafe.RawQuestion{Type: "rank"})

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "RawQuestion")
	})
}

func TestFailure_Error(t *testing.T) {
	t.Parallel()

	t.Run("names the status it writes", func(t *testing.T) {
		t.Parallel()
		failure := &typesafetest.Failure{StatusCode: http.StatusTooManyRequests}

		assert.Equal(t, "typesafetest: failing with status 429", failure.Error())
	})
}
