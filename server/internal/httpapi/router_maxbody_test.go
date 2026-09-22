package httpapi

import (
	"github.com/ankek/Household-Objects-Dev/server/internal/attachments"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http/httptest"
	"strings"
	"testing"
)

func mountV1FuncDecl(t *testing.T, fset *token.FileSet) *ast.FuncDecl {
	t.Helper()
	file, err := parser.ParseFile(fset, "router.go", nil, 0)
	if err != nil {
		t.Fatalf("parse router.go: %v", err)
	}
	for _, decl := range file.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok && fn.Name.Name == "mountV1" {
			return fn
		}
	}
	t.Fatal("router.go no longer defines mountV1; this test can no longer find what it is meant to check")
	return nil
}

type topLevelGroupCall struct {
	pos token.Pos
	lit *ast.FuncLit
}

func topLevelGroupCalls(stmts []ast.Stmt) []topLevelGroupCall {
	var out []topLevelGroupCall
	for _, stmt := range stmts {
		switch s := stmt.(type) {
		case *ast.ExprStmt:
			call, ok := s.X.(*ast.CallExpr)
			if !ok {
				continue
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "Group" || len(call.Args) != 1 {
				continue
			}
			lit, ok := call.Args[0].(*ast.FuncLit)
			if !ok {
				continue
			}
			out = append(out, topLevelGroupCall{pos: call.Pos(), lit: lit})
		case *ast.IfStmt:
			if s.Body != nil {
				out = append(out, topLevelGroupCalls(s.Body.List)...)
			}
		}
	}
	return out
}

func isMaxBodyCall(expr ast.Expr) bool {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	return ok && pkg.Name == "middleware" && sel.Sel.Name == "MaxBody"
}

func hasOwnMaxBodyUse(body *ast.BlockStmt) bool {
	for _, stmt := range body.List {
		exprStmt, ok := stmt.(*ast.ExprStmt)
		if !ok {
			continue
		}
		call, ok := exprStmt.X.(*ast.CallExpr)
		if !ok {
			continue
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Use" {
			continue
		}
		for _, arg := range call.Args {
			if isMaxBodyCall(arg) {
				return true
			}
		}
	}
	return false
}

func TestEveryTopLevelRouteGroupInstallsItsOwnMaxBody(t *testing.T) {
	fset := token.NewFileSet()
	mountV1 := mountV1FuncDecl(t, fset)

	groups := topLevelGroupCalls(mountV1.Body.List)
	if len(groups) == 0 {
		t.Fatal("found zero top-level r.Group(...) calls in mountV1; either the router was restructured in a way this AST walk no longer recognises, or the walk itself is broken -- either way this test is currently asserting nothing")
	}

	for _, g := range groups {
		if !hasOwnMaxBodyUse(g.lit.Body) {
			t.Errorf("the route group starting at router.go:%d has no middleware.MaxBody call among its own top-level Use()s; every route registered inside it would carry NO body-size cap at all -- see Config.MaxBodyBytes' own doc, \"Why this is no longer applied at the root of the middleware chain\"", fset.Position(g.pos).Line)
		}
	}
}

func TestAttachmentsGroupAcceptsABodyLargerThanTheGeneralDefaultCap(t *testing.T) {
	const smallDefault = 64

	itemsCfg := itemsTestConfig(fakeItemRepository{})
	itemsCfg.MaxBodyBytes = smallDefault
	itemsRouter, err := NewRouter(itemsCfg)
	if err != nil {
		t.Fatalf("NewRouter(items): %v", err)
	}
	oversizedJSON := httptest.NewRequest("POST", "/api/v1/items", strings.NewReader(strings.Repeat("x", smallDefault+1)))
	oversizedJSON.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	itemsRouter.ServeHTTP(rec, oversizedJSON)
	if rec.Code != 413 {
		t.Fatalf("general authenticated group: POST /items with a %d-byte body = %d, want 413 (sanity check on smallDefault itself)", smallDefault+1, rec.Code)
	}

	attCfg := attachmentsTestConfig(t, liveAttachmentItem(), creatingAttachmentRepository())
	attCfg.MaxBodyBytes = smallDefault
	attCfg.MaxAttachmentBytes = attachments.DefaultMaxUploadBytes
	if got, want := uploadBodyLimit(attCfg), int64(smallDefault); got <= want {
		t.Fatalf("uploadBodyLimit(attCfg) = %d, want it to be well over smallDefault (%d) for this test to mean anything", got, want)
	}

	attRouter, err := NewRouter(attCfg)
	if err != nil {
		t.Fatalf("NewRouter(attachments): %v", err)
	}
	payload := bytesReaderContent(smallDefault * 4)
	rec = doAttachmentUpload(t, attRouter, "item-1", attachments.CategoryImage, "photo.jpg", payload)
	if rec.Code == 413 {
		t.Fatalf("attachments group: POST /items/{itemID}/attachments with a %d-byte file (< uploadBodyLimit, > cfg.MaxBodyBytes=%d) = 413; the attachments group's own, larger cap is no longer taking effect -- see this file's own doc", len(payload), smallDefault)
	}
	if rec.Code != 201 {
		t.Fatalf("attachments group: upload = %d, want 201 (a status other than 413 proves the body cap did not fire, but this pins the whole happy path too): %s", rec.Code, rec.Body.String())
	}
}

func bytesReaderContent(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte('a' + i%26)
	}
	return b
}
