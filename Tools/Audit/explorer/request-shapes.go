// Command request-shapes reads Go syntax only. It never imports/executes a host
// runtime, contacts a board, or infers semantic requiredness from Go zero values.
package main

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"reflect"
	"strconv"
	"strings"
)

type source struct{ Name, Text string }
type schema map[string]any

var types = map[string][]ast.Expr{}
var scope string

func namedShape(name string, depth int) schema {
	if !strings.Contains(name, ".") {
		name = scope + "." + name
	}
	previous := scope
	scope = strings.Split(name, ".")[0]
	defer func() { scope = previous }()
	var structs []ast.Expr
	for _, expr := range types[name] {
		if _, ok := expr.(*ast.StructType); ok {
			structs = append(structs, expr)
		}
	}
	if len(structs) == 1 {
		value := shape(structs[0], depth+1)
		value["x-go-type"] = name
		return value
	}
	if len(types[name]) == 1 {
		return shape(types[name][0], depth+1)
	}
	return schema{}
}

func shape(expr ast.Expr, depth int) schema {
	if depth > 6 {
		return schema{}
	}
	switch t := expr.(type) {
	case *ast.StarExpr:
		return shape(t.X, depth+1)
	case *ast.ArrayType:
		if element, ok := t.Elt.(*ast.Ident); ok && (element.Name == "byte" || element.Name == "uint8") && t.Len == nil {
			return schema{"type": "string", "contentEncoding": "base64"}
		}
		return schema{"type": "array", "items": shape(t.Elt, depth+1)}
	case *ast.MapType:
		return schema{"type": "object", "additionalProperties": shape(t.Value, depth+1)}
	case *ast.StructType:
		props := map[string]schema{}
		for _, field := range t.Fields.List {
			if len(field.Names) == 0 {
				if embedded, ok := shape(field.Type, depth+1)["properties"].(map[string]schema); ok {
					for name, value := range embedded {
						props[name] = value
					}
				}
				continue
			}
			if field.Tag == nil {
				continue
			}
			tag, _ := strconv.Unquote(field.Tag.Value)
			name := strings.Split(reflect.StructTag(tag).Get("json"), ",")[0]
			if name == "" || name == "-" {
				continue
			}
			value := shape(field.Type, depth+1)
			if field.Doc != nil {
				value["description"] = strings.TrimSpace(field.Doc.Text())
			}
			props[name] = value
		}
		return schema{"type": "object", "properties": props}
	case *ast.Ident:
		switch t.Name {
		case "string":
			return schema{"type": "string"}
		case "bool":
			return schema{"type": "boolean"}
		case "float32", "float64":
			return schema{"type": "number"}
		case "int", "int8", "int16", "int32", "int64":
			return schema{"type": "integer"}
		case "uint", "uint8", "uint16", "uint32", "uint64", "byte":
			return schema{"type": "integer", "minimum": 0}
		}
		return namedShape(t.Name, depth)
	case *ast.SelectorExpr:
		if pkg, ok := t.X.(*ast.Ident); ok {
			if pkg.Name == "time" && t.Sel.Name == "Time" {
				return schema{"type": "string", "format": "date-time"}
			}
			return namedShape(pkg.Name+"."+t.Sel.Name, depth)
		}
	}
	return schema{} // A named/custom payload is not falsely described as a scalar.
}

func fields(node ast.Node, helpers map[string]*ast.FuncDecl, depth int) schema {
	if depth > 3 {
		return nil
	}
	vars := map[string]ast.Expr{}
	ast.Inspect(node, func(n ast.Node) bool {
		if v, ok := n.(*ast.ValueSpec); ok && v.Type != nil {
			for _, name := range v.Names {
				vars[name.Name] = v.Type
			}
		}
		return true
	})
	var found schema
	ast.Inspect(node, func(n ast.Node) bool {
		if found != nil {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		name := ""
		switch f := call.Fun.(type) {
		case *ast.Ident:
			name = f.Name
		case *ast.SelectorExpr:
			name = f.Sel.Name
		}
		argument := 1
		if name == "Decode" {
			argument = 0
		}
		if (name == "decodeParams" || name == "decodeStrictParams" || name == "Unmarshal" || name == "Decode" || name == "decodeHTTP" || name == "decodeHTTPJSON") && len(call.Args) > argument {
			if name == "decodeHTTP" || name == "decodeHTTPJSON" {
				argument = len(call.Args) - 1
			}
			if address, ok := call.Args[argument].(*ast.UnaryExpr); ok {
				if id, ok := address.X.(*ast.Ident); ok {
					expr := vars[id.Name]
					// The same handler can decode another local `params` in a
					// nested command branch. Resolve the identifier's declaration,
					// not the last variable with that spelling in the whole body.
					if id.Obj != nil {
						if declaration, ok := id.Obj.Decl.(*ast.ValueSpec); ok {
							expr = declaration.Type
						}
					}
					if expr != nil {
						candidate := shape(expr, 0)
						if props, ok := candidate["properties"].(map[string]schema); ok && len(props) > 0 {
							found = candidate
						}
					}
				}
			}
		}
		return true
	})
	if found != nil {
		return found
	}
	ast.Inspect(node, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || found != nil {
			return true
		}
		if f, ok := call.Fun.(*ast.SelectorExpr); ok {
			if helper := helpers[f.Sel.Name]; helper != nil {
				found = fields(helper.Body, helpers, depth+1)
			}
		}
		return true
	})
	return found
}

func main() {
	var sources []source
	if err := json.NewDecoder(os.Stdin).Decode(&sources); err != nil {
		panic(err)
	}
	fset := token.NewFileSet()
	var files []*ast.File
	helpers := map[string]*ast.FuncDecl{}
	for _, s := range sources {
		file, err := parser.ParseFile(fset, s.Name, s.Text, parser.ParseComments)
		if err != nil {
			panic(err)
		}
		files = append(files, file)
		for _, decl := range file.Decls {
			if group, ok := decl.(*ast.GenDecl); ok && group.Tok == token.TYPE {
				for _, entry := range group.Specs {
					if t, ok := entry.(*ast.TypeSpec); ok {
						key := file.Name.Name + "." + t.Name.Name
						types[key] = append(types[key], t.Type)
					}
				}
			}
			if fn, ok := decl.(*ast.FuncDecl); ok {
				helpers[fn.Name.Name] = fn
			}
		}
	}
	result := map[string]schema{}
	priorities := map[string]int{}
	httpShapes := map[string]schema{}
	for _, file := range files {
		scope = file.Name.Name
		ast.Inspect(file, func(node ast.Node) bool {
			var names []string
			var body ast.Node
			switch n := node.(type) {
			case *ast.CallExpr:
				if selector, ok := n.Fun.(*ast.SelectorExpr); ok && selector.Sel.Name == "HandleFunc" && len(n.Args) == 2 {
					if path, ok := n.Args[0].(*ast.BasicLit); ok && path.Kind == token.STRING {
						name, _ := strconv.Unquote(path.Value)
						if value := fields(n.Args[1], helpers, 0); value != nil {
							httpShapes[name] = value
						}
					}
				}
			case *ast.CaseClause:
				for _, e := range n.List {
					if lit, ok := e.(*ast.BasicLit); ok && lit.Kind == token.STRING {
						name, _ := strconv.Unquote(lit.Value)
						if strings.HasPrefix(name, "controller.") {
							names = append(names, name)
						}
					}
				}
				body = n
			case *ast.IfStmt:
				if eq, ok := n.Cond.(*ast.BinaryExpr); ok && eq.Op == token.EQL {
					if lit, ok := eq.Y.(*ast.BasicLit); ok && lit.Kind == token.STRING {
						name, _ := strconv.Unquote(lit.Value)
						if strings.HasPrefix(name, "controller.") {
							names = []string{name}
							body = n.Body
						}
					}
				}
			}
			if len(names) > 0 {
				if value := fields(body, helpers, 0); value != nil {
					priority := 1
					// Policy classifiers also switch on method names and may
					// decode a permissive subset. Prefer the actual dispatcher.
					for _, declaration := range file.Decls {
						if fn, ok := declaration.(*ast.FuncDecl); ok && fn.Pos() <= node.Pos() && node.End() <= fn.End() {
							if fn.Name.Name == "dispatch" || fn.Name.Name == "DispatchRPC" {
								priority = 2
							}
							break
						}
					}
					for _, name := range names {
						if result[name] == nil || priority > priorities[name] {
							result[name] = value
							priorities[name] = priority
						}
					}
				}
			}
			return true
		})
	}
	named := map[string]schema{}
	for name := range types {
		named[name] = namedShape(name, 0)
	}
	result["$types"] = schema{}
	for name, value := range named {
		result["$types"][name] = value
	}
	result["$http"] = schema{}
	for name, value := range httpShapes {
		result["$http"][name] = value
	}
	if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {
		panic(err)
	}
}
