package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	root := "/Users/png/01. Project/coderpush"
	fset := token.NewFileSet()

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if strings.Contains(path, ".git") || strings.Contains(path, ".agents") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, "_test.go") {
			return nil
		}

		node, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if err != nil {
			fmt.Printf("Error parsing %s: %v\n", path, err)
			return nil
		}

		ast.Inspect(node, func(n ast.Node) bool {
			if cl, ok := n.(*ast.CompositeLit); ok {
				// Direct domain.Product or *domain.Product
				isProduct := false
				switch t := cl.Type.(type) {
				case *ast.SelectorExpr:
					if x, ok := t.X.(*ast.Ident); ok && x.Name == "domain" && t.Sel.Name == "Product" {
						isProduct = true
					}
				case *ast.Ident:
					if t.Name == "Product" {
						isProduct = true
					}
				}
				if isProduct {
					hasSupplierTimestamp := false
					isZero := false
					for _, elt := range cl.Elts {
						if kv, ok := elt.(*ast.KeyValueExpr); ok {
							if k, ok := kv.Key.(*ast.Ident); ok && k.Name == "SupplierTimestamp" {
								hasSupplierTimestamp = true
								if clVal, ok := kv.Value.(*ast.CompositeLit); ok {
									if sel, ok := clVal.Type.(*ast.SelectorExpr); ok {
										if sel.Sel.Name == "Time" && len(clVal.Elts) == 0 {
											isZero = true
										}
									}
								}
							}
						}
					}
					pos := fset.Position(cl.Pos())
					if !hasSupplierTimestamp {
						fmt.Printf("[PRODUCT MISSING SUPPLIER_TIMESTAMP] %s:%d\n", pos.Filename, pos.Line)
					} else if isZero {
						fmt.Printf("[PRODUCT ZERO SUPPLIER_TIMESTAMP] %s:%d\n", pos.Filename, pos.Line)
					}
				}
			}
			return true
		})

		return nil
	})

	if err != nil {
		fmt.Printf("Walk error: %v\n", err)
	}
}
