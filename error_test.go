package typesafe_test

import (
	"errors"
	"net/http"
	"testing"

	"github.com/alecthomas/assert/v2"

	"github.com/jkakar/typesafe-sdk-go"
)

func TestAPIError_Error(t *testing.T) {
	t.Parallel()

	t.Run("names the request, status, message and request id", func(t *testing.T) {
		t.Parallel()
		err := &typesafe.APIError{
			Method:     http.MethodPost,
			URL:        "https://api.typesafe.ai/v1/systemone",
			StatusCode: http.StatusTooManyRequests,
			Message:    "slow down",
			RequestID:  "req_1",
		}

		assert.Equal(t, "POST https://api.typesafe.ai/v1/systemone: 429 rate limit exceeded: "+
			"slow down (request req_1)", err.Error())
	})

	t.Run("omits a message and request id it does not have", func(t *testing.T) {
		t.Parallel()
		err := &typesafe.APIError{
			Method:     http.MethodGet,
			URL:        "https://api.typesafe.ai/v1/models",
			StatusCode: http.StatusForbidden,
		}

		assert.Equal(t, "GET https://api.typesafe.ai/v1/models: 403 permission denied", err.Error())
	})
}

func TestAPIError_Unwrap(t *testing.T) {
	t.Parallel()

	t.Run("returns the sentinel for the status code", func(t *testing.T) {
		t.Parallel()
		kinds := map[int]error{
			http.StatusBadRequest:          typesafe.ErrBadRequest,
			http.StatusUnauthorized:        typesafe.ErrAuthentication,
			http.StatusForbidden:           typesafe.ErrPermissionDenied,
			http.StatusNotFound:            typesafe.ErrNotFound,
			http.StatusUnprocessableEntity: typesafe.ErrUnprocessable,
			http.StatusTooManyRequests:     typesafe.ErrRateLimit,
			529:                            typesafe.ErrOverloaded,
			http.StatusInternalServerError: typesafe.ErrServer,
			http.StatusBadGateway:          typesafe.ErrServer,
			http.StatusTeapot:              typesafe.ErrAPI,
		}
		for status, want := range kinds {
			t.Run(http.StatusText(status), func(t *testing.T) {
				t.Parallel()
				err := &typesafe.APIError{StatusCode: status}

				assert.IsError(t, err, want)
			})
		}
	})
}

func TestDecodeError_Error(t *testing.T) {
	t.Parallel()

	t.Run("names the request and the decoding failure", func(t *testing.T) {
		t.Parallel()
		err := &typesafe.DecodeError{
			Method:    http.MethodPost,
			URL:       "https://api.typesafe.ai/v1/systemone",
			RequestID: "req_1",
			Err:       errors.New("unexpected end of JSON input"),
		}

		assert.Equal(t, "POST https://api.typesafe.ai/v1/systemone: decode response: "+
			"unexpected end of JSON input (request req_1)", err.Error())
	})

	t.Run("omits a request id it does not have", func(t *testing.T) {
		t.Parallel()
		err := &typesafe.DecodeError{
			Method: http.MethodGet,
			URL:    "https://api.typesafe.ai/v1/models",
			Err:    errors.New("boom"),
		}

		assert.Equal(t, "GET https://api.typesafe.ai/v1/models: decode response: boom", err.Error())
	})
}

func TestDecodeError_Unwrap(t *testing.T) {
	t.Parallel()

	t.Run("returns the decoding failure", func(t *testing.T) {
		t.Parallel()
		cause := errors.New("boom")
		err := &typesafe.DecodeError{Err: cause}

		assert.IsError(t, err, cause)
	})
}
