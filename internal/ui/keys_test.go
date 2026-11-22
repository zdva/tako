package ui

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"testing"

	"github.com/zdva/tako/internal/i18n"
)

// Every translation key the code uses exists in both languages, and neither
// language carries keys the code does not use.
func TestKeys_MatchLocales(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "keys.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	used := map[string]bool{}
	ast.Inspect(f, func(n ast.Node) bool {
		if vs, ok := n.(*ast.ValueSpec); ok {
			for _, v := range vs.Values {
				if lit, ok := v.(*ast.BasicLit); ok {
					s, _ := strconv.Unquote(lit.Value)
					used[s] = true
				}
			}
		}
		return true
	})
	for _, lang := range i18n.Languages() {
		have := map[string]bool{}
		for _, k := range i18n.Keys(lang) {
			have[k] = true
			if !used[k] {
				t.Errorf("%s has the unused key %q", lang, k)
			}
		}
		for k := range used {
			if !have[k] {
				t.Errorf("%s lacks the key %q", lang, k)
			}
		}
	}
}
