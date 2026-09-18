// Package typesafetest runs a fake TypeSafe API, so a program that calls the
// API can be tested without reaching the network.
//
// A zero-configuration server answers every question with a zero-information
// answer, which is enough to exercise the surrounding code:
//
//	srv := typesafetest.NewServer()
//	defer srv.Close()
//
//	client := srv.Client()
//	resp, err := client.SystemOne(ctx, req)
//
// Register a handler to control the answers, assert on the questions the code
// under test asked, or return a failure:
//
//	srv.HandleSystemOne(func(_ context.Context, req typesafe.Request) (typesafe.Response, error) {
//		return typesafe.Response{
//			Model:   typesafe.DefaultModel,
//			Answers: typesafe.Answers{"urgent": typesafe.NoulAnswer{Noul: 0.92}},
//		}, nil
//	})
package typesafetest

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"

	"github.com/jkakar/typesafe-sdk-go"
)

// APIKey authenticates against a [Server]. A server accepts any non-empty key,
// so a test only needs this value when it asserts on the header.
const APIKey = "test-api-key"

// A SystemOneFunc answers a system one request. Returning a [Failure] writes
// that HTTP response; returning any other error writes 500.
type SystemOneFunc func(ctx context.Context, req typesafe.Request) (typesafe.Response, error)

// A ModelsFunc lists the models available to the account. Returning a
// [Failure] writes that HTTP response; returning any other error writes 500.
type ModelsFunc func(ctx context.Context) ([]typesafe.Model, error)

// A Failure makes a handler write an HTTP error response.
type Failure struct {
	// StatusCode is the status to write.
	StatusCode int
	// Body is the response body. An empty body writes a JSON error object
	// naming the status.
	Body string
	// Header holds extra response headers, such as Retry-After.
	Header http.Header
}

func (f *Failure) Error() string {
	return fmt.Sprintf("typesafetest: failing with status %d", f.StatusCode)
}

// A Recorded request is one request the server received.
type Recorded struct {
	// Method is the request method.
	Method string
	// Path is the request path.
	Path string
	// Header holds the request headers.
	Header http.Header
	// Body holds the request body.
	Body []byte
}

// A Server is a fake TypeSafe API backed by an [httptest.Server]. Its zero
// value is not usable; build one with [NewServer].
type Server struct {
	http *httptest.Server

	mu        sync.Mutex
	systemOne SystemOneFunc
	models    ModelsFunc
	recorded  []Recorded
}

// NewServer starts a fake TypeSafe API. Close it when the test ends, usually
// with t.Cleanup(srv.Close).
func NewServer() *Server {
	srv := &Server{systemOne: defaultSystemOne, models: defaultModels}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/systemone", srv.serveSystemOne)
	mux.HandleFunc("GET /v1/models", srv.serveModels)
	srv.http = httptest.NewServer(srv.record(mux))
	return srv
}

// Close shuts the server down and waits for its requests to finish.
func (s *Server) Close() { s.http.Close() }

// URL returns the server's base URL.
func (s *Server) URL() string { return s.http.URL }

// HTTPClient returns the server's own HTTP client, which owns a connection
// pool no other test shares. Use it to send a request this SDK cannot build.
func (s *Server) HTTPClient() *http.Client { return s.http.Client() }

// Client returns a client pointed at this server, using the server's own HTTP
// client so the test owns its connection pool. It panics when an option is
// invalid, which only a caller mistake can cause.
func (s *Server) Client(opts ...typesafe.Option) *typesafe.Client {
	base := []typesafe.Option{
		typesafe.WithBaseURL(s.URL()),
		typesafe.WithAPIKey(APIKey),
		typesafe.WithHTTPClient(s.http.Client()),
	}
	client, err := typesafe.NewClient(append(base, opts...)...)
	if err != nil {
		panic("typesafetest: " + err.Error())
	}
	return client
}

// HandleSystemOne answers later system one requests with fn.
func (s *Server) HandleSystemOne(fn SystemOneFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.systemOne = fn
}

// HandleModels answers later model list requests with fn.
func (s *Server) HandleModels(fn ModelsFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.models = fn
}

// Recorded returns the requests the server has received, oldest first.
func (s *Server) Recorded() []Recorded {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.recorded)
}

// record remembers each request before handing it to next.
func (s *Server) record(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := readAll(r)
		if err != nil {
			writeFailure(w, &Failure{StatusCode: http.StatusBadRequest, Body: err.Error()})
			return
		}
		s.mu.Lock()
		s.recorded = append(s.recorded, Recorded{
			Method: r.Method,
			Path:   r.URL.Path,
			Header: r.Header.Clone(),
			Body:   body,
		})
		s.mu.Unlock()
		next.ServeHTTP(w, r)
	})
}

func (s *Server) serveSystemOne(w http.ResponseWriter, r *http.Request) {
	if !authorized(w, r) {
		return
	}
	var wire struct {
		State     json.RawMessage    `json:"state"`
		Model     string             `json:"model"`
		Questions typesafe.Questions `json:"questions"`
	}
	if err := json.NewDecoder(r.Body).Decode(&wire); err != nil {
		writeFailure(w, &Failure{StatusCode: http.StatusUnprocessableEntity, Body: err.Error()})
		return
	}
	var state any
	if err := json.Unmarshal(wire.State, &state); err != nil {
		writeFailure(w, &Failure{StatusCode: http.StatusUnprocessableEntity, Body: err.Error()})
		return
	}

	s.mu.Lock()
	handler := s.systemOne
	s.mu.Unlock()
	resp, err := handler(r.Context(), typesafe.Request{State: state, Questions: wire.Questions, Model: wire.Model})
	if err != nil {
		writeError(w, err)
		return
	}
	if resp.Model == "" {
		resp.Model = wire.Model
	}
	writeJSON(w, http.StatusOK, systemOneBody(resp))
}

func (s *Server) serveModels(w http.ResponseWriter, r *http.Request) {
	if !authorized(w, r) {
		return
	}
	s.mu.Lock()
	handler := s.models
	s.mu.Unlock()
	models, err := handler(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"models": models})
}

// defaultSystemOne answers every question with a zero-information answer: a
// noul of one half, the first option of a choice, and the middle level of a
// score, each with a uniform distribution.
func defaultSystemOne(_ context.Context, req typesafe.Request) (typesafe.Response, error) {
	answers := make(typesafe.Answers, len(req.Questions))
	for name, question := range req.Questions {
		answer, err := DefaultAnswer(question)
		if err != nil {
			return typesafe.Response{}, &Failure{
				StatusCode: http.StatusUnprocessableEntity,
				Body:       fmt.Sprintf("question %q: %s", name, err),
			}
		}
		answers[name] = answer
	}
	return typesafe.Response{
		Answers: answers,
		Usage:   typesafe.Usage{InputTokens: 1, OutputTokens: 1},
	}, nil
}

// defaultModels lists the aliases the API documents.
func defaultModels(context.Context) ([]typesafe.Model, error) {
	return []typesafe.Model{
		{Name: "jev-latest", Description: "The most recent stable release.", ReleaseDate: "2026-09-15"},
		{Name: "jev-preview", Description: "The most recent release.", ReleaseDate: "2026-09-15"},
	}, nil
}

// DefaultAnswer returns the zero-information answer a server without a handler
// gives to question. It reports an error for a question type it cannot answer.
func DefaultAnswer(question typesafe.Question) (typesafe.Answer, error) {
	switch q := question.(type) {
	case typesafe.NoulQuestion:
		return typesafe.NoulAnswer{Noul: 0.5}, nil
	case typesafe.ChoiceQuestion:
		return uniformChoice(q.Criteria), nil
	case typesafe.ScoreQuestion:
		return uniformScore(q.Criteria), nil
	default:
		return nil, fmt.Errorf("cannot answer a %T", question)
	}
}

func uniformChoice(criteria typesafe.ChoiceCriteria) typesafe.ChoiceAnswer {
	options := slices.Sorted(maps.Keys(criteria))
	probabilities := make(map[string]float64, len(options))
	for _, option := range options {
		probabilities[option] = 1 / float64(len(options))
	}
	return typesafe.ChoiceAnswer{Choice: options[0], Confidence: 0, Probabilities: probabilities}
}

func uniformScore(criteria typesafe.ScoreCriteria) typesafe.ScoreAnswer {
	legend := make(map[int]any, len(criteria))
	probabilities := make(map[int]float64, len(criteria))
	for level, description := range criteria {
		legend[level] = description
		probabilities[level] = 1 / float64(len(criteria))
	}
	return typesafe.ScoreAnswer{
		Score:         float64(len(criteria)-1) / 2,
		Confidence:    0,
		Legend:        legend,
		Probabilities: probabilities,
	}
}

// authorized rejects a request without a bearer token, as the API does.
func authorized(w http.ResponseWriter, r *http.Request) bool {
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if strings.TrimSpace(token) != "" {
		return true
	}
	writeFailure(w, &Failure{
		StatusCode: http.StatusUnauthorized,
		Body:       `{"error":"missing or invalid api key"}`,
	})
	return false
}

// systemOneBody is the wire form of a response.
func systemOneBody(resp typesafe.Response) map[string]any {
	return map[string]any{
		"model":   resp.Model,
		"answers": resp.Answers,
		"usage": map[string]any{
			"input_tokens":  resp.Usage.InputTokens,
			"output_tokens": resp.Usage.OutputTokens,
		},
	}
}

// readAll reads a request body and leaves a fresh reader in its place. A
// server request always has a body, so there is nothing to guard against.
func readAll(r *http.Request) ([]byte, error) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, err
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	return body, nil
}

// writeError writes a [Failure] as the response it describes, and any other
// error as a server error.
func writeError(w http.ResponseWriter, err error) {
	var failure *Failure
	if errors.As(err, &failure) {
		writeFailure(w, failure)
		return
	}
	writeFailure(w, &Failure{StatusCode: http.StatusInternalServerError, Body: err.Error()})
}

// writeFailure writes the HTTP response a [Failure] describes.
func writeFailure(w http.ResponseWriter, failure *Failure) {
	maps.Copy(w.Header(), failure.Header)
	body := failure.Body
	if body == "" {
		body = fmt.Sprintf("{%q:%q}", "error", http.StatusText(failure.StatusCode))
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(failure.StatusCode)
	//nolint:errcheck // A test server has nowhere to report a broken connection.
	_, _ = io.WriteString(w, body)
}

// writeJSON writes value as a JSON response.
func writeJSON(w http.ResponseWriter, status int, value any) {
	body, err := json.Marshal(value)
	if err != nil {
		writeFailure(w, &Failure{StatusCode: http.StatusInternalServerError, Body: err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	//nolint:errcheck // A test server has nowhere to report a broken connection.
	_, _ = w.Write(body)
}
