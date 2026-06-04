package sauth

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/google/uuid"
)

type model struct {
	ID         uuid.UUID  `json:"id"`
	Name       string     `json:"name"`
	AppID      uuid.UUID  `json:"-" sauth:"app_id"`
	Internal   string     `json:"-"`
	Hidden     string     `json:"-" sauth:"-"`
	Plain      string     `json:"plain,omitempty"`
	Ptr        *string    `json:"ptr,omitempty"`
	Override   string     `json:"local_name" sauth:"wire_name"`
	Count      int        `json:"count,omitempty"`
	Created    *uuid.UUID `json:"created,omitempty"`
	Untagged   string
	unexported string
}

func TestMap_OmitsSkippedFields(t *testing.T) {
	m, err := Map(model{Hidden: "h"})
	if err != nil {
		t.Fatalf("Map: %v", err)
	}
	if _, ok := m["Hidden"]; ok {
		t.Fatalf("sauth:\"-\" must skip the field, got %#v", m)
	}
	if _, ok := m["hidden"]; ok {
		t.Fatalf("sauth:\"-\" must skip the field, got %#v", m)
	}
}

func TestMap_FallsBackToJSONWhenNoSauthTag(t *testing.T) {
	m, err := Map(model{Name: "n"})
	if err != nil {
		t.Fatalf("Map: %v", err)
	}
	got, ok := m["name"]
	if !ok {
		t.Fatalf("expected name from json fallback, got %#v", m)
	}
	if got != "n" {
		t.Fatalf("expected n got %v", got)
	}
}

func TestMap_SauthTagOverridesJSONDash(t *testing.T) {
	appID := uuid.New()
	m, err := Map(model{AppID: appID})
	if err != nil {
		t.Fatalf("Map: %v", err)
	}
	if got, ok := m["app_id"]; !ok || got != appID {
		t.Fatalf("expected app_id=%s, got %#v", appID, m)
	}
	if _, ok := m["-"]; ok {
		t.Fatalf("json:\"-\" must not leak as key, got %#v", m)
	}
}

func TestMap_InternalJSONDashWithNoSauthIsDropped(t *testing.T) {
	m, err := Map(model{Internal: "secret"})
	if err != nil {
		t.Fatalf("Map: %v", err)
	}
	if _, ok := m["Internal"]; ok {
		t.Fatalf("json:\"-\" without sauth must skip, got %#v", m)
	}
}

func TestMap_OverrideUsesSauthNameNotJSONName(t *testing.T) {
	m, err := Map(model{Override: "x"})
	if err != nil {
		t.Fatalf("Map: %v", err)
	}
	if _, ok := m["local_name"]; ok {
		t.Fatalf("sauth wire_name must override json local_name, got %#v", m)
	}
	if got, ok := m["wire_name"]; !ok || got != "x" {
		t.Fatalf("expected wire_name=x, got %#v", m)
	}
}

func TestMap_OmitemptyDropsZeroValues(t *testing.T) {
	m, err := Map(model{})
	if err != nil {
		t.Fatalf("Map: %v", err)
	}
	if _, ok := m["plain"]; ok {
		t.Fatalf("omitempty string must skip, got %#v", m)
	}
	if _, ok := m["ptr"]; ok {
		t.Fatalf("omitempty pointer must skip, got %#v", m)
	}
	if _, ok := m["count"]; ok {
		t.Fatalf("omitempty int must skip, got %#v", m)
	}
	if _, ok := m["created"]; ok {
		t.Fatalf("omitempty uuid pointer must skip, got %#v", m)
	}
}

func TestMap_OmitemptyKeepsNonZeroValues(t *testing.T) {
	s := "p"
	m, err := Map(model{Plain: "x", Ptr: &s, Count: 7})
	if err != nil {
		t.Fatalf("Map: %v", err)
	}
	if m["plain"] != "x" {
		t.Fatalf("plain must be x, got %#v", m["plain"])
	}
	if m["ptr"] != &s {
		t.Fatalf("ptr must be set, got %#v", m["ptr"])
	}
	if m["count"] != 7 {
		t.Fatalf("count must be 7, got %#v", m["count"])
	}
}

func TestMap_UntaggedFieldUsesGoName(t *testing.T) {
	m, err := Map(model{Untagged: "u"})
	if err != nil {
		t.Fatalf("Map: %v", err)
	}
	if m["Untagged"] != "u" {
		t.Fatalf("expected Untagged=u, got %#v", m)
	}
}

func TestMap_SkipsUnexported(t *testing.T) {
	m, err := Map(model{unexported: "x"})
	if err != nil {
		t.Fatalf("Map: %v", err)
	}
	if _, ok := m["unexported"]; ok {
		t.Fatalf("unexported must be skipped, got %#v", m)
	}
}

func TestMap_AcceptsPointer(t *testing.T) {
	m, err := Map(&model{Name: "n"})
	if err != nil {
		t.Fatalf("Map: %v", err)
	}
	if m["name"] != "n" {
		t.Fatalf("expected name=n, got %#v", m)
	}
}

func TestMap_NilPointerReturnsEmptyMap(t *testing.T) {
	var m *model
	got, err := Map(m)
	if err != nil {
		t.Fatalf("Map: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected empty map, got %#v", got)
	}
}

func TestMap_NonStructReturnsError(t *testing.T) {
	_, err := Map(42)
	if err == nil {
		t.Fatalf("expected error for non-struct")
	}
}

func TestMarshal_ProducesValidJSON(t *testing.T) {
	appID := uuid.New()
	data, err := Marshal(model{ID: uuid.New(), Name: "n", AppID: appID})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if raw["app_id"] != appID.String() {
		t.Fatalf("app_id should be %s in JSON, got %v", appID, raw["app_id"])
	}
}

func TestUnmarshal_PopulatesFieldsBySauthName(t *testing.T) {
	appID := uuid.New()
	raw := map[string]any{
		"id":     uuid.New().String(),
		"name":   "n",
		"app_id": appID.String(),
	}
	data, _ := json.Marshal(raw)
	var m model
	if err := Unmarshal(data, &m); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if m.AppID != appID {
		t.Fatalf("AppID should be %s, got %s", appID, m.AppID)
	}
	if m.Name != "n" {
		t.Fatalf("Name should be n, got %s", m.Name)
	}
}

func TestUnmarshal_HonorsSauthOverride(t *testing.T) {
	raw := map[string]any{
		"wire_name": "wire",
	}
	data, _ := json.Marshal(raw)
	var m model
	if err := Unmarshal(data, &m); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if m.Override != "wire" {
		t.Fatalf("Override should be wire, got %s", m.Override)
	}
}

func TestUnmarshal_SkipsDashedFields(t *testing.T) {
	raw := map[string]any{
		"Hidden": "x",
	}
	data, _ := json.Marshal(raw)
	var m model
	if err := Unmarshal(data, &m); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if m.Hidden != "" {
		t.Fatalf("Hidden should stay empty, got %s", m.Hidden)
	}
}

func TestUnmarshal_NilPointerErrors(t *testing.T) {
	if err := Unmarshal([]byte(`{}`), nil); err == nil {
		t.Fatalf("expected error for nil pointer")
	}
	var m *model
	if err := Unmarshal([]byte(`{}`), m); err == nil {
		t.Fatalf("expected error for typed nil pointer")
	}
}

func TestUnmarshal_NonStructErrors(t *testing.T) {
	x := 5
	if err := Unmarshal([]byte(`{}`), &x); err == nil {
		t.Fatalf("expected error for non-struct pointer")
	}
}

func TestRoundTrip_PreservesValues(t *testing.T) {
	in := model{
		ID:       uuid.New(),
		Name:     "n",
		AppID:    uuid.New(),
		Override: "wire",
		Untagged: "u",
		Count:    9,
	}
	data, err := Marshal(in)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var out model
	if err := Unmarshal(data, &out); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if !reflect.DeepEqual(in.ID, out.ID) {
		t.Fatalf("ID mismatch: in=%s out=%s", in.ID, out.ID)
	}
	if in.Name != out.Name {
		t.Fatalf("Name mismatch: in=%s out=%s", in.Name, out.Name)
	}
	if in.AppID != out.AppID {
		t.Fatalf("AppID mismatch: in=%s out=%s", in.AppID, out.AppID)
	}
	if in.Override != out.Override {
		t.Fatalf("Override mismatch: in=%s out=%s", in.Override, out.Override)
	}
	if in.Untagged != out.Untagged {
		t.Fatalf("Untagged mismatch: in=%s out=%s", in.Untagged, out.Untagged)
	}
	if in.Count != out.Count {
		t.Fatalf("Count mismatch: in=%d out=%d", in.Count, out.Count)
	}
}

type typedNullable struct {
	Name *string `json:"name,omitempty"`
}

func TestMap_StarStringNilOmitted(t *testing.T) {
	m, err := Map(typedNullable{})
	if err != nil {
		t.Fatalf("Map: %v", err)
	}
	if _, ok := m["name"]; ok {
		t.Fatalf("nil *string must be omitted, got %#v", m)
	}
}

func TestMap_StarStringNotNilKept(t *testing.T) {
	s := "x"
	m, err := Map(typedNullable{Name: &s})
	if err != nil {
		t.Fatalf("Map: %v", err)
	}
	if m["name"] != &s {
		t.Fatalf("non-nil *string must be present, got %#v", m)
	}
}
