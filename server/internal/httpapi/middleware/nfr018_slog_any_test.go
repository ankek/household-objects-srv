package middleware

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestNoSlogAnyInProductionCode(t *testing.T) {
	root := filepath.Join("..", "..", "..")

	var scanned int
	var hits []string

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case "node_modules", ".git", "testdata", "gen", "db":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		rel = filepath.ToSlash(rel)

		src, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		if !strings.Contains(string(src), "slog.Any") {
			return nil
		}

		scanned++

		fset := token.NewFileSet()
		file, parseErr := parser.ParseFile(fset, path, src, 0)
		if parseErr != nil {
			return parseErr
		}

		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "Any" {
				return true
			}
			pkgIdent, ok := sel.X.(*ast.Ident)
			if !ok || pkgIdent.Name != "slog" {
				return true
			}
			hits = append(hits, rel+":"+strconv.Itoa(fset.Position(call.Pos()).Line))
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}

	if len(hits) > 0 {
		t.Errorf("slog.Any(...) found in production code (NFR-018): %v\n\n"+
			"slog.Any renders its argument's dynamic value via %%v/reflection with no type-level "+
			"redaction. Use a typed constructor (slog.String, slog.Int, ...) on a value already "+
			"reduced to what is safe to log, or give the type a slog.LogValuer that redacts it -- "+
			"see internal/httpapi/middleware/recover.go's panicSummary and tenantscope.go's "+
			"errSummary for the pattern this codebase already uses.", hits)
	}

	_ = scanned
}
