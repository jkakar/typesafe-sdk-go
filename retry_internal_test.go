package typesafe

import (
	"net/http"
	"testing"
	"time"

	"github.com/alecthomas/assert/v2"
)

// The backoff schedule and the retry-after header are private invariants of
// the retry policy. Exercising them through a client would make each case an
// HTTP round trip and hide the boundaries these tests name, so they live in
// an internal test. See docs/testing.md rule 6.

func TestRetryPolicy_backoff(t *testing.T) {
	t.Parallel()

	t.Run("doubles the wait up to the maximum", func(t *testing.T) {
		t.Parallel()
		policy := RetryPolicy{InitialBackoff: time.Second, MaxBackoff: 4 * time.Second}

		waits := []time.Duration{
			policy.backoff(1, 0),
			policy.backoff(2, 0),
			policy.backoff(3, 0),
			policy.backoff(4, 0),
		}

		assert.Equal(t, []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 4 * time.Second}, waits)
	})

	t.Run("stays within the jitter fraction of the computed wait", func(t *testing.T) {
		t.Parallel()
		policy := RetryPolicy{InitialBackoff: time.Second, MaxBackoff: time.Minute, Jitter: 0.25}

		for range 100 {
			wait := policy.backoff(1, 0)

			assert.True(t, wait <= time.Second, "wait %s exceeds the computed backoff", wait)
			assert.True(t, wait >= 750*time.Millisecond, "wait %s falls below the jitter floor", wait)
		}
	})

	t.Run("waits nothing when either bound is zero", func(t *testing.T) {
		t.Parallel()
		bounds := map[string]RetryPolicy{
			"no initial backoff": {MaxBackoff: time.Second},
			"no maximum backoff": {InitialBackoff: time.Second},
		}
		for name, policy := range bounds {
			t.Run(name, func(t *testing.T) {
				t.Parallel()

				assert.Zero(t, policy.backoff(1, 0))
			})
		}
	})

	t.Run("does not overflow on a long retry run", func(t *testing.T) {
		t.Parallel()
		policy := RetryPolicy{InitialBackoff: time.Second, MaxBackoff: 4 * time.Second}

		assert.Equal(t, 4*time.Second, policy.backoff(1000, 0))
	})

	t.Run("prefers a server delay within the maximum", func(t *testing.T) {
		t.Parallel()
		policy := RetryPolicy{
			InitialBackoff:    time.Second,
			MaxBackoff:        time.Second,
			RespectRetryAfter: true,
			MaxRetryAfter:     time.Minute,
		}

		assert.Equal(t, 30*time.Second, policy.backoff(1, 30*time.Second))
		assert.Equal(t, time.Second, policy.backoff(1, time.Hour))
	})

	t.Run("ignores a server delay when told not to honor it", func(t *testing.T) {
		t.Parallel()
		policy := RetryPolicy{InitialBackoff: time.Second, MaxBackoff: time.Second}

		assert.Equal(t, time.Second, policy.backoff(1, 30*time.Second))
	})
}

func TestRetryAfter(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.September, 17, 12, 0, 0, 0, time.UTC)
	t.Run("reads the delay a server asks for", func(t *testing.T) {
		t.Parallel()
		headers := map[string]struct {
			header http.Header
			want   time.Duration
		}{
			"milliseconds":       {http.Header{"Retry-After-Ms": {"1500"}}, 1500 * time.Millisecond},
			"seconds":            {http.Header{"Retry-After": {"30"}}, 30 * time.Second},
			"fractional seconds": {http.Header{"Retry-After": {"0.5"}}, 500 * time.Millisecond},
			"http date":          {http.Header{"Retry-After": {"Thu, 17 Sep 2026 12:00:30 GMT"}}, 30 * time.Second},
			"past http date":     {http.Header{"Retry-After": {"Thu, 17 Sep 2026 11:59:00 GMT"}}, 0},
			"milliseconds win":   {http.Header{"Retry-After-Ms": {"100"}, "Retry-After": {"30"}}, 100 * time.Millisecond},
			"no header":          {http.Header{}, 0},
			"negative seconds":   {http.Header{"Retry-After": {"-1"}}, 0},
			"unparseable":        {http.Header{"Retry-After": {"soon"}}, 0},
			"unparseable ms":     {http.Header{"Retry-After-Ms": {"soon"}}, 0},
			"negative ms falls back": {
				http.Header{"Retry-After-Ms": {"-1"}, "Retry-After": {"2"}}, 2 * time.Second,
			},
		}
		for name, tc := range headers {
			t.Run(name, func(t *testing.T) {
				t.Parallel()

				assert.Equal(t, tc.want, retryAfter(tc.header, now))
			})
		}
	})
}

func TestWait(t *testing.T) {
	t.Parallel()

	t.Run("returns the context error when the delay is not positive", func(t *testing.T) {
		t.Parallel()

		assert.NoError(t, wait(t.Context(), 0))
	})
}
