package shared_test

import (
	"encoding/json"
	"errors"
	"testing"

	sdb "github.com/nimling/samna-auth-middleware/v2/shared"
)

func TestProfileSchemaIsATypedStructWithAFixedVocabulary(t *testing.T) {
	var schema sdb.ProfileSchema
	if err := json.Unmarshal([]byte(`{"type": "string", "format": "uri", "component": "link", "title": "Site", "maxLength": 200}`), &schema); err != nil {
		t.Fatalf("a schema of the vocabulary decodes: %v", err)
	}
	if schema.Type != "string" || schema.Format != "uri" || schema.Component != "link" || *schema.MaxLength != 200 {
		t.Fatalf("the fields land typed, got %+v", schema)
	}
	if err := json.Unmarshal([]byte(`{"type": "string", "widget": "spinner"}`), &schema); !errors.Is(err, sdb.ErrBadProfileItem) {
		t.Fatalf("a key no renderer reads is refused at decode, got %v", err)
	}
	cases := map[string]string{
		"a type outside the list":         `{"type": "money"}`,
		"a format outside the list":       `{"type": "string", "format": "hologram"}`,
		"a component outside the list":    `{"type": "string", "component": "hologram"}`,
		"a pattern that does not compile": `{"type": "string", "pattern": "("}`,
		"items that are not a schema":     `{"type": "array", "items": {"type": "money"}}`,
		"a default outside the schema":    `{"type": "integer", "default": "one"}`,
	}
	for name, raw := range cases {
		var s sdb.ProfileSchema
		if err := json.Unmarshal([]byte(raw), &s); err == nil {
			err = s.Validate("schema")
			if !errors.Is(err, sdb.ErrBadProfileItem) {
				t.Errorf("%s must be refused, got %v", name, err)
			}
		}
	}
}

func TestProfileValueIsCheckedAgainstItsSchema(t *testing.T) {
	item := func(schema, value string) sdb.ProfileItem {
		var s sdb.ProfileSchema
		if err := json.Unmarshal([]byte(schema), &s); err != nil {
			t.Fatalf("schema %s: %v", schema, err)
		}
		return sdb.ProfileItem{Schema: s, Value: json.RawMessage(value)}
	}
	good := []sdb.ProfileItem{
		item(`{"type": "string", "minLength": 1, "maxLength": 5}`, `"hello"`),
		item(`{"type": "string", "format": "uri"}`, `"https://example.no"`),
		item(`{"type": "integer", "minimum": 0, "maximum": 10}`, `7`),
		item(`{"type": "string", "enum": ["a", "b"]}`, `"b"`),
		item(`{"type": "array", "items": {"type": "number"}}`, `[1, 2.5]`),
		item(`{"type": "object", "properties": {"name": {"type": "string"}}, "required": ["name"]}`, `{"name": "x"}`),
		item(`{"type": "boolean", "component": "boolean"}`, `true`),
		item(`{"type": "null"}`, `null`),
	}
	for n, i := range good {
		if err := i.Validate(); err != nil {
			t.Errorf("case %d must pass, got %v", n, err)
		}
	}
	bad := []sdb.ProfileItem{
		item(`{"type": "string", "maxLength": 3}`, `"hello"`),
		item(`{"type": "string", "format": "image"}`, `"http://example.no/a.png"`),
		item(`{"type": "integer"}`, `7.5`),
		item(`{"type": "number", "maximum": 1}`, `2`),
		item(`{"type": "string", "enum": ["a"]}`, `"b"`),
		item(`{"type": "array", "items": {"type": "number"}}`, `[1, "two"]`),
		item(`{"type": "object", "required": ["name"]}`, `{}`),
		item(`{"type": "string", "pattern": "^[a-z]+$"}`, `"A1"`),
		item(`{"type": "boolean"}`, `"yes"`),
	}
	for n, i := range bad {
		if err := i.Validate(); !errors.Is(err, sdb.ErrBadProfileItem) {
			t.Errorf("case %d must be refused, got %v", n, err)
		}
	}
	if err := (sdb.ProfileItem{Schema: sdb.ProfileSchema{Type: "string"}}).Validate(); !errors.Is(err, sdb.ErrBadProfileItem) {
		t.Errorf("a missing value is refused, got %v", err)
	}
}
