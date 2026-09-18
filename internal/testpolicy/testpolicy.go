// Package testpolicy checks Go source against the mechanical rules in
// docs/testing.md.
//
// The checks read Go syntax, never lines of text, so the same characters
// inside a comment or a string literal are not a violation. A rule belongs
// here only when it can be decided from syntax alone; everything else belongs
// to review.
package testpolicy

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// A Finding is one rule violation, with the position that produced it.
type Finding struct {
	// Position is where the violation appears.
	Position token.Position
	// Rule names the docs/testing.md rule the code breaks.
	Rule string
	// Message says what to do instead.
	Message string
}

func (f Finding) String() string {
	return fmt.Sprintf("%s: %s (docs/testing.md %s)", f.Position, f.Message, f.Rule)
}

// Forbidden methods on a *testing.T, *testing.B, or testing.TB.
var (
	// haltingMethods report or halt a test without assert's attribution.
	haltingMethods = []string{"Error", "Errorf", "Fatal", "Fatalf", "Fail", "FailNow"}
	// environmentMethods mutate the process environment.
	environmentMethods = []string{"Setenv"}
)

// Forbidden package-level identifiers in test files, by import path.
var forbidden = map[string]map[string]string{
	"net/http": {
		"DefaultClient":    "use the server's own client, which owns a private connection pool",
		"DefaultTransport": "give the client a private transport",
		"Get":              "use the server's own client",
		"Head":             "use the server's own client",
		"Post":             "use the server's own client",
		"PostForm":         "use the server's own client",
	},
	"os": {
		"Setenv":   "build typed configuration in the test instead",
		"Unsetenv": "build typed configuration in the test instead",
	},
}

// CheckDir returns the violations in every Go file under root, skipping
// directories the Go tool ignores.
func CheckDir(root string) ([]Finding, error) {
	fset := token.NewFileSet()
	var findings []Finding
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return skipDir(entry.Name())
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		findings = append(findings, CheckFile(fset, file)...)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return findings, nil
}

// skipDir tells the walk to leave a directory the Go tool ignores.
func skipDir(name string) error {
	if strings.HasPrefix(name, ".") && name != "." || name == "testdata" || name == "vendor" {
		return fs.SkipDir
	}
	return nil
}

// CheckFile returns the violations in one parsed file.
func CheckFile(fset *token.FileSet, file *ast.File) []Finding {
	check := &checker{fset: fset, file: file, testFile: strings.HasSuffix(fset.Position(file.Pos()).Filename, "_test.go")}
	check.imports = importNames(file)
	ast.Inspect(file, check.visit)
	return check.findings
}

// a checker accumulates the findings in one file.
type checker struct {
	fset     *token.FileSet
	file     *ast.File
	testFile bool
	// imports maps each imported package's local name to its path.
	imports map[string]string
	// testing names the testing.TB parameters in scope, innermost last.
	testing  []string
	findings []Finding
}

func (c *checker) visit(node ast.Node) bool {
	switch node := node.(type) {
	case *ast.FuncDecl:
		c.enter(node.Type)
		c.checkTestMain(node)
	case *ast.FuncLit:
		c.enter(node.Type)
	case *ast.SelectorExpr:
		c.checkSelector(node)
	}
	return true
}

// enter records the testing.TB parameters a function signature introduces.
// Names accumulate for the whole file, which is enough: a name bound to a
// testing.TB in one function is never something else in another.
func (c *checker) enter(signature *ast.FuncType) {
	for _, param := range signature.Params.List {
		if !isTestingTB(param.Type) {
			continue
		}
		for _, name := range param.Names {
			if !slices.Contains(c.testing, name.Name) {
				c.testing = append(c.testing, name.Name)
			}
		}
	}
}

// checkSelector reports a forbidden method on a testing.TB and a forbidden
// identifier in a package this file imports.
func (c *checker) checkSelector(selector *ast.SelectorExpr) {
	receiver, ok := selector.X.(*ast.Ident)
	if !ok {
		return
	}
	if slices.Contains(c.testing, receiver.Name) {
		c.checkTestingMethod(selector)
		return
	}
	if !c.testFile {
		return
	}
	names, ok := forbidden[c.imports[receiver.Name]]
	if !ok {
		return
	}
	if advice, ok := names[selector.Sel.Name]; ok {
		rule := "rule 8"
		if c.imports[receiver.Name] == "os" {
			rule = "rule 12"
		}
		c.report(selector.Pos(), rule, fmt.Sprintf("%s.%s is not allowed in a test: %s",
			receiver.Name, selector.Sel.Name, advice))
	}
}

// checkTestingMethod reports a call that halts or reports without assert, and
// one that mutates the process environment.
func (c *checker) checkTestingMethod(selector *ast.SelectorExpr) {
	name := selector.Sel.Name
	switch {
	case slices.Contains(haltingMethods, name):
		c.report(selector.Pos(), "rule 13", fmt.Sprintf(
			"%s.%s does not attribute the failure: use github.com/alecthomas/assert/v2",
			selector.X.(*ast.Ident).Name, name))
	case slices.Contains(environmentMethods, name):
		c.report(selector.Pos(), "rule 12", fmt.Sprintf(
			"%s.%s mutates the process environment: build typed configuration in the test instead",
			selector.X.(*ast.Ident).Name, name))
	}
}

// checkTestMain requires a handwritten TestMain to run the leak check.
func (c *checker) checkTestMain(decl *ast.FuncDecl) {
	if decl.Name.Name != "TestMain" || decl.Recv != nil {
		return
	}
	found := false
	ast.Inspect(decl.Body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if ok && calls(call, "CheckAfter") {
			found = true
		}
		return !found
	})
	if !found {
		c.report(decl.Pos(), "rule 22",
			"TestMain must end the package with leak.CheckAfter, which fails a run that leaked a goroutine")
	}
}

// calls reports whether a call names the given function, whether the package
// owns it or imports it.
func calls(call *ast.CallExpr, name string) bool {
	switch fn := call.Fun.(type) {
	case *ast.Ident:
		return fn.Name == name
	case *ast.SelectorExpr:
		return fn.Sel.Name == name
	default:
		return false
	}
}

func (c *checker) report(pos token.Pos, rule, message string) {
	c.findings = append(c.findings, Finding{Position: c.fset.Position(pos), Rule: rule, Message: message})
}

// importNames maps each import's local name to its path.
func importNames(file *ast.File) map[string]string {
	names := make(map[string]string, len(file.Imports))
	for _, spec := range file.Imports {
		path, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			continue
		}
		name := path[strings.LastIndex(path, "/")+1:]
		if spec.Name != nil {
			name = spec.Name.Name
		}
		names[name] = path
	}
	return names
}

// isTestingTB reports whether a parameter type is *testing.T, *testing.B, or
// testing.TB.
func isTestingTB(expr ast.Expr) bool {
	if star, ok := expr.(*ast.StarExpr); ok {
		expr = star.X
	}
	selector, ok := expr.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := selector.X.(*ast.Ident)
	if !ok || pkg.Name != "testing" {
		return false
	}
	return slices.Contains([]string{"T", "B", "TB"}, selector.Sel.Name)
}
