package typesafe

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// A Request asks named questions about one piece of state.
type Request struct {
	// State is the content every question refers to. It must encode as a
	// JSON string, object, or array, so a string, map, slice, or struct
	// with JSON tags all work. See
	// https://docs.typesafe.ai/concepts/state.
	State any

	// Questions holds at least one question, keyed by the names their
	// answers come back under.
	Questions Questions

	// Model names the model that answers this request. An empty value uses
	// the client's model.
	Model string
}

// A Response holds one answer per question, with the model that answered and
// the tokens it used.
type Response struct {
	// Model is the versioned model that answered, which may differ from the
	// alias the request named.
	Model string `json:"model"`

	// Answers holds one answer per question, keyed by the request's names.
	Answers Answers `json:"answers"`

	// Usage reports the tokens the request used.
	Usage Usage `json:"usage"`

	// RequestID is the x-typesafe-request-id response header, which support
	// uses to find the request.
	RequestID string `json:"-"`

	// HTTPResponse is the response the answers were read from. Its body is
	// a fresh reader over the bytes already read.
	HTTPResponse *http.Response `json:"-"`
}

// Usage reports the tokens one request used. Input tokens are billed and
// output tokens are currently free.
type Usage struct {
	// InputTokens counts the billable tokens the request consumed.
	InputTokens int `json:"input_tokens"`
	// OutputTokens counts the tokens the answers consumed.
	OutputTokens int `json:"output_tokens"`
}

// systemOneRequest is the wire form of a [Request].
type systemOneRequest struct {
	State     json.RawMessage `json:"state"`
	Model     string          `json:"model"`
	Questions Questions       `json:"questions"`
}

// SystemOne answers req's questions about its state. See
// https://docs.typesafe.ai/concepts/system-one.
//
// The options override the client's settings for this call alone. The context
// bounds the whole call, including retries and the waits between them.
//
// SystemOne reports [ErrNoQuestions], [ErrNoCriteria], or [ErrInvalidState]
// for a request the API would reject, an [APIError] for an unsuccessful
// response, and a [DecodeError] for a response it cannot read.
func (c *Client) SystemOne(ctx context.Context, req Request, opts ...Option) (*Response, error) {
	cfg, err := c.callConfig(opts)
	if err != nil {
		return nil, err
	}
	body, err := encodeRequest(cfg, req)
	if err != nil {
		return nil, err
	}
	res, err := c.send(ctx, cfg, http.MethodPost, pathSystemOne, body)
	if err != nil {
		return nil, err
	}
	var resp Response
	if err := json.Unmarshal(res.body, &resp); err != nil {
		return nil, newDecodeError(res.request, res.response, res.body, err)
	}
	resp.RequestID = res.response.Header.Get(headerRequestID)
	resp.HTTPResponse = res.response
	return &resp, nil
}

// encodeRequest validates req and encodes it as the API's request body.
func encodeRequest(cfg config, req Request) ([]byte, error) {
	if err := req.Questions.validate(); err != nil {
		return nil, err
	}
	state, err := json.Marshal(req.State)
	if err != nil {
		return nil, fmt.Errorf("encode state: %w", err)
	}
	if err := validateState(state); err != nil {
		return nil, err
	}
	model := req.Model
	if model == "" {
		model = cfg.model
	}
	body, err := json.Marshal(systemOneRequest{
		State:     json.RawMessage(state),
		Model:     model,
		Questions: req.Questions,
	})
	if err != nil {
		return nil, fmt.Errorf("encode request: %w", err)
	}
	return body, nil
}

// validateState rejects state the API does not accept. The API takes text, a
// JSON object, or a JSON array, so a number, boolean, or null is a caller
// mistake worth naming before a round trip.
func validateState(state []byte) error {
	switch state[0] {
	case '"', '{', '[':
		return nil
	default:
		return fmt.Errorf("%w: state must encode as a JSON string, object, or array, not %s",
			ErrInvalidState, truncate(string(state), 32))
	}
}
