# Changelog

This project follows [semantic versioning](https://semver.org). Until 1.0 the
exported API may change in a minor release; a change that breaks a caller is
called out here.

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
