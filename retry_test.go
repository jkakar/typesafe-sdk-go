package typesafe_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/alecthomas/assert/v2"

	"github.com/jkakar/typesafe-sdk-go"
)

// fastRetries retries twice with a backoff short enough that a test spends no
// meaningful time waiting, and no jitter so the waits are predictable.
func fastRetries() typesafe.RetryPolicy {
	policy := typesafe.DefaultRetryPolicy()
	policy.InitialBackoff = time.Millisecond
	policy.MaxBackoff = time.Millisecond
	policy.Jitter = 0
	return policy
}

// oneQuestion is the smallest valid request.
func oneQuestion() typesafe.Request {
	return typesafe.Request{
		State:     "ticket",
		Questions: typesafe.Questions{"urgent": typesafe.NoulQuestion{}},
	}
}

// roundTripperFunc adapts a function to [http.RoundTripper]. It stands in for
// the network in tests that drive the fake clock, where real I/O cannot go.
type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

// statusResponse builds a response the client can read without a network.
func statusResponse(status int, header http.Header) *http.Response {
	if header == nil {
		header = http.Header{}
	}
	return &http.Response{
		StatusCode: status,
		Header:     header,
		Body:       io.NopCloser(strings.NewReader("")),
	}
}

func TestDefaultRetryPolicy(t *testing.T) {
	t.Parallel()

	t.Run("retries twice with capped exponential backoff", func(t *testing.T) {
		t.Parallel()

		policy := typesafe.DefaultRetryPolicy()

		assert.Equal(t, 2, policy.MaxRetries)
		assert.Equal(t, 500*time.Millisecond, policy.InitialBackoff)
		assert.Equal(t, 5*time.Second, policy.MaxBackoff)
		assert.Equal(t, 0.25, policy.Jitter)
		assert.True(t, policy.RespectRetryAfter)
		assert.Equal(t, time.Minute, policy.MaxRetryAfter)
		assert.True(t, policy.RetryStatus == nil)
		assert.True(t, policy.RetryError == nil)
	})
}

func TestRetryPolicy(t *testing.T) {
	t.Parallel()

	t.Run("retries a rate limit and returns the answer", func(t *testing.T) {
		t.Parallel()
		var attempts atomic.Int32
		client := newRawServer(t, func(w http.ResponseWriter, _ *http.Request) {
			if attempts.Add(1) == 1 {
				writeJSON(t, w, http.StatusTooManyRequests, `{"error":"slow down"}`)
				return
			}
			writeJSON(t, w, http.StatusOK, `{"model":"jev-latest","usage":{},"answers":{}}`)
		})

		resp, err := client.SystemOne(t.Context(), oneQuestion(), typesafe.WithRetryPolicy(fastRetries()))

		assert.NoError(t, err)
		assert.Equal(t, "jev-latest", resp.Model)
		assert.Equal(t, int32(2), attempts.Load())
	})

	t.Run("counts the retry in a request header", func(t *testing.T) {
		t.Parallel()
		var counts []string
		client := newRawServer(t, func(w http.ResponseWriter, r *http.Request) {
			counts = append(counts, r.Header.Get("X-TypeSafe-Retry-Count"))
			writeJSON(t, w, http.StatusServiceUnavailable, "")
		})

		_, err := client.SystemOne(t.Context(), oneQuestion(), typesafe.WithRetryPolicy(fastRetries()))

		assert.IsError(t, err, typesafe.ErrServer)
		assert.Equal(t, []string{"", "1", "2"}, counts)
	})

	t.Run("stops after the last retry and returns the final error", func(t *testing.T) {
		t.Parallel()
		var attempts atomic.Int32
		client := newRawServer(t, func(w http.ResponseWriter, _ *http.Request) {
			attempts.Add(1)
			writeJSON(t, w, http.StatusInternalServerError, `{"error":"boom"}`)
		})
		policy := fastRetries()
		policy.MaxRetries = 1

		_, err := client.SystemOne(t.Context(), oneQuestion(), typesafe.WithRetryPolicy(policy))

		assert.IsError(t, err, typesafe.ErrServer)
		assert.Equal(t, int32(2), attempts.Load())
	})

	t.Run("does not retry a status it excludes", func(t *testing.T) {
		t.Parallel()
		var attempts atomic.Int32
		client := newRawServer(t, func(w http.ResponseWriter, _ *http.Request) {
			attempts.Add(1)
			writeJSON(t, w, http.StatusBadRequest, `{"error":"bad"}`)
		})

		_, err := client.SystemOne(t.Context(), oneQuestion(), typesafe.WithRetryPolicy(fastRetries()))

		assert.IsError(t, err, typesafe.ErrBadRequest)
		assert.Equal(t, int32(1), attempts.Load())
	})

	t.Run("retries the statuses a caller names", func(t *testing.T) {
		t.Parallel()
		var attempts atomic.Int32
		client := newRawServer(t, func(w http.ResponseWriter, _ *http.Request) {
			attempts.Add(1)
			writeJSON(t, w, http.StatusBadRequest, `{"error":"bad"}`)
		})
		policy := fastRetries()
		policy.MaxRetries = 1
		policy.RetryStatus = func(status int) bool { return status == http.StatusBadRequest }

		_, err := client.SystemOne(t.Context(), oneQuestion(), typesafe.WithRetryPolicy(policy))

		assert.IsError(t, err, typesafe.ErrBadRequest)
		assert.Equal(t, int32(2), attempts.Load())
	})

	t.Run("retries a connection the server drops", func(t *testing.T) {
		t.Parallel()
		var attempts atomic.Int32
		client := newRawServer(t, func(w http.ResponseWriter, r *http.Request) {
			if attempts.Add(1) == 1 {
				// A closed connection is the transport failure a
				// retry exists for, and only the server can cause it.
				r.Close = true
				w.Header().Set("Connection", "close")
				hijacked, _, err := w.(http.Hijacker).Hijack()
				assert.NoError(t, err)
				assert.NoError(t, hijacked.Close())
				return
			}
			writeJSON(t, w, http.StatusOK, `{"model":"jev-latest","usage":{},"answers":{}}`)
		})

		resp, err := client.SystemOne(t.Context(), oneQuestion(), typesafe.WithRetryPolicy(fastRetries()))

		assert.NoError(t, err)
		assert.Equal(t, "jev-latest", resp.Model)
		assert.Equal(t, int32(2), attempts.Load())
	})

	t.Run("does not retry an error a caller excludes", func(t *testing.T) {
		t.Parallel()
		var attempts atomic.Int32
		client := newRawServer(t, func(w http.ResponseWriter, _ *http.Request) {
			attempts.Add(1)
			hijacked, _, err := w.(http.Hijacker).Hijack()
			assert.NoError(t, err)
			assert.NoError(t, hijacked.Close())
		})
		policy := fastRetries()
		policy.RetryError = func(error) bool { return false }

		_, err := client.SystemOne(t.Context(), oneQuestion(), typesafe.WithRetryPolicy(policy))

		assert.Error(t, err)
		assert.Equal(t, int32(1), attempts.Load())
	})

	t.Run("stops retrying when the caller cancels during the backoff", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := context.WithCancel(t.Context())
		var attempts atomic.Int32
		client := newRawServer(t, func(w http.ResponseWriter, _ *http.Request) {
			attempts.Add(1)
			cancel()
			writeJSON(t, w, http.StatusServiceUnavailable, "")
		})
		policy := fastRetries()
		policy.InitialBackoff = time.Hour
		policy.MaxBackoff = time.Hour

		_, err := client.SystemOne(ctx, oneQuestion(), typesafe.WithRetryPolicy(policy))

		assert.IsError(t, err, context.Canceled)
		assert.Equal(t, int32(1), attempts.Load())
	})

	t.Run("waits the backoff the policy sets", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			var sent []time.Time
			transport := roundTripperFunc(func(*http.Request) (*http.Response, error) {
				sent = append(sent, time.Now())
				return statusResponse(http.StatusServiceUnavailable, nil), nil
			})
			client := newStubClient(t, transport, typesafe.RetryPolicy{
				MaxRetries:     2,
				InitialBackoff: time.Second,
				MaxBackoff:     time.Minute,
			})

			_, err := client.SystemOne(t.Context(), oneQuestion())

			assert.IsError(t, err, typesafe.ErrServer)
			assert.Equal(t, 3, len(sent))
			assert.Equal(t, time.Second, sent[1].Sub(sent[0]))
			assert.Equal(t, 2*time.Second, sent[2].Sub(sent[1]))
		})
	})

	t.Run("caps the backoff at the maximum", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			var sent []time.Time
			transport := roundTripperFunc(func(*http.Request) (*http.Response, error) {
				sent = append(sent, time.Now())
				return statusResponse(http.StatusServiceUnavailable, nil), nil
			})
			client := newStubClient(t, transport, typesafe.RetryPolicy{
				MaxRetries:     3,
				InitialBackoff: time.Second,
				MaxBackoff:     time.Second,
			})

			_, err := client.SystemOne(t.Context(), oneQuestion())

			assert.IsError(t, err, typesafe.ErrServer)
			assert.Equal(t, time.Second, sent[2].Sub(sent[1]))
			assert.Equal(t, time.Second, sent[3].Sub(sent[2]))
		})
	})

	t.Run("waits as long as the server asks", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			var sent []time.Time
			transport := roundTripperFunc(func(*http.Request) (*http.Response, error) {
				sent = append(sent, time.Now())
				return statusResponse(http.StatusTooManyRequests,
					http.Header{"Retry-After": []string{"30"}}), nil
			})
			client := newStubClient(t, transport, typesafe.RetryPolicy{
				MaxRetries:        1,
				InitialBackoff:    time.Second,
				MaxBackoff:        time.Second,
				RespectRetryAfter: true,
				MaxRetryAfter:     time.Minute,
			})

			_, err := client.SystemOne(t.Context(), oneQuestion())

			assert.IsError(t, err, typesafe.ErrRateLimit)
			assert.Equal(t, 30*time.Second, sent[1].Sub(sent[0]))
		})
	})

	t.Run("ignores a server delay longer than the maximum", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			var sent []time.Time
			transport := roundTripperFunc(func(*http.Request) (*http.Response, error) {
				sent = append(sent, time.Now())
				return statusResponse(http.StatusTooManyRequests,
					http.Header{"Retry-After": []string{"3600"}}), nil
			})
			client := newStubClient(t, transport, typesafe.RetryPolicy{
				MaxRetries:        1,
				InitialBackoff:    time.Second,
				MaxBackoff:        time.Second,
				RespectRetryAfter: true,
				MaxRetryAfter:     time.Minute,
			})

			_, err := client.SystemOne(t.Context(), oneQuestion())

			assert.IsError(t, err, typesafe.ErrRateLimit)
			assert.Equal(t, time.Second, sent[1].Sub(sent[0]))
		})
	})

	t.Run("stops retrying when the context expires during the backoff", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			var attempts int
			transport := roundTripperFunc(func(*http.Request) (*http.Response, error) {
				attempts++
				return statusResponse(http.StatusServiceUnavailable, nil), nil
			})
			client := newStubClient(t, transport, typesafe.RetryPolicy{
				MaxRetries:     2,
				InitialBackoff: time.Hour,
				MaxBackoff:     time.Hour,
			})
			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
			defer cancel()

			_, err := client.SystemOne(ctx, oneQuestion())

			assert.IsError(t, err, context.DeadlineExceeded)
			assert.Equal(t, 1, attempts)
		})
	})

	t.Run("reports the server delay on a rate limit", func(t *testing.T) {
		t.Parallel()
		client := newRawServer(t, func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Retry-After-Ms", "1500")
			writeJSON(t, w, http.StatusTooManyRequests, `{"error":"slow down"}`)
		})
		policy := fastRetries()
		policy.MaxRetries = 0

		_, err := client.SystemOne(t.Context(), oneQuestion(), typesafe.WithRetryPolicy(policy))

		var apiErr *typesafe.APIError
		assert.True(t, errors.As(err, &apiErr))
		assert.Equal(t, 1500*time.Millisecond, apiErr.RetryAfter)
	})
}
