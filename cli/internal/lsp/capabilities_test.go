package lsp

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/sourcegraph/jsonrpc2"
)

// gafferMethodConstants extracts every string constant in the package whose
// value starts with "gaffer/" by parsing the source. Reading them out of the
// AST rather than restating them keeps this test honest: a new gaffer/* method
// constant is picked up automatically, so it can't be added to the switch and
// silently left out of the advertised set.
func gafferMethodConstants(t *testing.T) map[string]string {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("reading package directory: %v", err)
	}

	fset := token.NewFileSet()
	// name -> value, e.g. "MethodDiffVersions" -> "gaffer/diffVersions".
	found := map[string]string{}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatalf("parsing %s: %v", name, err)
		}
		for _, decl := range file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.CONST {
				continue
			}
			for _, spec := range gen.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for i, name := range vs.Names {
					if i >= len(vs.Values) {
						continue
					}
					lit, ok := vs.Values[i].(*ast.BasicLit)
					if !ok || lit.Kind != token.STRING {
						continue
					}
					val, err := strconv.Unquote(lit.Value)
					if err != nil || !strings.HasPrefix(val, "gaffer/") {
						continue
					}
					found[name.Name] = val
				}
			}
		}
	}
	if len(found) == 0 {
		t.Fatal("no gaffer/* method constants found - the AST scan is broken, not the code")
	}
	return found
}

// TestGafferMethodsMatchProtocol pins both drift directions on the advertised
// method set, since each fails in its own bad way:
//
//   - advertised but not dispatched: the client is told a surface works, offers
//     it, and the call comes back MethodNotFound after the user clicks.
//   - dispatched but not advertised: the method works, but every capability-
//     gated client hides the surface, so a shipped feature is invisible.
func TestGafferMethodsMatchProtocol(t *testing.T) {
	advertised := map[string]struct{}{}
	for _, m := range gafferMethods {
		if _, dup := advertised[m]; dup {
			t.Errorf("method %q is listed twice in gafferMethods", m)
		}
		advertised[m] = struct{}{}
	}

	t.Run("every advertised method is dispatched", func(t *testing.T) {
		s := NewServer(ServerOptions{})
		for _, m := range gafferMethods {
			_, err := s.handle(context.Background(), nil, &jsonrpc2.Request{Method: m})
			var je *jsonrpc2.Error
			if errors.As(err, &je) && je.Code == jsonrpc2.CodeMethodNotFound {
				t.Errorf("advertised method %q is not dispatched by handle's switch", m)
			}
		}
	})

	t.Run("every gaffer method constant is advertised", func(t *testing.T) {
		for name, value := range gafferMethodConstants(t) {
			if _, ok := advertised[value]; !ok {
				t.Errorf("%s (%q) is not in gafferMethods, so capability-gated clients will hide it", name, value)
			}
		}
	})
}

// TestInitializeAdvertisesGafferMethods pins the wire shape the extension reads.
// The nesting (experimental.gaffer.methods) is a contract with every client, so
// a rename here is a breaking change for them, not an internal refactor.
func TestInitializeAdvertisesGafferMethods(t *testing.T) {
	s := NewServer(ServerOptions{})
	res, err := s.handle(context.Background(), nil, &jsonrpc2.Request{Method: MethodInitialize})
	if err != nil {
		t.Fatalf("initialize: %v", err)
	}
	init, ok := res.(InitializeResult)
	if !ok {
		t.Fatalf("initialize returned %T, want InitializeResult", res)
	}
	if init.Capabilities.Experimental == nil {
		t.Fatal("initialize did not advertise experimental capabilities")
	}
	got := init.Capabilities.Experimental.Gaffer.Methods
	if len(got) != len(gafferMethods) {
		t.Fatalf("advertised %d methods, want %d", len(got), len(gafferMethods))
	}
	for i, m := range gafferMethods {
		if got[i] != m {
			t.Errorf("advertised method %d = %q, want %q", i, got[i], m)
		}
	}
}

// TestInitializeAdvertisesMethodsWithoutStatusLensOptIn guards the deliberate
// asymmetry with HoverProvider: hover is withheld from a client that didn't opt
// into the status surface, but the served method set is a fact about the build
// and goes to everyone. A client can't ask "what do you serve?" any other way.
func TestInitializeAdvertisesMethodsWithoutStatusLensOptIn(t *testing.T) {
	s := NewServer(ServerOptions{})
	res, err := s.handle(context.Background(), nil, &jsonrpc2.Request{Method: MethodInitialize})
	if err != nil {
		t.Fatalf("initialize: %v", err)
	}
	init, ok := res.(InitializeResult)
	if !ok {
		t.Fatalf("initialize returned %T, want InitializeResult", res)
	}
	if init.Capabilities.HoverProvider != nil {
		t.Error("hover should be withheld without the statusLens opt-in")
	}
	if init.Capabilities.Experimental == nil || len(init.Capabilities.Experimental.Gaffer.Methods) == 0 {
		t.Error("gaffer methods should be advertised regardless of the statusLens opt-in")
	}
}
