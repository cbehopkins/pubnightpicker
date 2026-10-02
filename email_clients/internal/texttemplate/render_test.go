package texttemplate

import (
	"maps"
	"reflect"
	"strings"
	"testing"
)

func TestRender(t *testing.T) {
	variables := map[string]any{"name": "A&B <Guest>", "count": 3, "enabled": true}
	before := maps.Clone(variables)
	got, err := Render("{{name}} {{ name }} {{count}} {{enabled}}", variables)
	if err != nil || got != "A&B <Guest> A&B <Guest> 3 true" {
		t.Fatalf("Render = %q, %v", got, err)
	}
	if !reflect.DeepEqual(variables, before) {
		t.Fatal("rendering changed variables")
	}
	if got, err := Render("literal text", nil); err != nil || got != "literal text" {
		t.Fatalf("literal Render = %q, %v", got, err)
	}
	if got, err := Render("{{{name}}}", variables); err != nil || got != "{A&B <Guest>}" {
		t.Fatalf("surrounding braces Render = %q, %v", got, err)
	}
}

func TestRenderRejectsMissingAndUnsupportedPlaceholders(t *testing.T) {
	for _, source := range []string{"{{user.name}}", "{{#if name}}", "{{name", "name}}"} {
		if err := Validate(source); err == nil {
			t.Errorf("Validate(%q) succeeded", source)
		}
		if _, err := Render(source, nil); err == nil {
			t.Errorf("Render(%q) succeeded", source)
		}
	}
	if _, err := Render("Hello {{name}}", nil); err == nil || !strings.Contains(err.Error(), `missing variable "name"`) {
		t.Fatalf("missing variable error = %v", err)
	}
}
