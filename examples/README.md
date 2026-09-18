# Examples

Runnable programs that call the real API. Each one needs a key:

```sh
export TYPESAFE_API_KEY=...
```

| Command | What it shows |
|---|---|
| [`triage`](triage) | Four questions in one call, then a routing decision the program owns |
| [`models`](models) | Listing the models the account can use |

```sh
go run ./examples/triage "I was charged twice. Please fix this ASAP."
echo "The checkout page is down." | go run ./examples/triage
go run ./examples/models
```

For examples that run without a key or a network, see the [package
documentation](https://pkg.go.dev/github.com/jkakar/typesafe-sdk-go#pkg-examples),
which is generated from `example_test.go` and runs as part of the test suite.
