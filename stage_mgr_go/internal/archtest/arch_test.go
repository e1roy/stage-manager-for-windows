// Package archtest checks rules of AGENTS.md that the compiler cannot:
// which layer may import which, where Win32 calls may go, where words for
// people may be written. Each test names the rule it keeps, so that a
// failure says what to do instead.
package archtest

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"unicode"
)

const module = "github.com/e1roy/stage_manager_for_windows/internal/"

// file is a parsed Go file of internal/, test files included or not.
type file struct {
	pkg, path string
	test      bool
	ast       *ast.File
	fset      *token.FileSet
}

func (f file) pos(n ast.Node) string {
	p := f.fset.Position(n.Pos())
	return filepath.ToSlash(p.Filename) + ":" + strconv.Itoa(p.Line)
}

func sources(t *testing.T) []file {
	t.Helper()
	var out []file
	root := filepath.Join("..")
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") {
			return err
		}
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		pkg := strings.Split(filepath.ToSlash(rel), "/")[0]
		out = append(out, file{pkg, path, strings.HasSuffix(path, "_test.go"), f, fset})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// TestLayers keeps the dependencies of docs/CODEMAP.md going one way:
// winapi holds no app state, stage/settings/i18n/update are pure logic
// that never touch Win32, catalog/parking know windows but not stages.
func TestLayers(t *testing.T) {
	allowed := map[string][]string{
		"winapi":   {},
		"catalog":  {"settings", "winapi"},
		"parking":  {"catalog", "winapi"},
		"stage":    {},
		"settings": {"i18n"},
		"i18n":     {},
		"update":   {},
		"archtest": {},
	}
	for _, f := range sources(t) {
		// A package not named in the table would be watched by no one:
		// rather than skip it, say so, so that a new one joins the table
		// the day it is born.
		if _, ruled := allowed[f.pkg]; !ruled && f.pkg != "app" {
			t.Errorf("%s: package %s is in no layer of the table; add it or its rules", f.path, f.pkg)
		}
		if f.test {
			continue
		}
		ok, _ := allowed[f.pkg]
		if f.pkg == "app" {
			// App is the top: everything under it is allowed.
			ok = []string{"winapi", "catalog", "parking", "stage", "settings", "i18n", "update"}
		}
		for _, imp := range f.ast.Imports {
			path, _ := strconv.Unquote(imp.Path.Value)
			dep, inner := strings.CutPrefix(path, module)
			if inner && !slices.Contains(ok, dep) {
				t.Errorf("%s: %s imports %s; it may import only %v", f.pos(imp), f.pkg, dep, ok)
			}
		}
	}
}

// TestBuildTags: every file of winapi is Windows only.
func TestBuildTags(t *testing.T) {
	for _, f := range sources(t) {
		if f.pkg != "winapi" {
			continue
		}
		if len(f.ast.Comments) == 0 || f.ast.Comments[0].List[0].Text != "//go:build windows" {
			t.Errorf("%s: missing //go:build windows", f.path)
		}
	}
}

// ownWindow matches the handles of Stage Manager's own windows. Case
// matters: their fields are lowercase hwnd and uhwnd, while the windows
// of other apps are known by catalog.Window's HWND — a SetWindowPos on
// one of those is the very thing this test keeps out.
var ownWindow = regexp.MustCompile(`hwnd|NativeHandle`)

// TestMovesOfOtherAppsWindows: outside winapi and parking, the windows of
// other apps move with winapi.Move or MoveAsync, which do not wait on an
// app that hangs, and go off the desktop only through parking, whose
// journal brings them back after a crash. SetWindowPos is for Stage
// Manager's own windows.
func TestMovesOfOtherAppsWindows(t *testing.T) {
	for _, f := range sources(t) {
		if f.test || f.pkg == "winapi" || f.pkg == "parking" {
			continue
		}
		ast.Inspect(f.ast, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || !isCall(call, "winapi", "SetWindowPos") || len(call.Args) == 0 {
				return true
			}
			if !ownWindow.MatchString(exprText(call.Args[0])) {
				t.Errorf("%s: SetWindowPos on %s, which is not one of Stage Manager's own windows: use winapi.Move, MoveAsync or parking", f.pos(call), exprText(call.Args[0]))
			}
			return true
		})
	}
}

// TestWordsGoThroughI18n: the words people read are in i18n.Strings, in
// four languages, never written in the code (AGENTS.md, 界面文字).
// Chinese, Japanese and the like in a string literal outside i18n is a
// sure sign of one that was not.
func TestWordsGoThroughI18n(t *testing.T) {
	for _, f := range sources(t) {
		if f.test || f.pkg == "i18n" {
			continue
		}
		ast.Inspect(f.ast, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if ok && lit.Kind == token.STRING && strings.IndexFunc(lit.Value, func(r rune) bool {
				return unicode.Is(unicode.Han, r) || unicode.Is(unicode.Hiragana, r) || unicode.Is(unicode.Katakana, r)
			}) >= 0 {
				t.Errorf("%s: %s belongs in i18n.Strings", f.pos(lit), lit.Value)
			}
			return true
		})
	}
}

// TestHooksSyncTogether: whether each hook is wanted depends on the
// settings, the displays and quitting at once, so they are synced
// together, from App.syncHooks only. One synced on its own was once left
// out when the displays changed, and Win+D stayed taken with no display
// to clear.
func TestHooksSyncTogether(t *testing.T) {
	// installers are the winapi functions that take a hook.
	installers := regexp.MustCompile(`^Hook(WinTab|WinDesktop|MinButton)$`)
	synced := map[string]bool{}    // sync* methods syncHooks calls, anywhere.
	syncers := map[string]string{} // sync* method -> where it is declared.
	for _, f := range sources(t) {
		if f.pkg != "app" || f.test {
			continue
		}
		for _, decl := range f.ast.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			if fn.Name.Name == "syncHooks" {
				ast.Inspect(fn.Body, func(n ast.Node) bool {
					if call, ok := n.(*ast.CallExpr); ok {
						if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
							synced[sel.Sel.Name] = true
						}
					}
					return true
				})
				continue
			}
			touchesHooks := false
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				if isCall(call, "winapi", "") && installers.MatchString(sel.Sel.Name) {
					touchesHooks = true
					if !strings.HasPrefix(fn.Name.Name, "sync") {
						t.Errorf("%s: %s installs a hook; install it in a sync* method called by syncHooks", f.pos(call), fn.Name.Name)
					}
				}
				// The hooks are let go through the App's own unhook* fields.
				if id, ok := sel.X.(*ast.Ident); ok && id.Name == "a" && strings.HasPrefix(sel.Sel.Name, "unhook") {
					touchesHooks = true
				}
				return true
			})
			if touchesHooks && fn.Name.Name != "syncHooks" && fn.Name.Name != "shutdown" {
				// shutdown calls syncHooks itself, and lets the WinEvent
				// hook go as the app ends.
				syncers[fn.Name.Name] = f.pos(fn)
			}
		}
	}
	// Every sync* method must be called by syncHooks: one synced on its
	// own was once left out when the displays changed.
	if len(synced) == 0 {
		t.Fatal("no syncHooks found in the app package")
	}
	for name, at := range syncers {
		if !synced[name] {
			t.Errorf("%s: %s syncs a hook syncHooks does not call", at, name)
		}
	}
}

// isCall reports whether call is pkg.name(...).
func isCall(call *ast.CallExpr, pkg, name string) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != name {
		return false
	}
	id, ok := sel.X.(*ast.Ident)
	return ok && id.Name == pkg
}

// exprText is the source of a simple expression, enough to name it.
func exprText(e ast.Expr) string {
	switch e := e.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.SelectorExpr:
		return exprText(e.X) + "." + e.Sel.Name
	case *ast.CallExpr:
		var args []string
		for _, a := range e.Args {
			args = append(args, exprText(a))
		}
		return exprText(e.Fun) + "(" + strings.Join(args, ", ") + ")"
	case *ast.ParenExpr:
		return exprText(e.X)
	}
	return "?"
}
