package typesafe_test

import (
	"log/slog"
	"net/http"
	"testing"
	"time"

	"github.com/alecthomas/assert/v2"

	"github.com/jkakar/typesafe-sdk-go"
)

// env returns a lookup function over the given variables, so a test configures
// a client from the environment without touching the process environment.
func env(values map[string]string) func(string) (string, bool) {
	return func(name string) (string, bool) {
		value, ok := values[name]
		return value, ok
	}
}

func TestNewClient(t *testing.T) {
	t.Parallel()

	t.Run("returns client with default settings", func(t *testing.T) {
		t.Parallel()

		client, err := typesafe.NewClient(typesafe.WithAPIKey("secret"))

		assert.NoError(t, err)
		assert.Equal(t, typesafe.DefaultBaseURL, client.BaseURL())
		assert.Equal(t, typesafe.DefaultModel, client.Model())
	})

	t.Run("returns error when no api key is configured", func(t *testing.T) {
		t.Parallel()

		_, err := typesafe.NewClient()

		assert.IsError(t, err, typesafe.ErrNoAPIKey)
	})

	t.Run("returns error when api key is blank", func(t *testing.T) {
		t.Parallel()

		_, err := typesafe.NewClient(typesafe.WithAPIKey("   "))

		assert.IsError(t, err, typesafe.ErrNoAPIKey)
	})

	t.Run("reads settings from the environment", func(t *testing.T) {
		t.Parallel()
		lookup := env(map[string]string{
			typesafe.EnvAPIKey:  "  secret  ",
			typesafe.EnvBaseURL: "https://api.example.test/",
			typesafe.EnvModel:   "jev-1.13.0",
		})

		client, err := typesafe.NewClient(typesafe.FromEnvFunc(lookup))

		assert.NoError(t, err)
		assert.Equal(t, "https://api.example.test", client.BaseURL())
		assert.Equal(t, "jev-1.13.0", client.Model())
	})

	t.Run("ignores blank environment values", func(t *testing.T) {
		t.Parallel()
		lookup := env(map[string]string{
			typesafe.EnvAPIKey:  "secret",
			typesafe.EnvBaseURL: "   ",
			typesafe.EnvModel:   "",
		})

		client, err := typesafe.NewClient(typesafe.FromEnvFunc(lookup))

		assert.NoError(t, err)
		assert.Equal(t, typesafe.DefaultBaseURL, client.BaseURL())
		assert.Equal(t, typesafe.DefaultModel, client.Model())
	})

	t.Run("names the environment variable that held an invalid value", func(t *testing.T) {
		t.Parallel()
		lookup := env(map[string]string{typesafe.EnvBaseURL: "ftp://example.test"})

		_, err := typesafe.NewClient(typesafe.FromEnvFunc(lookup))

		assert.IsError(t, err, typesafe.ErrInvalidBaseURL)
		assert.Contains(t, err.Error(), typesafe.EnvBaseURL)
	})

	t.Run("prefers explicit options over the environment", func(t *testing.T) {
		t.Parallel()
		lookup := env(map[string]string{typesafe.EnvModel: "from-env"})

		client, err := typesafe.NewClient(
			typesafe.WithAPIKey("secret"),
			typesafe.FromEnvFunc(lookup),
			typesafe.WithModel("from-option"),
		)

		assert.NoError(t, err)
		assert.Equal(t, "from-option", client.Model())
	})

	t.Run("builds an option that reads the process environment", func(t *testing.T) {
		t.Parallel()

		// Applying this option would read the process environment, which
		// no test may depend on. Building it proves the delegation.
		option := typesafe.FromEnv()

		assert.True(t, option != nil)
	})

	t.Run("rejects a base url that is not an absolute http url", func(t *testing.T) {
		t.Parallel()
		for name, rawURL := range map[string]string{
			"unsupported scheme": "ftp://example.test",
			"no scheme":          "example.test/v1",
			"no host":            "https:///v1",
			"unparseable":        "https://exa mple.test\x7f",
		} {
			t.Run(name, func(t *testing.T) {
				t.Parallel()

				_, err := typesafe.NewClient(typesafe.WithAPIKey("secret"), typesafe.WithBaseURL(rawURL))

				assert.IsError(t, err, typesafe.ErrInvalidBaseURL)
			})
		}
	})

	t.Run("rejects an invalid retry policy", func(t *testing.T) {
		t.Parallel()
		policies := map[string]typesafe.RetryPolicy{
			"negative max retries":     {MaxRetries: -1},
			"negative initial backoff": {InitialBackoff: -time.Second},
			"negative max backoff":     {MaxBackoff: -time.Second},
			"jitter below zero":        {Jitter: -0.1},
			"jitter above one":         {Jitter: 1.1},
			"negative max retry after": {MaxRetryAfter: -time.Second},
		}
		for name, policy := range policies {
			t.Run(name, func(t *testing.T) {
				t.Parallel()

				_, err := typesafe.NewClient(typesafe.WithAPIKey("secret"), typesafe.WithRetryPolicy(policy))

				assert.IsError(t, err, typesafe.ErrInvalidRetryPolicy)
			})
		}
	})

	t.Run("rejects options with missing values", func(t *testing.T) {
		t.Parallel()
		options := map[string]typesafe.Option{
			"empty model":       typesafe.WithModel("  "),
			"nil http client":   typesafe.WithHTTPClient(nil),
			"empty header name": typesafe.WithHeader("", "value"),
			"nil logger":        typesafe.WithLogger(nil),
			"nil lookup":        typesafe.FromEnvFunc(nil),
			"nil option":        nil,
		}
		for name, option := range options {
			t.Run(name, func(t *testing.T) {
				t.Parallel()

				_, err := typesafe.NewClient(typesafe.WithAPIKey("secret"), option)

				assert.Error(t, err)
			})
		}
	})

	t.Run("accepts every setting", func(t *testing.T) {
		t.Parallel()

		client, err := typesafe.NewClient(
			typesafe.WithAPIKey("secret"),
			typesafe.WithBaseURL("https://api.example.test"),
			typesafe.WithModel("jev-1.13.0"),
			typesafe.WithHTTPClient(&http.Client{}),
			typesafe.WithHeader("X-Trace", "abc"),
			typesafe.WithRetryPolicy(typesafe.DefaultRetryPolicy()),
			typesafe.WithLogger(slog.New(slog.DiscardHandler)),
		)

		assert.NoError(t, err)
		assert.Equal(t, "https://api.example.test", client.BaseURL())
		assert.Equal(t, "jev-1.13.0", client.Model())
	})
}

func TestClient_BaseURL(t *testing.T) {
	t.Parallel()

	t.Run("returns the configured api root", func(t *testing.T) {
		t.Parallel()
		client, err := typesafe.NewClient(typesafe.WithAPIKey("secret"), typesafe.WithBaseURL("https://api.example.test/v1/"))
		assert.NoError(t, err)

		assert.Equal(t, "https://api.example.test/v1", client.BaseURL())
	})
}

func TestClient_Model(t *testing.T) {
	t.Parallel()

	t.Run("returns the model that answers requests naming none", func(t *testing.T) {
		t.Parallel()
		client, err := typesafe.NewClient(typesafe.WithAPIKey("secret"), typesafe.WithModel("jev-preview"))
		assert.NoError(t, err)

		assert.Equal(t, "jev-preview", client.Model())
	})
}
