package sauth

import (
	"encoding/json"
	"fmt"
	"reflect"
)

func MarshalRows(model any) ([]json.RawMessage, error) {
	v := reflect.ValueOf(model)
	for v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface {
		if v.IsNil() {
			return nil, nil
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Slice {
		return nil, fmt.Errorf("sauth.MarshalRows: expected slice, got %s", v.Kind())
	}
	out := make([]json.RawMessage, 0, v.Len())
	for i := 0; i < v.Len(); i++ {
		raw, err := Marshal(v.Index(i).Interface())
		if err != nil {
			return nil, err
		}
		out = append(out, raw)
	}
	return out, nil
}
