package db

import (
	"reflect"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/nimling/nimbody"
)

func hasAnyJSONKey(model any) bool {
	bv := validatorOf(model)
	if bv == nil {
		return false
	}
	found := false
	walkJSONFields(model, func(jsonName string, _ reflect.StructField, _ reflect.Value) bool {
		if bv.HasKey(jsonName) {
			found = true
			return true
		}
		return false
	})
	return found
}

func bunNullColumns(model any) []string {
	bv := validatorOf(model)
	if bv == nil {
		return nil
	}
	var nulls []string
	walkJSONFields(model, func(jsonName string, f reflect.StructField, v reflect.Value) bool {
		if f.Type.Kind() != reflect.Ptr {
			return false
		}
		if !bv.HasKey(jsonName) {
			return false
		}
		if !v.IsNil() {
			return false
		}
		column := firstTagPart(f.Tag.Get("bun"))
		if column == "" || column == "-" {
			return false
		}
		nulls = append(nulls, column)
		return false
	})
	return nulls
}

func walkJSONFields(model any, fn func(string, reflect.StructField, reflect.Value) bool) {
	v := reflect.ValueOf(model)
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return
	}
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		jsonName := firstTagPart(f.Tag.Get("json"))
		if jsonName == "" || jsonName == "-" {
			continue
		}
		if fn(jsonName, f, v.Field(i)) {
			return
		}
	}
}

func firstTagPart(tag string) string {
	if comma := strings.IndexByte(tag, ','); comma >= 0 {
		return tag[:comma]
	}
	return tag
}

func validatorOf(model any) *nimbody.BodyValidator {
	type validator interface {
		Validator() *nimbody.BodyValidator
	}
	if v, ok := model.(validator); ok {
		return v.Validator()
	}
	return nil
}

func (u *SamnaUser) Bind(ctx echo.Context) error {
	return u.Body.Bound(ctx, u)
}

func (u *SamnaUser) PatchNulls() []string {
	return bunNullColumns(u)
}

func (o *Organization) Bind(ctx echo.Context) error {
	return o.Body.Bound(ctx, o)
}

func (o *Organization) PatchNulls() []string {
	return bunNullColumns(o)
}

func (l *Location) Bind(ctx echo.Context) error {
	return l.Body.Bound(ctx, l)
}

func (l *Location) PatchNulls() []string {
	return bunNullColumns(l)
}

func (c *Claim) Bind(ctx echo.Context) error {
	return c.Body.Bound(ctx, c)
}

func (c *Claim) PatchNulls() []string {
	return bunNullColumns(c)
}

func (a *Asset) Bind(ctx echo.Context) error {
	return a.Body.Bound(ctx, a)
}

func (a *Asset) PatchNulls() []string {
	return bunNullColumns(a)
}

func (s *SamnaSecret) Bind(ctx echo.Context) error {
	return s.Body.Bound(ctx, s)
}

func (s *SamnaSecret) PatchNulls() []string {
	return bunNullColumns(s)
}

func (f *ObjectField) Bind(ctx echo.Context) error {
	return f.Body.Bound(ctx, f)
}

func (f *ObjectField) PatchNulls() []string {
	return bunNullColumns(f)
}

func (f *UserField) Bind(ctx echo.Context) error {
	return f.Body.Bound(ctx, f)
}

func (f *UserField) PatchNulls() []string {
	return bunNullColumns(f)
}

func (r *UserRelation) Bind(ctx echo.Context) error {
	return r.Body.Bound(ctx, r)
}

func (r *UserRelation) PatchNulls() []string {
	return bunNullColumns(r)
}
