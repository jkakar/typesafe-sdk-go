package typesafe

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"net/http"
	"strconv"
	"time"
)

// ErrInvalidRetryPolicy reports a [RetryPolicy] the SDK cannot use.
var ErrInvalidRetryPolicy = errors.New("invalid retry policy")

// A RetryPolicy decides which failures the client retries and how long it
// waits between attempts. Start from [DefaultRetryPolicy] and change the
// fields a caller cares about.
type RetryPolicy struct {
	// MaxRetries is how many times a failed request is retried. Zero
	// disables retries.
	MaxRetries int

	// InitialBackoff is the wait before the first retry. Each later wait
	// doubles it, up to MaxBackoff.
	InitialBackoff time.Duration

	// MaxBackoff caps the wait between attempts.
	MaxBackoff time.Duration

	// Jitter is the fraction of each wait randomly subtracted from it,
	// from zero to one. It spreads the retries of concurrent callers.
	Jitter float64

	// RespectRetryAfter honors the retry-after and retry-after-ms response
	// headers in place of the computed backoff.
	RespectRetryAfter bool

	// MaxRetryAfter caps the delay the client accepts from a server. A
	// longer delay falls back to the computed backoff.
	MaxRetryAfter time.Duration

	// RetryStatus reports whether an unsuccessful HTTP status is retried. A
	// nil value retries 408, 429, and every 5xx status.
	RetryStatus func(statusCode int) bool

	// RetryError reports whether a transport failure is retried. A nil
	// value retries every failure except a canceled or expired context.
	RetryError func(err error) bool
}

// DefaultRetryPolicy returns the policy a client uses when a caller sets none.
// It retries twice, waiting 500ms before the first retry and doubling up to
// five seconds, and honors a server delay of up to a minute.
func DefaultRetryPolicy() RetryPolicy {
	return RetryPolicy{
		MaxRetries:        2,
		InitialBackoff:    500 * time.Millisecond,
		MaxBackoff:        5 * time.Second,
		Jitter:            0.25,
		RespectRetryAfter: true,
		MaxRetryAfter:     time.Minute,
	}
}

// validate reports a policy with values the client cannot use.
func (p RetryPolicy) validate() error {
	switch {
	case p.MaxRetries < 0:
		return fmt.Errorf("%w: MaxRetries must not be negative", ErrInvalidRetryPolicy)
	case p.InitialBackoff < 0:
		return fmt.Errorf("%w: InitialBackoff must not be negative", ErrInvalidRetryPolicy)
	case p.MaxBackoff < 0:
		return fmt.Errorf("%w: MaxBackoff must not be negative", ErrInvalidRetryPolicy)
	case p.Jitter < 0 || p.Jitter > 1:
		return fmt.Errorf("%w: Jitter must be between zero and one", ErrInvalidRetryPolicy)
	case p.MaxRetryAfter < 0:
		return fmt.Errorf("%w: MaxRetryAfter must not be negative", ErrInvalidRetryPolicy)
	}
	return nil
}

// retriesStatus reports whether the policy retries an HTTP status code.
func (p RetryPolicy) retriesStatus(status int) bool {
	if p.RetryStatus != nil {
		return p.RetryStatus(status)
	}
	return status == http.StatusRequestTimeout ||
		status == http.StatusTooManyRequests ||
		status >= http.StatusInternalServerError
}

// retriesError reports whether the policy retries a transport failure.
func (p RetryPolicy) retriesError(err error) bool {
	if p.RetryError != nil {
		return p.RetryError(err)
	}
	return !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded)
}

// backoff returns the wait before retry attempt, counted from one. A server
// delay within MaxRetryAfter replaces the computed wait.
func (p RetryPolicy) backoff(attempt int, serverDelay time.Duration) time.Duration {
	if p.RespectRetryAfter && serverDelay > 0 && serverDelay <= p.MaxRetryAfter {
		return serverDelay
	}
	if p.InitialBackoff <= 0 || p.MaxBackoff <= 0 {
		return 0
	}
	// Doubling in float avoids overflowing the shift on a long retry run.
	delay := float64(p.InitialBackoff) * math.Pow(2, float64(attempt-1))
	delay = min(delay, float64(p.MaxBackoff))
	return time.Duration(delay * (1 - rand.Float64()*p.Jitter))
}

// headerRetryAfter and headerRetryAfterMS carry a server's requested delay.
const (
	headerRetryAfter   = "Retry-After"
	headerRetryAfterMS = "Retry-After-Ms"
)

// retryAfter reads the delay a server asked for, preferring the millisecond
// header. It returns zero when neither header carries a usable delay.
func retryAfter(header http.Header, now time.Time) time.Duration {
	if raw := header.Get(headerRetryAfterMS); raw != "" {
		if ms, err := strconv.ParseFloat(raw, 64); err == nil && ms >= 0 {
			return time.Duration(ms * float64(time.Millisecond))
		}
	}
	raw := header.Get(headerRetryAfter)
	if raw == "" {
		return 0
	}
	if seconds, err := strconv.ParseFloat(raw, 64); err == nil {
		if seconds < 0 {
			return 0
		}
		return time.Duration(seconds * float64(time.Second))
	}
	deadline, err := http.ParseTime(raw)
	if err != nil {
		return 0
	}
	return max(0, deadline.Sub(now))
}

// wait sleeps for delay, or returns the context's error if it ends first.
func wait(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
