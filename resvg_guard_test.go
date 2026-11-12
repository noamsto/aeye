package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestResvgOnlyViaHelper fails when a non-test file other than resvg_run.go
// reaches resvg without runResvg: it names resvgBin, or passes "resvg" or
// resvgBin() to exec.Command/exec.CommandContext. Such a call would skip the
// memory bound and timeout.
func TestResvgOnlyViaHelper(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	checked := 0
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") || name == "resvg_run.go" {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		checked++
		ast.Inspect(f, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.Ident:
				if n.Name == "resvgBin" {
					t.Errorf("%s: uses resvgBin; run resvg through runResvg", fset.Position(n.Pos()))
				}
			case *ast.CallExpr:
				if isExecCommand(n) && execRunsResvg(n) {
					t.Errorf("%s: runs resvg with exec; use runResvg", fset.Position(n.Pos()))
				}
			}
			return true
		})
	}
	if checked == 0 {
		t.Fatal("scanned no source files")
	}
}

// isExecCommand reports whether call is exec.Command or exec.CommandContext.
func isExecCommand(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	return ok && pkg.Name == "exec" && (sel.Sel.Name == "Command" || sel.Sel.Name == "CommandContext")
}

// execRunsResvg reports whether the command name, the first argument of
// exec.Command or the second of exec.CommandContext, is "resvg" or resvgBin().
func execRunsResvg(call *ast.CallExpr) bool {
	for _, arg := range call.Args[:min(len(call.Args), 2)] {
		switch a := arg.(type) {
		case *ast.BasicLit:
			if s, err := strconv.Unquote(a.Value); err == nil && s == "resvg" {
				return true
			}
		case *ast.CallExpr:
			if id, ok := a.Fun.(*ast.Ident); ok && id.Name == "resvgBin" {
				return true
			}
		}
	}
	return false
}
