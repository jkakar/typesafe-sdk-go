package typesafe

import (
	"context"
	"encoding/json"
	"net/http"
)

// A Model is a model or alias the account can name in a request. See
// https://docs.typesafe.ai/models.
type Model struct {
	// Name is the value to send in a request's model field.
	Name string `json:"name"`
	// Description says what the model does.
	Description string `json:"description"`
	// ReleaseDate is the model's release date, as the API sends it. Treat
	// the format as unspecified: the published schema documents
	// YYYY-MM-DD and the API returns an RFC 3339 timestamp, so parse it
	// defensively or display it as the text it is.
	ReleaseDate string `json:"release_date"`
}

// ListModels returns the models the account can use. The list holds the
// aliases; a versioned name such as jev-1.13.0 works whether or not it appears
// here.
//
// The options override the client's settings for this call alone. The context
// bounds the whole call, including retries and the waits between them.
func (c *Client) ListModels(ctx context.Context, opts ...Option) ([]Model, error) {
	cfg, err := c.callConfig(opts)
	if err != nil {
		return nil, err
	}
	res, err := c.send(ctx, cfg, http.MethodGet, pathModels, nil)
	if err != nil {
		return nil, err
	}
	var payload struct {
		Models []Model `json:"models"`
	}
	if err := json.Unmarshal(res.body, &payload); err != nil {
		return nil, newDecodeError(res.request, res.response, res.body, err)
	}
	return payload.Models, nil
}
