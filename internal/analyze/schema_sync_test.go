package analyze

import (
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/janiorvalle/better-git-review/internal/document"
)

func TestSchemaMatchesDocumentAnalysisTypes(t *testing.T) {
	var schema schemaNode
	if err := json.Unmarshal(Schema, &schema); err != nil {
		t.Fatal(err)
	}
	assertStructSchema(t, schema, reflect.TypeOf(document.Analysis{}), map[string]bool{
		"stubbedFiles":    true,
		"mechanicalFiles": true,
		"fileKeySymbols":  true,
		"stubbedCohorts":  true,
	})

	cohortSchema := schema.Properties["cohorts"].Items
	if cohortSchema == nil {
		t.Fatal("cohorts item schema is missing")
	}
	assertStructSchema(t, *cohortSchema, reflect.TypeOf(document.Cohort{}), nil)

	layer := cohortSchema.Properties["layer"]
	if !slices.Equal(layer.Enum, document.Layers) {
		t.Fatalf("layer enum = %#v, want %#v", layer.Enum, document.Layers)
	}
}

type schemaNode struct {
	Type                 any                   `json:"type"`
	Required             []string              `json:"required"`
	Properties           map[string]schemaNode `json:"properties"`
	Items                *schemaNode           `json:"items"`
	Enum                 []string              `json:"enum"`
	AdditionalProperties any                   `json:"additionalProperties"`
}

func assertStructSchema(t *testing.T, schema schemaNode, typ reflect.Type, ignored map[string]bool) {
	t.Helper()
	expected := map[string]bool{}
	for index := 0; index < typ.NumField(); index++ {
		field := typ.Field(index)
		name := strings.Split(field.Tag.Get("json"), ",")[0]
		if name == "" || name == "-" || ignored[name] {
			continue
		}
		expected[name] = true
		if _, ok := schema.Properties[name]; !ok {
			t.Errorf("%s.%s (%q) is missing from the JSON schema", typ.Name(), field.Name, name)
		}
	}
	for name := range schema.Properties {
		if !expected[name] {
			t.Errorf("JSON schema property %q has no matching %s field", name, typ.Name())
		}
	}
	for name := range expected {
		if !slices.Contains(schema.Required, name) {
			t.Errorf("%s property %q is not required by the JSON schema", typ.Name(), name)
		}
	}
	if value, ok := schema.AdditionalProperties.(bool); !ok || value {
		t.Errorf("%s schema must reject additional properties", typ.Name())
	}
}
