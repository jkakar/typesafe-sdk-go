package typesafe

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Sentinels naming the kinds of unsuccessful HTTP response the API returns.
// Every [APIError] unwraps to one of them, so callers match a kind with
// [errors.Is] and read the detail with [errors.As].
var (
	// ErrBadRequest reports HTTP 400.
	ErrBadRequest = errors.New("bad request")
	// ErrAuthentication reports HTTP 401, a missing or invalid API key.
	ErrAuthentication = errors.New("authentication failed")
	// ErrPermissionDenied reports HTTP 403.
	ErrPermissionDenied = errors.New("permission denied")
	// ErrNotFound reports HTTP 404.
	ErrNotFound = errors.New("not found")
	// ErrUnprocessable reports HTTP 422, a body that failed validation.
	ErrUnprocessable = errors.New("unprocessable entity")
	// ErrRateLimit reports HTTP 429, an exceeded rate limit.
	ErrRateLimit = errors.New("rate limit exceeded")
	// ErrOverloaded reports HTTP 529, a temporarily overloaded service.
	ErrOverloaded = errors.New("overloaded")
	// ErrServer reports any other HTTP 5xx status.
	ErrServer = errors.New("server error")
	// ErrAPI reports an unsuccessful status with no more specific kind.
	ErrAPI = errors.New("api error")
)

// statusOverloaded is the status the API returns when it is temporarily
// overloaded. It has no [net/http] constant.
const statusOverloaded = 529

// maxMessageLength caps how much of an undescribed error body reaches the
// error message.
const maxMessageLength = 200

// An APIError reports an unsuccessful HTTP response. It unwraps to the
// sentinel for its status.
type APIError struct {
	// Method is the HTTP method of the failed request.
	Method string
	// URL is the requested URL, without query or fragment.
	URL string
	// StatusCode is the HTTP status code.
	StatusCode int
	// Message describes the failure, taken from the response body when the
	// body describes it.
	Message string
	// RequestID is the x-typesafe-request-id response header, which support
	// uses to find the request.
	RequestID string
	// RetryAfter is the delay the server asked for, or zero when it asked
	// for none.
	RetryAfter time.Duration
	// Header holds the response headers.
	Header http.Header
	// Body holds the response body.
	Body []byte
}

func (e *APIError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s: %d %s", e.Method, e.URL, e.StatusCode, errorKind(e.StatusCode))
	if e.Message != "" {
		fmt.Fprintf(&b, ": %s", e.Message)
	}
	if e.RequestID != "" {
		fmt.Fprintf(&b, " (request %s)", e.RequestID)
	}
	return b.String()
}

// Unwrap returns the sentinel naming this error's kind, derived from
// StatusCode.
func (e *APIError) Unwrap() error { return errorKind(e.StatusCode) }

// newAPIError builds the error for an unsuccessful response.
func newAPIError(req *http.Request, resp *http.Response, body []byte) *APIError {
	return &APIError{
		Method:     req.Method,
		URL:        endpoint(req),
		StatusCode: resp.StatusCode,
		Message:    errorMessage(body),
		RequestID:  resp.Header.Get(headerRequestID),
		RetryAfter: retryAfter(resp.Header, time.Now()),
		Header:     resp.Header.Clone(),
		Body:       body,
	}
}

// errorKind returns the sentinel for an HTTP status code.
func errorKind(status int) error {
	switch status {
	case http.StatusBadRequest:
		return ErrBadRequest
	case http.StatusUnauthorized:
		return ErrAuthentication
	case http.StatusForbidden:
		return ErrPermissionDenied
	case http.StatusNotFound:
		return ErrNotFound
	case http.StatusUnprocessableEntity:
		return ErrUnprocessable
	case http.StatusTooManyRequests:
		return ErrRateLimit
	case statusOverloaded:
		return ErrOverloaded
	}
	if status >= http.StatusInternalServerError {
		return ErrServer
	}
	return ErrAPI
}

// A DecodeError reports a successful HTTP response whose body the SDK could
// not decode.
type DecodeError struct {
	// Method is the HTTP method of the request.
	Method string
	// URL is the requested URL, without query or fragment.
	URL string
	// StatusCode is the HTTP status code.
	StatusCode int
	// RequestID is the x-typesafe-request-id response header.
	RequestID string
	// Body holds the response body.
	Body []byte
	// Err is the decoding failure.
	Err error
}

func (e *DecodeError) Error() string {
	msg := fmt.Sprintf("%s %s: decode response: %s", e.Method, e.URL, e.Err)
	if e.RequestID != "" {
		msg += fmt.Sprintf(" (request %s)", e.RequestID)
	}
	return msg
}

// Unwrap returns the decoding failure.
func (e *DecodeError) Unwrap() error { return e.Err }

// newDecodeError builds the error for a body that did not decode.
func newDecodeError(req *http.Request, resp *http.Response, body []byte, err error) *DecodeError {
	return &DecodeError{
		Method:     req.Method,
		URL:        endpoint(req),
		StatusCode: resp.StatusCode,
		RequestID:  resp.Header.Get(headerRequestID),
		Body:       body,
		Err:        err,
	}
}

// endpoint describes a request's target without its query or fragment, which
// can carry credentials.
func endpoint(req *http.Request) string {
	url := *req.URL
	url.User = nil
	url.RawQuery = ""
	url.Fragment = ""
	return url.String()
}

// errorMessage extracts the server's description of a failure. It understands
// the shapes the API and its proxies use, and falls back to the body itself.
func errorMessage(body []byte) string {
	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" {
		return ""
	}
	if message := unwrapMessage(body); message != "" {
		return truncate(message, maxMessageLength)
	}
	var payload errorPayload
	if err := json.Unmarshal(body, &payload); err == nil {
		if message := payload.message(); message != "" {
			return truncate(message, maxMessageLength)
		}
	}
	return truncate(trimmed, maxMessageLength)
}

// errorPayload covers the nested error bodies the API and its proxies return:
// a string or object under "error", and the string, object, or validation
// list the server sends under "detail". A top-level "message" needs no entry,
// because unwrapMessage reads it from the body itself.
type errorPayload struct {
	Error  json.RawMessage `json:"error"`
	Detail json.RawMessage `json:"detail"`
}

func (p errorPayload) message() string {
	if message := unwrapMessage(p.Error); message != "" {
		return message
	}
	if message := unwrapMessage(p.Detail); message != "" {
		return message
	}
	return validationMessage(p.Detail)
}

// unwrapMessage reads a string, or the message field of an object.
func unwrapMessage(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return text
	}
	var object struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(raw, &object); err == nil {
		return object.Message
	}
	return ""
}

// validationMessage formats a validation failure list as "field: reason"
// entries joined by semicolons.
func validationMessage(raw json.RawMessage) string {
	var failures []struct {
		Location []any  `json:"loc"`
		Message  string `json:"msg"`
	}
	if err := json.Unmarshal(raw, &failures); err != nil {
		return ""
	}
	parts := make([]string, 0, len(failures))
	for _, failure := range failures {
		if failure.Message == "" {
			continue
		}
		if path := fieldPath(failure.Location); path != "" {
			parts = append(parts, path+": "+failure.Message)
			continue
		}
		parts = append(parts, failure.Message)
	}
	return strings.Join(parts, "; ")
}

// fieldPath joins a validation failure's location, dropping the "body" prefix
// the server adds.
func fieldPath(location []any) string {
	segments := make([]string, 0, len(location))
	for _, segment := range location {
		text := fmt.Sprint(segment)
		if text == "body" {
			continue
		}
		segments = append(segments, text)
	}
	return strings.Join(segments, ".")
}

// truncate shortens text to limit runes, marking what it dropped.
func truncate(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit]) + "…"
}
