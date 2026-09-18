package typesafe_test

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/alecthomas/assert/v2"

	"github.com/jkakar/typesafe-sdk-go"
	"github.com/jkakar/typesafe-sdk-go/typesafetest"
)

func TestClient_ListModels(t *testing.T) {
	t.Parallel()

	t.Run("returns the models the account can use", func(t *testing.T) {
		t.Parallel()
		srv := typesafetest.NewServer()
		t.Cleanup(srv.Close)
		srv.HandleModels(func(context.Context) ([]typesafe.Model, error) {
			return []typesafe.Model{
				{Name: "jev-latest", Description: "The most recent stable release.", ReleaseDate: "2026-09-15"},
			}, nil
		})
		client := srv.Client()

		models, err := client.ListModels(t.Context())

		assert.NoError(t, err)
		assert.Equal(t, []typesafe.Model{
			{Name: "jev-latest", Description: "The most recent stable release.", ReleaseDate: "2026-09-15"},
		}, models)
		assert.Equal(t, "/v1/models", srv.Recorded()[0].Path)
		assert.Equal(t, http.MethodGet, srv.Recorded()[0].Method)
	})

	t.Run("applies a call option to one call only", func(t *testing.T) {
		t.Parallel()
		srv := typesafetest.NewServer()
		t.Cleanup(srv.Close)
		client := srv.Client()

		_, err := client.ListModels(t.Context(), typesafe.WithHeader("X-Trace", "once"))
		assert.NoError(t, err)
		_, err = client.ListModels(t.Context())
		assert.NoError(t, err)

		recorded := srv.Recorded()
		assert.Equal(t, "once", recorded[0].Header.Get("X-Trace"))
		assert.Equal(t, "", recorded[1].Header.Get("X-Trace"))
	})

	t.Run("returns the error from an invalid call option", func(t *testing.T) {
		t.Parallel()
		srv := typesafetest.NewServer()
		t.Cleanup(srv.Close)
		client := srv.Client()

		_, err := client.ListModels(t.Context(), typesafe.WithModel(""))

		assert.Error(t, err)
		assert.Zero(t, len(srv.Recorded()))
	})

	t.Run("returns an api error for an unsuccessful response", func(t *testing.T) {
		t.Parallel()
		srv := typesafetest.NewServer()
		t.Cleanup(srv.Close)
		srv.HandleModels(func(context.Context) ([]typesafe.Model, error) {
			return nil, &typesafetest.Failure{StatusCode: http.StatusForbidden}
		})
		client := srv.Client()

		_, err := client.ListModels(t.Context())

		assert.IsError(t, err, typesafe.ErrPermissionDenied)
	})

	t.Run("returns a decode error for a body it cannot read", func(t *testing.T) {
		t.Parallel()
		client := newRawServer(t, func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(t, w, http.StatusOK, `{"models":`)
		})

		_, err := client.ListModels(t.Context())

		var decodeErr *typesafe.DecodeError
		assert.True(t, errors.As(err, &decodeErr))
		assert.HasSuffix(t, decodeErr.URL, "/v1/models")
	})
}
