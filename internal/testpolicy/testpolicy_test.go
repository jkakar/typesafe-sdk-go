package testpolicy_test

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"testing"

	"github.com/alecthomas/assert/v2"

	"github.com/jkakar/typesafe-sdk-go/internal/leak"
	"github.com/jkakar/typesafe-sdk-go/internal/testpolicy"
)

// TestMain fails the package when a test leaves a goroutine behind. See
// docs/testing.md rule 22.
func TestMain(m *testing.M) {
	os.Exit(leak.CheckAfter(m.Run(), os.Stderr))
}

// findings parses source under name and returns the violations it holds.
func findings(t *testing.T, name, source string) []testpolicy.Finding {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, name, source, parser.SkipObjectResolution)
	assert.NoError(t, err)
	return testpolicy.CheckFile(fset, file)
}

// rules returns the rule of each finding, in order.
func rules(found []testpolicy.Finding) []string {
	named := make([]string, 0, len(found))
	for _, finding := range found {
		named = append(named, finding.Rule)
	}
	return named
}

func TestCheckFile(t *testing.T) {
	t.Parallel()

	t.Run("accepts a test that follows the rules", func(t *testing.T) {
		t.Parallel()
		source := `package x_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/alecthomas/assert/v2"
)

func TestThing(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(srv.Close)

	resp, err := srv.Client().Get(srv.URL)

	assert.NoError(t, err)
	assert.NoError(t, resp.Body.Close())
}
`

		assert.Zero(t, len(findings(t, "x_test.go", source)))
	})

	t.Run("rejects a failure the assertion library should report", func(t *testing.T) {
		t.Parallel()
		source := `package x_test

import "testing"

func TestThing(t *testing.T) {
	t.Run("does a thing", func(t *testing.T) {
		if false {
			t.Fatalf("boom")
		}
	})
}

func helper(tb testing.TB) {
	tb.Error("boom")
}
`

		found := findings(t, "x_test.go", source)

		assert.Equal(t, []string{"rule 13", "rule 13"}, rules(found))
		assert.Contains(t, found[0].Message, "t.Fatalf")
		assert.Contains(t, found[1].Message, "tb.Error")
	})

	t.Run("rejects a failure reported outside a test file", func(t *testing.T) {
		t.Parallel()
		source := `package x

import "testing"

func Helper(t *testing.T) {
	t.Fatal("boom")
}
`

		assert.Equal(t, []string{"rule 13"}, rules(findings(t, "helper.go", source)))
	})

	t.Run("allows a package function that shares a name with a testing method", func(t *testing.T) {
		t.Parallel()
		source := `package x_test

import (
	"log"
	"testing"
)

func TestThing(t *testing.T) {
	log.Fatal("boom")
}
`

		assert.Zero(t, len(findings(t, "x_test.go", source)))
	})

	t.Run("rejects environment mutation", func(t *testing.T) {
		t.Parallel()
		source := `package x_test

import (
	"os"
	"testing"
)

func TestThing(t *testing.T) {
	t.Setenv("A", "1")
	os.Setenv("B", "2")
	os.Unsetenv("C")
}
`

		assert.Equal(t, []string{"rule 12", "rule 12", "rule 12"}, rules(findings(t, "x_test.go", source)))
	})

	t.Run("rejects the shared http client and its helpers", func(t *testing.T) {
		t.Parallel()
		source := `package x_test

import (
	"net/http"
	"testing"
)

func TestThing(t *testing.T) {
	_ = http.DefaultClient
	_ = http.DefaultTransport
	_, _ = http.Get("http://example.test")
	_, _ = http.Head("http://example.test")
	_, _ = http.Post("http://example.test", "", nil)
	_, _ = http.PostForm("http://example.test", nil)
}
`

		assert.Equal(t, 6, len(findings(t, "x_test.go", source)))
		assert.Equal(t, "rule 8", findings(t, "x_test.go", source)[0].Rule)
	})

	t.Run("allows the shared http client outside a test file", func(t *testing.T) {
		t.Parallel()
		source := `package x

import "net/http"

func Client() *http.Client { return http.DefaultClient }
`

		assert.Zero(t, len(findings(t, "client.go", source)))
	})

	t.Run("follows an import alias", func(t *testing.T) {
		t.Parallel()
		source := `package x_test

import (
	nethttp "net/http"
	"testing"
)

func TestThing(t *testing.T) {
	_ = nethttp.DefaultClient
}
`

		assert.Equal(t, []string{"rule 8"}, rules(findings(t, "x_test.go", source)))
	})

	t.Run("ignores an unrelated package with a matching name", func(t *testing.T) {
		t.Parallel()
		source := `package x_test

import (
	"testing"

	"example.test/os"
)

func TestThing(t *testing.T) {
	os.Setenv("A", "1")
}
`

		assert.Zero(t, len(findings(t, "x_test.go", source)))
	})

	t.Run("requires a handwritten TestMain to check for leaks", func(t *testing.T) {
		t.Parallel()
		source := `package x_test

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	os.Exit(m.Run())
}
`

		assert.Equal(t, []string{"rule 22"}, rules(findings(t, "main_test.go", source)))
	})

	t.Run("accepts a TestMain that checks for leaks", func(t *testing.T) {
		t.Parallel()
		sources := map[string]string{
			"qualified": `package x_test

import (
	"os"
	"testing"

	"github.com/jkakar/typesafe-sdk-go/internal/leak"
)

func TestMain(m *testing.M) {
	os.Exit(leak.CheckAfter(m.Run(), os.Stderr))
}
`,
			"unqualified": `package leak

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	os.Exit(CheckAfter(m.Run(), os.Stderr))
}
`,
		}
		for name, source := range sources {
			t.Run(name, func(t *testing.T) {
				t.Parallel()

				assert.Zero(t, len(findings(t, "main_test.go", source)))
			})
		}
	})

	t.Run("ignores a method named TestMain", func(t *testing.T) {
		t.Parallel()
		source := `package x_test

import "testing"

type runner struct{}

func (r runner) TestMain(m *testing.M) {}
`

		assert.Zero(t, len(findings(t, "x_test.go", source)))
	})

	t.Run("ignores a parameter that is not a testing type", func(t *testing.T) {
		t.Parallel()
		source := `package x_test

import "testing"

func helper(counts map[string]int, b *bytes.Buffer, c chan int) {}

func TestThing(t *testing.T) { helper(nil, nil, nil) }
`

		assert.Zero(t, len(findings(t, "x_test.go", source)))
	})

	t.Run("accepts a TestMain that calls the check through a value", func(t *testing.T) {
		t.Parallel()
		source := `package x_test

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	check := func(code int) int { return code }
	os.Exit(func() int { return check(m.Run()) }())
}
`

		assert.Equal(t, []string{"rule 22"}, rules(findings(t, "main_test.go", source)))
	})

	t.Run("ignores a call on something that is not an identifier", func(t *testing.T) {
		t.Parallel()
		source := `package x_test

import "testing"

type suite struct{ t *testing.T }

func TestThing(t *testing.T) {
	s := suite{t}
	_ = s.t
}
`

		assert.Zero(t, len(findings(t, "x_test.go", source)))
	})
}

func TestCheckDir(t *testing.T) {
	t.Parallel()

	t.Run("reports violations under the directory", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		write(t, filepath.Join(root, "ok_test.go"), "package x\n")
		write(t, filepath.Join(root, "bad_test.go"), `package x

import "testing"

func TestThing(t *testing.T) { t.Fatal("boom") }
`)

		found, err := testpolicy.CheckDir(root)

		assert.NoError(t, err)
		assert.Equal(t, []string{"rule 13"}, rules(found))
		assert.HasSuffix(t, found[0].Position.Filename, "bad_test.go")
	})

	t.Run("skips directories the go tool ignores", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		for _, dir := range []string{".hidden", "testdata", "vendor"} {
			assert.NoError(t, os.Mkdir(filepath.Join(root, dir), 0o755))
			write(t, filepath.Join(root, dir, "bad_test.go"), `package x

import "testing"

func TestThing(t *testing.T) { t.Fatal("boom") }
`)
		}

		found, err := testpolicy.CheckDir(root)

		assert.NoError(t, err)
		assert.Zero(t, len(found))
	})

	t.Run("ignores files that are not go source", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		write(t, filepath.Join(root, "notes.md"), "t.Fatal is fine in prose\n")

		found, err := testpolicy.CheckDir(root)

		assert.NoError(t, err)
		assert.Zero(t, len(found))
	})

	t.Run("reports a file it cannot parse", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		write(t, filepath.Join(root, "broken.go"), "package")

		_, err := testpolicy.CheckDir(root)

		assert.Error(t, err)
	})

	t.Run("reports a directory it cannot read", func(t *testing.T) {
		t.Parallel()

		_, err := testpolicy.CheckDir(filepath.Join(t.TempDir(), "missing"))

		assert.Error(t, err)
	})
}

func TestFinding_String(t *testing.T) {
	t.Parallel()

	t.Run("names the position, the advice and the rule", func(t *testing.T) {
		t.Parallel()
		finding := testpolicy.Finding{
			Position: token.Position{Filename: "x_test.go", Line: 7, Column: 2},
			Rule:     "rule 13",
			Message:  "t.Fatal does not attribute the failure",
		}

		assert.Equal(t, "x_test.go:7:2: t.Fatal does not attribute the failure (docs/testing.md rule 13)",
			finding.String())
	})
}

// write creates a file with the given contents.
func write(t *testing.T, path, contents string) {
	t.Helper()
	assert.NoError(t, os.WriteFile(path, []byte(contents), 0o600))
}
