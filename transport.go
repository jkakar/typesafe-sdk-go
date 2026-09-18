package typesafe

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// API paths this SDK calls.
const (
	pathSystemOne = "/v1/systemone"
	pathModels    = "/v1/models"
)

// Headers the SDK sets on every request and reads from every response.
const (
	headerRequestID  = "X-Typesafe-Request-Id"
	headerSDK        = "X-Typesafe-Sdk"
	headerRuntime    = "X-Typesafe-Runtime"
	headerRetryCount = "X-Typesafe-Retry-Count"
)

const contentTypeJSON = "application/json"

// goRuntime describes the running program for the X-Typesafe-Runtime header.
var goRuntime = fmt.Sprintf("go/%s (%s; %s)", strings.TrimPrefix(runtime.Version(), "go"), runtime.GOOS, runtime.GOARCH)

// result carries a successful response and the body already read from it.
type result struct {
	request  *http.Request
	response *http.Response
	body     []byte
}

// send performs one API call, retrying the failures the policy allows. The
// returned response's body is replaced with a fresh reader over the bytes
// already read, so a caller can read it again.
func (c *Client) send(ctx context.Context, cfg config, method, path string, body []byte) (*result, error) {
	url := cfg.baseURL + path
	var lastErr error
	for attempt := 0; ; attempt++ {
		if attempt > 0 {
			delay := cfg.retry.backoff(attempt, retryAfterOf(lastErr))
			cfg.logger.LogAttrs(ctx, slog.LevelInfo, "typesafe: retrying request",
				slog.String("method", method), slog.String("url", url),
				slog.Int("attempt", attempt), slog.Duration("delay", delay),
				slog.String("reason", lastErr.Error()))
			if err := wait(ctx, delay); err != nil {
				return nil, fmt.Errorf("%s %s: %w", method, url, err)
			}
		}
		res, err := c.attempt(ctx, cfg, method, url, body, attempt)
		if err == nil {
			return res, nil
		}
		if attempt >= cfg.retry.MaxRetries || !retryable(cfg.retry, err) {
			return nil, err
		}
		lastErr = err
	}
}

// attempt sends one HTTP request and reads its response.
func (c *Client) attempt(ctx context.Context, cfg config, method, url string, body []byte, attempt int) (*result, error) {
	req, err := newRequest(ctx, cfg, method, url, body, attempt)
	if err != nil {
		return nil, err
	}
	cfg.logger.LogAttrs(ctx, slog.LevelDebug, "typesafe: sending request",
		slog.String("method", method), slog.String("url", url),
		slog.Any("headers", redacted(req.Header)), slog.String("body", string(body)))

	started := time.Now()
	resp, err := cfg.httpClient.Do(req) //nolint:bodyclose // readBody closes it.
	if err != nil {
		cfg.logger.LogAttrs(ctx, slog.LevelInfo, "typesafe: request failed",
			slog.String("method", method), slog.String("url", url),
			slog.Duration("duration", time.Since(started)), slog.Any("error", err))
		return nil, fmt.Errorf("%s %s: %w", method, url, err)
	}
	responseBody, err := readBody(resp)
	if err != nil {
		return nil, fmt.Errorf("%s %s: read response body: %w", method, url, err)
	}
	cfg.logger.LogAttrs(ctx, slog.LevelInfo, "typesafe: received response",
		slog.String("method", method), slog.String("url", url),
		slog.Int("status", resp.StatusCode), slog.Duration("duration", time.Since(started)),
		slog.String("request_id", resp.Header.Get(headerRequestID)))
	cfg.logger.LogAttrs(ctx, slog.LevelDebug, "typesafe: received body",
		slog.String("method", method), slog.String("url", url),
		slog.Any("headers", redacted(resp.Header)), slog.String("body", string(responseBody)))

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, newAPIError(req, resp, responseBody)
	}
	return &result{request: req, response: resp, body: responseBody}, nil
}

// newRequest builds one attempt's HTTP request. Caller headers are applied
// first so they cannot displace the headers the SDK owns.
func newRequest(ctx context.Context, cfg config, method, url string, body []byte, attempt int) (*http.Request, error) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, reader)
	if err != nil {
		return nil, fmt.Errorf("build %s %s request: %w", method, url, err)
	}
	req.Header = cfg.header.Clone()
	req.Header.Set("Authorization", "Bearer "+cfg.apiKey)
	req.Header.Set("Accept", contentTypeJSON)
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set(headerSDK, userAgent)
	req.Header.Set(headerRuntime, goRuntime)
	req.Header.Del(headerRetryCount)
	if body != nil {
		req.Header.Set("Content-Type", contentTypeJSON)
	}
	if attempt > 0 {
		req.Header.Set(headerRetryCount, strconv.Itoa(attempt))
	}
	return req, nil
}

// readBody reads and closes a response body.
func readBody(resp *http.Response) ([]byte, error) {
	defer resp.Body.Close() //nolint:errcheck // Nothing useful remains to report on a read-only body.
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	// Hand the bytes back so a caller reading the raw response sees a body.
	resp.Body = io.NopCloser(bytes.NewReader(body))
	return body, nil
}

// retryable reports whether the policy retries a failed attempt.
func retryable(policy RetryPolicy, err error) bool {
	if apiErr := asAPIError(err); apiErr != nil {
		return policy.retriesStatus(apiErr.StatusCode)
	}
	return policy.retriesError(err)
}

// retryAfterOf returns the delay the server asked for, or zero when the
// failure carried none.
func retryAfterOf(err error) time.Duration {
	if apiErr := asAPIError(err); apiErr != nil {
		return apiErr.RetryAfter
	}
	return 0
}

// asAPIError returns the [APIError] in err, or nil when err is not one.
func asAPIError(err error) *APIError {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr
	}
	return nil
}

// secretHeaders are redacted before a header reaches a log.
var secretHeaders = map[string]bool{
	"Authorization":       true,
	"Proxy-Authorization": true,
	"X-Api-Key":           true,
	"Api-Key":             true,
	"Cookie":              true,
	"Set-Cookie":          true,
}

// redacted returns header with credential values replaced, so debug logging
// cannot leak an API key.
func redacted(header http.Header) slog.Value {
	attrs := make([]slog.Attr, 0, len(header))
	for _, name := range sortedKeys(header) {
		value := strings.Join(header[name], ", ")
		if secretHeaders[name] {
			value = "***"
		}
		attrs = append(attrs, slog.String(name, value))
	}
	return slog.GroupValue(attrs...)
}
