package typesafe

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// Defaults the client uses when a caller sets no option.
const (
	// DefaultBaseURL is the root of the TypeSafe API.
	DefaultBaseURL = "https://api.typesafe.ai"
	// DefaultModel answers a request that names no model.
	DefaultModel = "jev-latest"
	// DefaultTimeout bounds one HTTP attempt of the default HTTP client.
	DefaultTimeout = 10 * time.Second
)

// Environment variables [FromEnv] reads.
const (
	// EnvAPIKey holds the API key.
	EnvAPIKey = "TYPESAFE_API_KEY"
	// EnvBaseURL holds the API root.
	EnvBaseURL = "TYPESAFE_BASE_URL"
	// EnvModel holds the default model.
	EnvModel = "TYPESAFE_DEFAULT_MODEL"
)

// Errors reported for a client a caller cannot use.
var (
	// ErrNoAPIKey reports a client built without an API key.
	ErrNoAPIKey = errors.New("no api key")
	// ErrInvalidBaseURL reports a base URL that is not an absolute HTTP URL.
	ErrInvalidBaseURL = errors.New("invalid base url")
)

// A Client calls the TypeSafe API. Build one with [NewClient]. A Client is
// safe for concurrent use by multiple goroutines.
type Client struct {
	cfg config
}

// config holds the settings one request is sent with.
type config struct {
	apiKey     string
	baseURL    string
	model      string
	httpClient *http.Client
	header     http.Header
	retry      RetryPolicy
	logger     *slog.Logger
}

// clone copies the config so a per-call option cannot reach the client.
func (c config) clone() config {
	copied := c
	copied.header = c.header.Clone()
	return copied
}

// An Option configures a [Client] or a single call. Passing an option to
// [Client.SystemOne] or [Client.ListModels] overrides the client's setting for
// that call alone.
type Option interface {
	apply(*config) error
}

type optionFunc func(*config) error

func (f optionFunc) apply(cfg *config) error { return f(cfg) }

// WithAPIKey authenticates requests with key.
func WithAPIKey(key string) Option {
	return optionFunc(func(cfg *config) error {
		if strings.TrimSpace(key) == "" {
			return fmt.Errorf("%w: WithAPIKey was given an empty key", ErrNoAPIKey)
		}
		cfg.apiKey = key
		return nil
	})
}

// WithBaseURL sends requests to rawURL instead of [DefaultBaseURL].
func WithBaseURL(rawURL string) Option {
	return optionFunc(func(cfg *config) error {
		parsed, err := url.Parse(rawURL)
		if err != nil {
			return fmt.Errorf("%w: %w", ErrInvalidBaseURL, err)
		}
		if parsed.Scheme != "http" && parsed.Scheme != "https" {
			return fmt.Errorf("%w: %q is not an http or https url", ErrInvalidBaseURL, rawURL)
		}
		if parsed.Host == "" {
			return fmt.Errorf("%w: %q has no host", ErrInvalidBaseURL, rawURL)
		}
		cfg.baseURL = strings.TrimRight(rawURL, "/")
		return nil
	})
}

// WithModel answers requests that name no model with name.
func WithModel(name string) Option {
	return optionFunc(func(cfg *config) error {
		if strings.TrimSpace(name) == "" {
			return errors.New("WithModel was given an empty name")
		}
		cfg.model = name
		return nil
	})
}

// WithHTTPClient sends requests through client. Its Timeout bounds one
// attempt; the context passed to a call bounds the whole operation.
func WithHTTPClient(client *http.Client) Option {
	return optionFunc(func(cfg *config) error {
		if client == nil {
			return errors.New("WithHTTPClient was given a nil client")
		}
		cfg.httpClient = client
		return nil
	})
}

// WithHeader sends name with every request. The headers the SDK sets itself,
// such as Authorization, are not overridden.
func WithHeader(name, value string) Option {
	return optionFunc(func(cfg *config) error {
		if name == "" {
			return errors.New("WithHeader was given an empty name")
		}
		cfg.header.Set(name, value)
		return nil
	})
}

// WithRetryPolicy replaces the retry behavior described by
// [DefaultRetryPolicy].
func WithRetryPolicy(policy RetryPolicy) Option {
	return optionFunc(func(cfg *config) error {
		if err := policy.validate(); err != nil {
			return err
		}
		cfg.retry = policy
		return nil
	})
}

// WithLogger logs request summaries at info level and full wire detail at
// debug level. Credential headers are redacted; request and response bodies
// are not. A client logs nothing until a caller sets a logger.
func WithLogger(logger *slog.Logger) Option {
	return optionFunc(func(cfg *config) error {
		if logger == nil {
			return errors.New("WithLogger was given a nil logger")
		}
		cfg.logger = logger
		return nil
	})
}

// FromEnv reads the API key, base URL, and default model from the process
// environment. See [EnvAPIKey], [EnvBaseURL], and [EnvModel]. An unset, empty,
// or blank variable leaves its setting alone.
//
// Nothing else in this package reads the environment, so a program that
// configures a client explicitly has no ambient state to reason about.
func FromEnv() Option { return FromEnvFunc(os.LookupEnv) }

// FromEnvFunc reads the same variables as [FromEnv] through lookup, which lets
// a caller supply them from a file, a secret store, or a test.
func FromEnvFunc(lookup func(name string) (value string, ok bool)) Option {
	return optionFunc(func(cfg *config) error {
		if lookup == nil {
			return errors.New("FromEnvFunc was given a nil lookup")
		}
		settings := []struct {
			name   string
			option func(string) Option
		}{
			{EnvAPIKey, WithAPIKey},
			{EnvBaseURL, WithBaseURL},
			{EnvModel, WithModel},
		}
		for _, setting := range settings {
			value, ok := lookup(setting.name)
			if !ok || strings.TrimSpace(value) == "" {
				continue
			}
			if err := setting.option(strings.TrimSpace(value)).apply(cfg); err != nil {
				return fmt.Errorf("%s: %w", setting.name, err)
			}
		}
		return nil
	})
}

// NewClient returns a client configured by opts. It needs an API key, from
// [WithAPIKey] or [FromEnv]:
//
//	client, err := typesafe.NewClient(typesafe.FromEnv())
func NewClient(opts ...Option) (*Client, error) {
	cfg := config{
		baseURL:    DefaultBaseURL,
		model:      DefaultModel,
		httpClient: &http.Client{Timeout: DefaultTimeout},
		header:     http.Header{},
		retry:      DefaultRetryPolicy(),
		logger:     slog.New(slog.DiscardHandler),
	}
	if err := applyOptions(&cfg, opts); err != nil {
		return nil, err
	}
	if cfg.apiKey == "" {
		return nil, fmt.Errorf("%w: pass WithAPIKey or FromEnv", ErrNoAPIKey)
	}
	return &Client{cfg: cfg}, nil
}

// applyOptions applies opts in order, naming the one that failed.
func applyOptions(cfg *config, opts []Option) error {
	for i, opt := range opts {
		if opt == nil {
			return fmt.Errorf("option %d is nil", i)
		}
		if err := opt.apply(cfg); err != nil {
			return err
		}
	}
	return nil
}

// BaseURL returns the API root the client sends requests to.
func (c *Client) BaseURL() string { return c.cfg.baseURL }

// Model returns the model that answers requests naming none.
func (c *Client) Model() string { return c.cfg.model }

// callConfig returns the settings for one call, with opts applied over the
// client's own.
func (c *Client) callConfig(opts []Option) (config, error) {
	if len(opts) == 0 {
		return c.cfg, nil
	}
	cfg := c.cfg.clone()
	if err := applyOptions(&cfg, opts); err != nil {
		return config{}, err
	}
	return cfg, nil
}
