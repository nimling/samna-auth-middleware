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
	if got, ok := m["name"]; !ok || got != "n" {
		t.Fatalf("expected name=n from json fallback, got %#v", m)
	}
}

func TestMap_SauthTagSurfacesJSONHiddenField(t *testing.T) {
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
	for _, k := range []string{"plain", "ptr", "count", "created"} {
		if _, ok := m[k]; ok {
			t.Fatalf("omitempty %s must skip, got %#v", k, m)
		}
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
	if m["ptr"] != "p" {
		t.Fatalf("ptr must decode to p, got %#v", m["ptr"])
	}
	if v, ok := m["count"].(float64); !ok || v != 7 {
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
	if _, err := Map(make(chan int)); err == nil {
		t.Fatalf("expected error for unmarshalable value")
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
	data, _ := json.Marshal(map[string]any{"wire_name": "wire"})
	var m model
	if err := Unmarshal(data, &m); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if m.Override != "wire" {
		t.Fatalf("Override should be wire, got %s", m.Override)
	}
}

func TestUnmarshal_SkipsDashedFields(t *testing.T) {
	data, _ := json.Marshal(map[string]any{"Hidden": "x"})
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
		t.Fatalf("expected error decoding object into non-struct pointer")
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
	if m["name"] != "x" {
		t.Fatalf("non-nil *string must decode to x, got %#v", m["name"])
	}
}

type emptyKinds struct {
	Slice []string       `json:"slice,omitempty"`
	Map   map[string]int `json:"map,omitempty"`
	Array [2]int         `json:"array,omitempty"`
	Bool  bool           `json:"bool,omitempty"`
	Uint  uint64         `json:"uint,omitempty"`
	Float float64        `json:"float,omitempty"`
	Iface any            `json:"iface,omitempty"`
}

func TestMap_OmitemptySliceMapBoolUintFloat(t *testing.T) {
	m, err := Map(emptyKinds{})
	if err != nil {
		t.Fatalf("Map: %v", err)
	}
	for _, k := range []string{"slice", "map", "bool", "uint", "float", "iface"} {
		if _, ok := m[k]; ok {
			t.Fatalf("zero %s must be omitted, got %#v", k, m)
		}
	}
}

func TestMap_KeepsNonZeroSliceMapBoolUintFloat(t *testing.T) {
	m, err := Map(emptyKinds{
		Slice: []string{"a"},
		Map:   map[string]int{"a": 1},
		Bool:  true,
		Uint:  3,
		Float: 1.5,
		Iface: "x",
	})
	if err != nil {
		t.Fatalf("Map: %v", err)
	}
	for _, k := range []string{"slice", "map", "bool", "uint", "float", "iface"} {
		if _, ok := m[k]; !ok {
			t.Fatalf("non-zero %s must be kept, got %#v", k, m)
		}
	}
}

type tagEdge struct {
	A string `json:",omitempty"`
	B string `sauth:",omitempty"`
}

func TestFieldKey_EmptyJSONNameFallsBackToGoFieldName(t *testing.T) {
	m, err := Map(tagEdge{A: "a", B: "b"})
	if err != nil {
		t.Fatalf("Map: %v", err)
	}
	if m["A"] != "a" {
		t.Fatalf("json tag with empty name must use Go field name A, got %#v", m)
	}
	if m["B"] != "b" {
		t.Fatalf("field B must serialize under its Go field name, got %#v", m)
	}
}

func TestMarshal_PropagatesMapError(t *testing.T) {
	if _, err := Marshal(make(chan int)); err == nil {
		t.Fatalf("Marshal of unmarshalable value must error")
	}
}

func TestUnmarshal_BadEnvelopeErrors(t *testing.T) {
	var m model
	if err := Unmarshal([]byte("not json"), &m); err == nil {
		t.Fatalf("Unmarshal must error on bad envelope")
	}
}

func TestUnmarshal_PerFieldDecodeErrorPropagates(t *testing.T) {
	var m model
	if err := Unmarshal([]byte(`{"count": "not-an-int"}`), &m); err == nil {
		t.Fatalf("Unmarshal must propagate per-field decode error")
	}
}
