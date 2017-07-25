// godepth calculates maximum depth of go methods in Go source code.
//
// This work was mainly inspired by github.com/fzipp/gocyclo
//
// Usage:
//      godepth [<flag> ...] <Go file or directory> ...
//
// Flags:
//      -over N   show functions with depth > N only and
//                return exit code 1 if the output is non-empty
//      -top N    show the top N most complex functions only
//      -avg      show the average depth
//
// The output fields for each line are:
// <depth> <package> <function> <file:row:column>
package main

import (
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const usageDoc = `Calculate maximum depth of Go functions.
Usage:
        godepth [flags...] <Go file or directory> ...

Flags:
        -over N        show functions with depth > N only and
                       return exit code 1 if the set is non-empty
        -top N         show the top N most complex functions only
        -avg           show the average depth over all functions,
                       not depending on whether -over or -top are set

The output fields for each line are:
<depth> <package> <function> <file:row:column>
`

func usage() {
	fmt.Fprintf(os.Stderr, usageDoc)
	os.Exit(2)
}

var (
	over     = flag.Int("over", 0, "show functions with depth > N only")
	top      = flag.Int("top", -1, "show the top N deepest functions only")
	avg      = flag.Bool("avg", false, "show the average deepness")
)

func main() {
	flag.Usage = usage
	flag.Parse()
	args := flag.Args()
	if len(args) == 0 {
		usage()
	}

	stats := analyze(args)
	sort.Sort(byDepth(stats))
	written := writeStats(os.Stdout, stats)

	if *avg {
		showAverage(stats)
	}

	if *over > 0 && written > 0 {
		os.Exit(1)
	}
}

func analyze(paths []string) []stat {
	stats := []stat{}
	for _, path := range paths {
		if isDir(path) {
			stats = analyzeDir(path, stats)
		} else {
			stats = analyzeFile(path, stats)
		}
	}
	return stats
}

func isDir(filename string) bool {
	fi, err := os.Stat(filename)
	return err == nil && fi.IsDir()
}

func analyzeFile(fname string, stats []stat) []stat {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, fname, nil, 0)
	if err != nil {
		exitError(err)
	}
	return buildStats(f, fset, stats)
}

func analyzeDir(dirname string, stats []stat) []stat {
	filepath.Walk(dirname, func(path string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() && strings.HasSuffix(path, ".go") {
			stats = analyzeFile(path, stats)
		}
		return err
	})
	files, _ := filepath.Glob(filepath.Join(dirname, "*.go"))
	for _, file := range files {
		stats = analyzeFile(file, stats)
	}
	return stats
}

func exitError(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}

func writeStats(w io.Writer, sortedStats []stat) int {
	for i, stat := range sortedStats {
		if i == *top {
			return i
		}
		if stat.Depth <= *over {
			return i
		}

		fmt.Fprintln(w, stat)
	}
	return len(sortedStats)
}

func showAverage(stats []stat) {
	fmt.Printf("Average: %.3g\n", average(stats))
}

func average(stats []stat) float64 {
	total := 0
	for _, s := range stats {
		total += s.Depth
	}
	return float64(total) / float64(len(stats))
}

type stat struct {
	PkgName  string
	FuncName string
	Depth    int
	Pos      token.Position
}

func (s stat) String() string {
	return fmt.Sprintf("%d %s %s %s", s.Depth, s.PkgName, s.FuncName, s.Pos)
}

type byDepth []stat

func (s byDepth) Len() int      { return len(s) }
func (s byDepth) Swap(i, j int) { s[i], s[j] = s[j], s[i] }
func (s byDepth) Less(i, j int) bool {
	return s[i].Depth >= s[j].Depth
}

func buildStats(f *ast.File, fset *token.FileSet, stats []stat) []stat {
	for _, decl := range f.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok {
			stats = append(stats, stat{
				PkgName:  f.Name.Name,
				FuncName: funcName(fn),
				Depth:    depth(fn),
				Pos:      fset.Position(fn.Pos()),
			})
		}
	}
	return stats
}

// funcName returns the name representation of a function or method:
// "(Type).Name" for methods or simply "Name" for functions.
func funcName(fn *ast.FuncDecl) string {
	if fn.Recv != nil {
		typ := fn.Recv.List[0].Type
		return fmt.Sprintf("(%s).%s", recvString(typ), fn.Name)
	}
	return fn.Name.Name
}

// recvString returns a string representation of recv of the
// form "T", "*T", or "BADRECV" (if not a proper receiver type).
func recvString(recv ast.Expr) string {
	switch t := recv.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		return "*" + recvString(t.X)
	}
	return "BADRECV"
}

func max(s []int) (m int) {
	for _, value := range s {
		if value > m {
			m = value
		}
	}
	return
}

// depth calculates the depth of a function
func depth(fn *ast.FuncDecl) int {
	allDepth := []int{}
	for _, lvl := range fn.Body.List {
		v := maxDepthVisitor{}
		ast.Walk(&v, lvl)
		allDepth = append(allDepth, max(v.NodeDepth))
	}
	return max(allDepth)
}

type maxDepthVisitor struct {
	Depth     int
	NodeDepth []int
	Lbrace    token.Pos
	Rbrace    token.Pos
}

// Visit implements the ast.Visitor interface.
func (v *maxDepthVisitor) Visit(node ast.Node) ast.Visitor {
	switch n := node.(type) {
	case *ast.BlockStmt:
		if v.Rbrace == 0 && v.Lbrace == 0 {
			v.Lbrace = n.Lbrace
			v.Rbrace = n.Rbrace
		}

		if n.Lbrace > v.Lbrace && n.Rbrace > v.Rbrace {
			v.Depth--
		}

		v.Lbrace = n.Lbrace
		v.Rbrace = n.Rbrace
		v.Depth++
		v.NodeDepth = append(v.NodeDepth, v.Depth)
	}

	return v
}
