// Command funcmap writes a JSON map of every function and method in the module
// (non-test code), with exact caller/callee edges resolved through go/types.
// The documentation site renders it as the "Code map" page, so the map is
// regenerated from source on every build and cannot drift from the code.
//
//	go run ./site/tools/funcmap > site/public/funcmap.json
package main

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const module = "github.com/ysinghc/snowflake-lite"

// Dirs holds the packages to map, as module-relative directories.
var dirs = []string{"snowflake", "saas", "cmd/snowflake", "wasm"}

type Func struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Recv     string   `json:"recv,omitempty"`
	Sig      string   `json:"sig"`
	File     string   `json:"file"`
	Line     int      `json:"line"`
	Exported bool     `json:"exported"`
	Doc      string   `json:"doc,omitempty"`
	Calls    []string `json:"calls,omitempty"`
	CalledBy []string `json:"calledBy,omitempty"`
}

type Pkg struct {
	Path  string  `json:"path"`
	Dir   string  `json:"dir"`
	Doc   string  `json:"doc,omitempty"`
	Funcs []*Func `json:"funcs"`
}

func main() {
	fset := token.NewFileSet()
	imp := importer.ForCompiler(fset, "source", nil)
	byObj := map[types.Object]*Func{}
	byID := map[string]*Func{}
	var pkgs []*Pkg
	type unit struct {
		files []*ast.File
		info  *types.Info
		pkg   *Pkg
	}
	var units []unit

	for _, dir := range dirs {
		matches, _ := filepath.Glob(filepath.Join(dir, "*.go"))
		var files []*ast.File
		pk := &Pkg{Path: module + "/" + dir, Dir: dir}
		for _, m := range matches {
			if strings.HasSuffix(m, "_test.go") {
				continue
			}
			f, err := parser.ParseFile(fset, m, nil, parser.ParseComments)
			if err != nil {
				fatal(err)
			}
			// The wasm package is a js/wasm program with a native stub; map the real bindings.
			if wasm := isWasm(f); wasm != (dir == "wasm") {
				continue
			}
			files = append(files, f)
			if f.Doc != nil && pk.Doc == "" {
				pk.Doc = oneLine(f.Doc.Text())
			}
		}
		if len(files) == 0 {
			continue
		}
		info := &types.Info{Defs: map[*ast.Ident]types.Object{}, Uses: map[*ast.Ident]types.Object{}}
		conf := types.Config{Importer: imp, Error: func(error) {}}
		conf.Check(pk.Path, fset, files, info)
		units = append(units, unit{files, info, pk})
		pkgs = append(pkgs, pk)

		for _, f := range files {
			for _, d := range f.Decls {
				fd, ok := d.(*ast.FuncDecl)
				if !ok {
					continue
				}
				fn := &Func{
					Name:     fd.Name.Name,
					File:     filepath.ToSlash(fset.Position(fd.Pos()).Filename),
					Line:     fset.Position(fd.Pos()).Line,
					Exported: fd.Name.IsExported(),
					Sig:      signature(fd),
				}
				if fd.Doc != nil {
					fn.Doc = oneLine(fd.Doc.Text())
				}
				fn.ID = pk.Dir + "." + fd.Name.Name
				if fd.Recv != nil && len(fd.Recv.List) > 0 {
					fn.Recv = typeName(fd.Recv.List[0].Type)
					fn.ID = pk.Dir + "." + fn.Recv + "." + fd.Name.Name
				}
				if obj := info.Defs[fd.Name]; obj != nil {
					byObj[obj] = fn
				}
				byID[fn.ID] = fn
				pk.Funcs = append(pk.Funcs, fn)
			}
		}
	}

	// Resolve call edges between functions defined in this module.
	for _, u := range units {
		for _, f := range u.files {
			for _, d := range f.Decls {
				fd, ok := d.(*ast.FuncDecl)
				if !ok || fd.Body == nil {
					continue
				}
				caller := byObj[u.info.Defs[fd.Name]]
				if caller == nil {
					continue
				}
				seen := map[string]bool{}
				ast.Inspect(fd.Body, func(n ast.Node) bool {
					var id *ast.Ident
					switch x := n.(type) {
					case *ast.CallExpr:
						switch fun := x.Fun.(type) {
						case *ast.Ident:
							id = fun
						case *ast.SelectorExpr:
							id = fun.Sel
						}
					case *ast.SelectorExpr:
						// Method values passed as handlers, e.g. mux.HandleFunc("/x", s.handle).
						id = x.Sel
					case *ast.Ident:
						id = x
					}
					if id == nil {
						return true
					}
					if callee := byObj[u.info.Uses[id]]; callee != nil && callee != caller && !seen[callee.ID] {
						seen[callee.ID] = true
						caller.Calls = append(caller.Calls, callee.ID)
						callee.CalledBy = append(callee.CalledBy, caller.ID)
					}
					return true
				})
			}
		}
	}

	for _, p := range pkgs {
		sort.SliceStable(p.Funcs, func(i, j int) bool {
			a, b := p.Funcs[i], p.Funcs[j]
			if a.File != b.File {
				return a.File < b.File
			}
			return a.Line < b.Line
		})
		for _, f := range p.Funcs {
			sort.Strings(f.Calls)
			sort.Strings(f.CalledBy)
		}
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", " ")
	if err := enc.Encode(map[string]any{"module": module, "packages": pkgs}); err != nil {
		fatal(err)
	}
}

func isWasm(f *ast.File) bool {
	for _, cg := range f.Comments {
		for _, c := range cg.List {
			if strings.HasPrefix(c.Text, "//go:build") && strings.Contains(c.Text, "js") && !strings.Contains(c.Text, "!") {
				return true
			}
		}
	}
	return false
}

func signature(fd *ast.FuncDecl) string {
	var b strings.Builder
	b.WriteString(fd.Name.Name)
	b.WriteString(fields(fd.Type.Params, true))
	if r := fd.Type.Results; r != nil && len(r.List) > 0 {
		s := fields(r, len(r.List) > 1 || len(r.List[0].Names) > 0)
		if len(r.List) == 1 && len(r.List[0].Names) == 0 {
			s = " " + exprString(r.List[0].Type)
		} else {
			s = " " + s
		}
		b.WriteString(s)
	}
	return b.String()
}

func fields(fl *ast.FieldList, parens bool) string {
	var parts []string
	if fl != nil {
		for _, f := range fl.List {
			t := exprString(f.Type)
			if len(f.Names) == 0 {
				parts = append(parts, t)
				continue
			}
			var ns []string
			for _, n := range f.Names {
				ns = append(ns, n.Name)
			}
			parts = append(parts, strings.Join(ns, ", ")+" "+t)
		}
	}
	s := strings.Join(parts, ", ")
	if parens {
		return "(" + s + ")"
	}
	return s
}

func exprString(e ast.Expr) string {
	switch x := e.(type) {
	case *ast.Ident:
		return x.Name
	case *ast.StarExpr:
		return "*" + exprString(x.X)
	case *ast.SelectorExpr:
		return exprString(x.X) + "." + x.Sel.Name
	case *ast.ArrayType:
		return "[" + map[bool]string{true: "", false: "..."}[x.Len == nil] + "]" + exprString(x.Elt)
	case *ast.MapType:
		return "map[" + exprString(x.Key) + "]" + exprString(x.Value)
	case *ast.InterfaceType:
		return "any"
	case *ast.Ellipsis:
		return "..." + exprString(x.Elt)
	case *ast.FuncType:
		return "func" + fields(x.Params, true)
	}
	return "?"
}

func typeName(e ast.Expr) string {
	if s, ok := e.(*ast.StarExpr); ok {
		return typeName(s.X)
	}
	if id, ok := e.(*ast.Ident); ok {
		return id.Name
	}
	return "?"
}

// oneLine keeps the first sentence of a doc comment.
func oneLine(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if i := strings.Index(s, ". "); i > 0 {
		s = s[:i+1]
	}
	return s
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "funcmap:", err)
	os.Exit(1)
}
