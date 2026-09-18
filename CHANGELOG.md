# Changelog

This module follows [Go's version rules](https://go.dev/ref/mod#versions).

Until `v1.0.0` it is unstable, and the go command treats it that way: any
release may break a caller, including a patch release. Every break is called
out here.

From `v1.0.0`, a backwards-incompatible change means a new major version and,
with it, a new import path — `github.com/jkakar/typesafe-sdk-go/v2`. An
existing caller keeps building against `v1` until it chooses to move.

## Unreleased

### Added

- The first release of the Go client for the TypeSafe API.
- `Client.SystemOne` evaluates a state against named questions, and
  `Client.ListModels` lists the models an account can use.
- `NoulQuestion`, `ChoiceQuestion`, and `ScoreQuestion`, with `NoulAnswer`,
  `ChoiceAnswer`, and `ScoreAnswer` to match. `RawQuestion` and
  `UnknownAnswer` carry a type this version does not model, in each direction.
- `Answers.Noul`, `Answers.Choice`, and `Answers.Score` read an answer by name
  and type, reporting `ErrNoAnswer` or an `*AnswerTypeError`.
- `APIError` carries the status, message, request ID, `RetryAfter`, headers,
  and body, and unwraps to a sentinel naming the kind of failure.
  `DecodeError` reports a body the SDK could not read.
- `RetryPolicy` retries rate limits, overload responses, server errors, and
  transport failures with exponential backoff, honouring `retry-after`.
- `WithLogger` takes a `*slog.Logger`. Credential headers are redacted.
- `FromEnv` and `FromEnvFunc` read `TYPESAFE_API_KEY`, `TYPESAFE_BASE_URL`,
  and `TYPESAFE_DEFAULT_MODEL`.
- `typesafetest` runs a fake TypeSafe API for testing a program that calls
  this SDK.
- `Model.ReleaseDate` is the string the API sends. Its format is not settled:
  the published schema documents `YYYY-MM-DD` and the API returns an RFC 3339
  timestamp, so treat it as text.
