package storage

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

const internalPrefix = "/internal/storage/internal/"

func TestGeneratedSQLIsUnreachableOutsideStorage(t *testing.T) {
	for _, dir := range []string{"internal/gen", "internal/db"} {
		if _, err := os.Stat(dir); err != nil {
			t.Fatalf("%s is not where the tenancy boundary expects it: %v; the SQL and the pools must live under internal/storage/internal so that Go's internal-package rule keeps them out of the handler layer", dir, err)
		}
	}

	root := filepath.Join("..", "..")
	var scanned, insideImports int

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == "node_modules" || d.Name() == ".git" || d.Name() == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}

		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		rel = filepath.ToSlash(rel)
		inside := strings.HasPrefix(rel, "internal/storage/")
		scanned++

		fset := token.NewFileSet()
		file, parseErr := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if parseErr != nil {
			return parseErr
		}
		for _, spec := range file.Imports {
			imported, unquoteErr := strconv.Unquote(spec.Path.Value)
			if unquoteErr != nil {
				return unquoteErr
			}
			if !strings.Contains(imported, internalPrefix) {
				continue
			}
			if inside {
				insideImports++
				continue
			}
			t.Errorf("%s imports %s from outside internal/storage; the generated queries and the connection pools are reachable only through this package's group-bound repositories (NFR-011, P-3)", rel, imported)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}

	if scanned == 0 {
		t.Fatal("scanned zero .go files; the walk is broken and this test asserts nothing")
	}
	if insideImports == 0 {
		t.Fatalf("no file under internal/storage imports %s*; either the packages moved or the prefix is stale, and either way this test would no longer notice a leak", internalPrefix)
	}
}

var databaseQualifiers = map[string]bool{"sql": true, "gen": true, "db": true}

var exportedAliasAllowlist = map[string]string{
	"Item":                "gen.Item",
	"Warranty":            "gen.ItemWarranty",
	"Sale":                "gen.ItemSale",
	"Purchase":            "gen.ItemPurchase",
	"Identification":      "gen.ItemIdentification",
	"CustomFieldDef":      "gen.CustomFieldDef",
	"ItemCustomField":     "gen.ItemCustomField",
	"StockAdjustment":     "gen.StockAdjustment",
	"Label":               "gen.Label",
	"Location":            "gen.Location",
	"Attachment":          "gen.Attachment",
	"ImportSession":       "gen.ImportSession",
	"ItemLabelAssignment": "gen.ItemLabel",
	"MutationLedgerEntry": "gen.Mutation",
}

func TestExportedSurfaceExposesNoDatabaseHandle(t *testing.T) {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi fs.FileInfo) bool { //nolint:staticcheck // SA1019: see above
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatalf("parse package: %v", err)
	}
	pkg, ok := pkgs["storage"]
	if !ok {
		t.Fatalf("package storage not found in %v", pkgs)
	}

	checked := 0
	report := func(what string, pos token.Pos, expr ast.Expr) {
		if leak := findDatabaseQualifier(expr); leak != "" {
			t.Errorf("%s: %s exposes %s; the exported surface of this package is scopes, repositories and rows -- anything that can issue a query must stay behind Storage.ForGroup (NFR-011, P-3)",
				fset.Position(pos), what, leak)
		}
	}

	for name, file := range pkg.Files {
		base := filepath.Base(name)
		for _, decl := range file.Decls {
			switch d := decl.(type) {
			case *ast.FuncDecl:
				if !d.Name.IsExported() || !receiverIsExported(d) {
					continue
				}
				checked++
				for _, result := range fieldList(d.Type.Results) {
					report(base+": func "+d.Name.Name, d.Pos(), result.Type)
				}

			case *ast.GenDecl:
				for _, spec := range d.Specs {
					ts, isType := spec.(*ast.TypeSpec)
					if !isType || !ts.Name.IsExported() {
						continue
					}
					checked++

					if ts.Assign != token.NoPos {
						target := exprString(ts.Type)
						if want, listed := exportedAliasAllowlist[ts.Name.Name]; !listed || want != target {
							t.Errorf("%s: exported alias %s = %s is not in exportedAliasAllowlist; an alias is the one way an internal type becomes nameable above this layer, so each one needs the argument that it carries no way to reach the database",
								fset.Position(ts.Pos()), ts.Name.Name, target)
						}
						continue
					}

					switch typ := ts.Type.(type) {
					case *ast.InterfaceType:
						for _, method := range fieldList(typ.Methods) {
							fn, isFunc := method.Type.(*ast.FuncType)
							if !isFunc {
								continue
							}
							for _, result := range fieldList(fn.Results) {
								report(base+": interface "+ts.Name.Name, method.Pos(), result.Type)
							}
						}
					case *ast.StructType:
						for _, field := range fieldList(typ.Fields) {
							if !anyNameExported(field) {
								continue
							}
							report(base+": field of "+ts.Name.Name, field.Pos(), field.Type)
						}
					default:
						report(base+": type "+ts.Name.Name, ts.Pos(), ts.Type)
					}
				}
			}
		}
	}

	if checked < 5 {
		t.Fatalf("inspected only %d exported declarations; the scan is not reaching this package's API and would not notice a leaked handle", checked)
	}
}

func receiverIsExported(d *ast.FuncDecl) bool {
	if d.Recv == nil {
		return true
	}
	for _, field := range fieldList(d.Recv) {
		if ident := rootIdent(field.Type); ident != nil {
			return ident.IsExported()
		}
	}
	return false
}

func findDatabaseQualifier(expr ast.Expr) string {
	var found string
	ast.Inspect(expr, func(n ast.Node) bool {
		sel, isSelector := n.(*ast.SelectorExpr)
		if !isSelector {
			return true
		}
		qualifier, isIdent := sel.X.(*ast.Ident)
		if isIdent && databaseQualifiers[qualifier.Name] {
			found = qualifier.Name + "." + sel.Sel.Name
			return false
		}
		return true
	})
	return found
}

func rootIdent(expr ast.Expr) *ast.Ident {
	switch e := expr.(type) {
	case *ast.Ident:
		return e
	case *ast.StarExpr:
		return rootIdent(e.X)
	case *ast.IndexExpr:
		return rootIdent(e.X)
	default:
		return nil
	}
}

func fieldList(list *ast.FieldList) []*ast.Field {
	if list == nil {
		return nil
	}
	return list.List
}

func anyNameExported(field *ast.Field) bool {
	if len(field.Names) == 0 {
		ident := rootIdent(field.Type)
		return ident != nil && ident.IsExported()
	}
	for _, name := range field.Names {
		if name.IsExported() {
			return true
		}
	}
	return false
}

func exprString(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.SelectorExpr:
		return exprString(e.X) + "." + e.Sel.Name
	case *ast.StarExpr:
		return "*" + exprString(e.X)
	default:
		return "<expr>"
	}
}
