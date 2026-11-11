package sauth

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"github.com/google/uuid"
)

var uuidType = reflect.TypeOf(uuid.UUID{})

func isNilUUID(v reflect.Value) bool {
	for v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return false
		}
		v = v.Elem()
	}
	return v.Type() == uuidType && v.Interface().(uuid.UUID) == uuid.Nil
}

func tagName(tag string) (name string, omitEmpty bool, skip bool) {
	tag = strings.TrimSpace(tag)
	if tag == "" {
		return "", false, false
	}
	parts := strings.Split(tag, ",")
	name = strings.TrimSpace(parts[0])
	if name == "-" {
		return "", false, true
	}
	for _, p := range parts[1:] {
		switch strings.TrimSpace(p) {
		case "omitempty", "omitzero":
			omitEmpty = true
		}
	}
	return name, omitEmpty, false
}

func isEmpty(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface:
		return v.IsNil()
	case reflect.Slice, reflect.Map, reflect.Array, reflect.String:
		return v.Len() == 0
	case reflect.Bool:
		return !v.Bool()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return v.Int() == 0
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return v.Uint() == 0
	case reflect.Float32, reflect.Float64:
		return v.Float() == 0
	case reflect.Struct:
		if v.Type().Comparable() {
			zero := reflect.Zero(v.Type()).Interface()
			return reflect.DeepEqual(v.Interface(), zero)
		}
	}
	return false
}

func Map(model any) (map[string]any, error) {
	b, err := json.Marshal(model)
	if err != nil {
		return nil, fmt.Errorf("sauth.Map marshal: %w", err)
	}
	out := map[string]any{}
	if len(b) > 0 && string(b) != "null" {
		if err := json.Unmarshal(b, &out); err != nil {
			return nil, fmt.Errorf("sauth.Map decode: %w", err)
		}
	}
	overlayMap(out, reflect.ValueOf(model))
	return out, nil
}

func overlayMap(out map[string]any, v reflect.Value) {
	for v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface {
		if v.IsNil() {
			return
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return
	}
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if !field.IsExported() {
			continue
		}
		jsonName, _, _ := tagName(field.Tag.Get("json"))
		sauthTag := strings.TrimSpace(field.Tag.Get("sauth"))
		if isNilUUID(v.Field(i)) {
			name := jsonName
			if name == "" {
				name = field.Name
			}
			delete(out, name)
			continue
		}
		if field.Anonymous && sauthTag == "" && jsonName == "" {
			overlayMap(out, v.Field(i))
			continue
		}
		if sauthTag == "" {
			continue
		}
		sauthName, omit, skip := tagName(sauthTag)
		if skip {
			continue
		}
		val := v.Field(i)
		if omit && isEmpty(val) {
			continue
		}
		if jsonName != "" && jsonName != sauthName {
			delete(out, jsonName)
		}
		out[sauthName] = val.Interface()
	}
}

func Marshal(model any) ([]byte, error) {
	m, err := Map(model)
	if err != nil {
		return nil, err
	}
	return json.Marshal(m)
}

func Unmarshal(data []byte, model any) error {
	if err := json.Unmarshal(data, model); err != nil {
		return fmt.Errorf("sauth.Unmarshal: %w", err)
	}
	raw := map[string]json.RawMessage{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("sauth.Unmarshal envelope: %w", err)
	}
	v := reflect.ValueOf(model)
	if v.Kind() != reflect.Pointer || v.IsNil() {
		return fmt.Errorf("sauth.Unmarshal: expected non-nil pointer")
	}
	overlayUnmarshal(raw, v.Elem())
	return nil
}

func overlayUnmarshal(raw map[string]json.RawMessage, v reflect.Value) error {
	for v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return nil
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return nil
	}
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if !field.IsExported() {
			continue
		}
		bunTag := field.Tag.Get("bun")
		if field.Anonymous && bunTag == "" {
			if err := overlayUnmarshal(raw, v.Field(i)); err != nil {
				return err
			}
			continue
		}
		col, _, skip := tagName(bunTag)
		if skip || col == "" || col == "-" || strings.Contains(bunTag, "rel:") {
			continue
		}
		msg, ok := raw[col]
		if !ok {
			continue
		}
		fv := v.Field(i)
		if !fv.CanAddr() {
			continue
		}
		if err := json.Unmarshal(msg, fv.Addr().Interface()); err != nil {
			return fmt.Errorf("sauth.Unmarshal %s: %w", col, err)
		}
	}
	return nil
}
