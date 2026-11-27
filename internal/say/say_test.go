package say

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// formats finds every message the module makes with say.New or say.Errorf.
func formats(t *testing.T) map[string]string {
	t.Helper()
	found := map[string]string{}
	root := filepath.Join("..", "..")
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && path != root && (strings.HasPrefix(d.Name(), ".") || d.Name() == "macos" || d.Name() == "dist") {
			return filepath.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		set := token.NewFileSet()
		file, err := parser.ParseFile(set, path, nil, 0)
		if err != nil {
			return err
		}
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || (sel.Sel.Name != "New" && sel.Sel.Name != "Errorf") {
				return true
			}
			pkg, ok := sel.X.(*ast.Ident)
			if !ok || pkg.Name != "say" {
				return true
			}
			where := set.Position(call.Pos()).String()
			lit, ok := call.Args[0].(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				t.Errorf("%s: the message is not written out, so it cannot be put into another language", where)
				return true
			}
			text, _ := strconv.Unquote(lit.Value)
			found[text] = where
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return found
}

func TestEveryMessageIsInRussian(t *testing.T) {
	used := formats(t)
	if len(used) < 50 {
		t.Fatalf("found only %d messages: the search is broken", len(used))
	}
	for text, where := range used {
		if _, ok := ru[text]; !ok {
			t.Errorf("%s: no Russian for %q", where, text)
		}
	}
	for text := range ru {
		if _, ok := used[text]; !ok {
			t.Errorf("the Russian for %q is for a message nothing makes", text)
		}
	}
}

var verb = regexp.MustCompile(`%(\[\d+\])?[-+# 0]*\d*(\.\d+)?[a-zA-Z%]`)

func TestTranslationsTakeTheSameArguments(t *testing.T) {
	for en, text := range ru {
		// Arguments of the kinds the English takes.
		var args []any
		for _, v := range verb.FindAllString(en, -1) {
			switch {
			case v == "%%":
			case strings.HasSuffix(v, "d"):
				args = append(args, 1)
			default:
				args = append(args, "x")
			}
		}
		out := fmt.Sprintf(strings.ReplaceAll(text, "%w", "%v"), args...)
		if strings.Contains(out, "%!") {
			t.Errorf("%q: the Russian %q does not take the same arguments: %s", en, text, out)
		}
	}
}

func TestIn(t *testing.T) {
	inner := Errorf("no policy %q", "home")
	err := Errorf("line %d: %w", 3, inner)
	if got := err.Error(); got != `line 3: no policy "home"` {
		t.Fatalf("English: %s", got)
	}
	if !errors.Is(err, inner) {
		t.Fatal("the inner error is not reachable")
	}
	ru := In("ru", err)
	if strings.Contains(ru, "policy") || !strings.Contains(ru, "home") || !strings.Contains(ru, "3") {
		t.Fatalf("Russian: %s", ru)
	}
	plain := fmt.Errorf("wrapped: %w", os.ErrNotExist)
	if In("ru", plain) != plain.Error() {
		t.Fatal("an error not made here changed")
	}
	all := Join([]error{New("the link has no password"), errors.New("raw")})
	if got := In("ru", all); !strings.HasSuffix(got, "; raw") || strings.Contains(got, "password") {
		t.Fatalf("join: %s", got)
	}
	SetLanguage("ru")
	defer SetLanguage("")
	if got := Text(New("no such server")); got != "Нет такого сервера" {
		t.Fatalf("Text: %s", got)
	}
	if Join(nil) != nil {
		t.Fatal("an empty join is not nil")
	}
}
