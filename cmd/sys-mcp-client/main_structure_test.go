package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"testing"
)

func TestMainCallsClientRun(t *testing.T) {
	fset := token.NewFileSet()
	path := filepath.Join("main.go")
	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}

	var found bool
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		pkg, ok := sel.X.(*ast.Ident)
		if !ok {
			return true
		}
		if pkg.Name != "client" || sel.Sel.Name != "Run" || len(call.Args) != 2 {
			return true
		}
		if ident, ok := call.Args[0].(*ast.Ident); !ok || ident.Name != "ctx" {
			return true
		}
		if ident, ok := call.Args[1].(*ast.Ident); !ok || ident.Name != "configPath" {
			return true
		}
		found = true
		return false
	})

	if !found {
		t.Fatal("expected main.go to call client.Run(ctx, configPath)")
	}
}
