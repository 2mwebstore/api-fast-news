package models

import (
	"reflect"
	"strings"
	"testing"
	"unicode"
)

// mysqlReservedWords is the subset of MySQL 8 reserved words plausible as a
// column name in this schema. A reserved word works fine when GORM quotes the
// identifier, but any hand-written `WHERE <word> = ?` is a syntax error — which
// is how `window` shipped broken.
var mysqlReservedWords = map[string]bool{
	// Window-function words, reserved as of MySQL 8.0 — this is where `window`
	// came from.
	"window": true, "over": true, "recursive": true, "groups": true,
	"rank": true, "dense_rank": true, "percent_rank": true, "cume_dist": true,
	"row_number": true, "first_value": true, "last_value": true, "nth_value": true,
	"lead": true, "lag": true, "ntile": true, "system": true,

	// Long-standing reserved words that read like ordinary column names.
	"key": true, "index": true, "order": true, "group": true, "range": true,
	"interval": true, "match": true, "partition": true, "condition": true,
	"primary": true, "references": true, "constraint": true, "column": true,
	"check": true, "default": true, "values": true, "usage": true, "option": true,
	"read": true, "write": true, "show": true, "left": true, "right": true,
	"natural": true, "cross": true, "union": true, "select": true, "table": true,
	"schema": true, "database": true, "int": true, "char": true, "binary": true,
	"blob": true, "long": true, "lines": true, "force": true, "ignore": true,
	"asc": true, "desc": true, "distinct": true, "having": true, "limit": true,
	"where": true, "from": true, "into": true, "leading": true, "trailing": true,
}

// TestNoReservedWordColumns walks every model and fails on a column name that
// MySQL reserves, unless the field opts out with an explicit `column:` tag
// naming something safe.
func TestNoReservedWordColumns(t *testing.T) {
	for _, model := range AllModels() {
		typ := reflect.TypeOf(model)
		for typ.Kind() == reflect.Ptr {
			typ = typ.Elem()
		}
		if typ.Kind() != reflect.Struct {
			continue
		}
		checkStruct(t, typ, typ.Name())
	}
}

func checkStruct(t *testing.T, typ reflect.Type, modelName string) {
	t.Helper()

	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)

		// Embedded structs (Base) contribute their own columns.
		if field.Anonymous && field.Type.Kind() == reflect.Struct {
			checkStruct(t, field.Type, modelName)
			continue
		}

		tag := field.Tag.Get("gorm")
		if tag == "-" {
			continue
		}
		// An association is not a column.
		if field.Type.Kind() == reflect.Slice && field.Type.Elem().Kind() == reflect.Struct {
			continue
		}
		if strings.Contains(tag, "many2many") || strings.Contains(tag, "foreignKey") {
			continue
		}

		column := columnName(field, tag)
		if column == "" {
			continue
		}
		if mysqlReservedWords[column] {
			t.Errorf("%s.%s maps to column %q, which MySQL reserves — "+
				"give it an explicit `gorm:\"column:...\"` name",
				modelName, field.Name, column)
		}
	}
}

// columnName resolves the column a field maps to: the explicit tag when there
// is one, otherwise GORM's snake_case conversion of the field name.
func columnName(field reflect.StructField, tag string) string {
	for _, part := range strings.Split(tag, ";") {
		part = strings.TrimSpace(part)
		if after, found := strings.CutPrefix(part, "column:"); found {
			return strings.ToLower(strings.TrimSpace(after))
		}
	}
	return toSnakeCase(field.Name)
}

func toSnakeCase(name string) string {
	var out strings.Builder
	runes := []rune(name)
	for i, r := range runes {
		if unicode.IsUpper(r) {
			// Break before an uppercase run that starts a new word, so
			// "ImageURL" becomes image_url rather than image_u_r_l.
			if i > 0 && (unicode.IsLower(runes[i-1]) ||
				(i+1 < len(runes) && unicode.IsLower(runes[i+1]))) {
				out.WriteByte('_')
			}
			out.WriteRune(unicode.ToLower(r))
			continue
		}
		out.WriteRune(r)
	}
	return out.String()
}
