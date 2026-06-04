package sauth

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
)

func fieldKey(field reflect.StructField) (name string, omitEmpty bool, skip bool) {
	tag := strings.TrimSpace(field.Tag.Get("sauth"))
	if tag == "" {
		tag = strings.TrimSpace(field.Tag.Get("json"))
	}
	if tag == "" {
		return field.Name, false, false
	}
	parts := strings.Split(tag, ",")
	first := strings.TrimSpace(parts[0])
	if first == "-" {
		return "", false, true
	}
	name = first
	if name == "" {
		name = field.Name
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
	v := reflect.ValueOf(model)
	for v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface {
		if v.IsNil() {
			return map[string]any{}, nil
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return nil, fmt.Errorf("sauth.Map: expected struct, got %s", v.Kind())
	}
	out := map[string]any{}
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if !field.IsExported() {
			continue
		}
		name, omitEmpty, skip := fieldKey(field)
		if skip {
			continue
		}
		val := v.Field(i)
		if omitEmpty && isEmpty(val) {
			continue
		}
		out[name] = val.Interface()
	}
	return out, nil
}

func Marshal(model any) ([]byte, error) {
	m, err := Map(model)
	if err != nil {
		return nil, err
	}
	return json.Marshal(m)
}

func Unmarshal(data []byte, model any) error {
	raw := map[string]json.RawMessage{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("sauth.Unmarshal envelope: %w", err)
	}
	v := reflect.ValueOf(model)
	if v.Kind() != reflect.Pointer || v.IsNil() {
		return fmt.Errorf("sauth.Unmarshal: expected non-nil pointer")
	}
	v = v.Elem()
	if v.Kind() != reflect.Struct {
		return fmt.Errorf("sauth.Unmarshal: expected struct, got %s", v.Kind())
	}
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if !field.IsExported() {
			continue
		}
		name, _, skip := fieldKey(field)
		if skip {
			continue
		}
		msg, ok := raw[name]
		if !ok {
			continue
		}
		if err := json.Unmarshal(msg, v.Field(i).Addr().Interface()); err != nil {
			return fmt.Errorf("sauth.Unmarshal %s: %w", name, err)
		}
	}
	return nil
}
