package echo

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strings"
)

func StripObjectSchema(qualified string) string {
	if dot := strings.IndexByte(qualified, '.'); dot >= 0 {
		return qualified[dot+1:]
	}
	return qualified
}

var bareObjectTypes = map[string]ObjectType{
	"samna_user":   OBJECT_TYPE_USER,
	"user":         OBJECT_TYPE_USER,
	"usr":          OBJECT_TYPE_USER,
	"organization": OBJECT_TYPE_ORGANIZATION,
	"org":          OBJECT_TYPE_ORGANIZATION,
	"location":     OBJECT_TYPE_LOCATION,
	"loc":          OBJECT_TYPE_LOCATION,
	"samna_client": OBJECT_TYPE_CLIENT,
	"client":       OBJECT_TYPE_CLIENT,
	"samna_app":    OBJECT_TYPE_APP,
	"app":          OBJECT_TYPE_APP,
	"claim":        OBJECT_TYPE_CLAIM,
	"samna_secret": OBJECT_TYPE_SECRET,
	"secret":       OBJECT_TYPE_SECRET,
	"audit":        OBJECT_TYPE_AUDIT,
	"code":         OBJECT_TYPE_CODE,
	"asset":        OBJECT_TYPE_ASSET,
	"provider":     OBJECT_TYPE_PROVIDER,
	"module":       OBJECT_TYPE_MODULE,
}

var qualifiedObjectTypes = map[ObjectType]struct{}{
	OBJECT_TYPE_USER:         {},
	OBJECT_TYPE_ORGANIZATION: {},
	OBJECT_TYPE_LOCATION:     {},
	OBJECT_TYPE_CLIENT:       {},
	OBJECT_TYPE_APP:          {},
	OBJECT_TYPE_CLAIM:        {},
	OBJECT_TYPE_SECRET:       {},
	OBJECT_TYPE_AUDIT:        {},
	OBJECT_TYPE_CODE:         {},
	OBJECT_TYPE_ASSET:        {},
	OBJECT_TYPE_PROVIDER:     {},
	OBJECT_TYPE_MODULE:       {},
}

func ResolveObjectType(s string) (ObjectType, error) {
	if s == "" {
		return "", fmt.Errorf("empty object_type")
	}
	if _, ok := qualifiedObjectTypes[ObjectType(s)]; ok {
		return ObjectType(s), nil
	}
	if q, ok := bareObjectTypes[s]; ok {
		return q, nil
	}
	if q, ok := bareObjectTypes[StripObjectSchema(s)]; ok {
		return q, nil
	}
	return "", fmt.Errorf("unknown object_type: %q", s)
}

func (o *ObjectType) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	qualified, err := ResolveObjectType(s)
	if err != nil {
		return err
	}
	*o = qualified
	return nil
}

func (o ObjectType) MarshalJSON() ([]byte, error) {
	return json.Marshal(StripObjectSchema(string(o)))
}

func (o ObjectType) Value() (driver.Value, error) {
	if o == "" {
		return nil, nil
	}
	return string(o), nil
}
