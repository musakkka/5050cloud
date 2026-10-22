// SPDX-License-Identifier: MIT

package api

import (
	"go/ast"
	"go/parser"
	"go/token"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

var camel = regexp.MustCompile(`^[a-z][a-zA-Z0-9]*$`)

// TestJSONTags fails when an exported field of a REST type has no json tag
// or one that is not camelCase. Route (the table type) is Go-only.
func TestJSONTags(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "types.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	ast.Inspect(f, func(n ast.Node) bool {
		ts, ok := n.(*ast.TypeSpec)
		if !ok {
			return true
		}
		st, ok := ts.Type.(*ast.StructType)
		if !ok {
			return false
		}
		for _, field := range st.Fields.List {
			if len(field.Names) == 0 {
				continue // embedded
			}
			if field.Tag == nil {
				t.Errorf("%s.%s has no json tag", ts.Name.Name, field.Names[0].Name)
				continue
			}
			tag, _ := strconv.Unquote(field.Tag.Value)
			name, _, _ := strings.Cut(reflect.StructTag(tag).Get("json"), ",")
			if !camel.MatchString(name) {
				t.Errorf("%s.%s: json name %q is not camelCase", ts.Name.Name, field.Names[0].Name, name)
			}
		}
		return false
	})
}
