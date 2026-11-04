package shared

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
)

var (
	ErrBadProfileItem = errors.New("bad profile item")
	ErrBadUserName    = errors.New("bad user name")
	ErrUserNameTaken  = errors.New("user name taken")
)

var (
	profileFieldPattern = regexp.MustCompile(`^[a-z][a-z0-9_.]{0,63}$`)
	userNamePattern     = regexp.MustCompile(`^[a-z][a-z0-9_-]{2,31}$`)
	profileTypes        = []string{"string", "number", "integer", "boolean", "array", "object", "null"}
	profileFormats      = []string{"multiline", "markdown", "uri", "email", "phone", "date", "time", "datetime", "image", "color", "country", "currency", "percent", "duration"}
	profileComponents   = []string{"text", "textarea", "markdown", "link", "badge", "chip", "tag", "list", "image", "avatar", "progress", "rating", "table", "keyvalue", "date", "boolean", "number", "card", "hidden"}
)

type ProfileSchema struct {
	Type        string                   `json:"type"`
	Title       string                   `json:"title,omitempty"`
	Description string                   `json:"description,omitempty"`
	Format      string                   `json:"format,omitempty"`
	Component   string                   `json:"component,omitempty"`
	Icon        string                   `json:"icon,omitempty"`
	Order       *int                     `json:"order,omitempty"`
	Hidden      bool                     `json:"hidden,omitempty"`
	Enum        []json.RawMessage        `json:"enum,omitempty"`
	Default     json.RawMessage          `json:"default,omitempty"`
	Minimum     *float64                 `json:"minimum,omitempty"`
	Maximum     *float64                 `json:"maximum,omitempty"`
	MinLength   *int                     `json:"minLength,omitempty"`
	MaxLength   *int                     `json:"maxLength,omitempty"`
	Pattern     string                   `json:"pattern,omitempty"`
	Link        string                   `json:"link,omitempty"`
	Items       *ProfileSchema           `json:"items,omitempty"`
	Properties  map[string]ProfileSchema `json:"properties,omitempty"`
	Required    []string                 `json:"required,omitempty"`
}

type ProfileItem struct {
	Schema ProfileSchema   `json:"schema"`
	Value  json.RawMessage `json:"value"`
}

type Profile map[string]ProfileItem

type BodySamProfileItem struct {
	UserID uuid.UUID       `json:"user_id"`
	Field  string          `json:"field"`
	Schema ProfileSchema   `json:"schema"`
	Value  json.RawMessage `json:"value"`
}

type BodySamProfileItemKey struct {
	UserID uuid.UUID `json:"user_id"`
	Field  *string   `json:"field,omitempty"`
}

type BodySamUserName struct {
	UserID   uuid.UUID `json:"user_id"`
	UserName *string   `json:"user_name"`
}

type UserNameAvailable struct {
	UserName  string `json:"user_name"`
	Available bool   `json:"available"`
}

func (s *ProfileSchema) UnmarshalJSON(data []byte) error {
	type plain ProfileSchema
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var out plain
	if err := decoder.Decode(&out); err != nil {
		return fmt.Errorf("%w: schema: %v", ErrBadProfileItem, err)
	}
	*s = ProfileSchema(out)
	return nil
}

func (b BodySamProfileItem) Item() ProfileItem {
	return ProfileItem{Schema: b.Schema, Value: b.Value}
}

func (i ProfileItem) Validate() error {
	if err := i.Schema.Validate("schema"); err != nil {
		return err
	}
	var value any
	if len(i.Value) == 0 || json.Unmarshal(i.Value, &value) != nil {
		return fmt.Errorf("%w: value must be JSON", ErrBadProfileItem)
	}
	return i.Schema.Check(value, "value")
}

func (s ProfileSchema) Validate(at string) error {
	if !contains(profileTypes, s.Type) {
		return fmt.Errorf("%w: %s type must be one of %s", ErrBadProfileItem, at, strings.Join(profileTypes, ", "))
	}
	if s.Format != "" && !contains(profileFormats, s.Format) {
		return fmt.Errorf("%w: %s format must be one of %s", ErrBadProfileItem, at, strings.Join(profileFormats, ", "))
	}
	if s.Component != "" && !contains(profileComponents, s.Component) {
		return fmt.Errorf("%w: %s component must be one of %s", ErrBadProfileItem, at, strings.Join(profileComponents, ", "))
	}
	if s.Pattern != "" {
		if _, err := regexp.Compile(s.Pattern); err != nil {
			return fmt.Errorf("%w: %s pattern does not compile", ErrBadProfileItem, at)
		}
	}
	if s.Items != nil {
		if err := s.Items.Validate(at + ".items"); err != nil {
			return err
		}
	}
	for name, property := range s.Properties {
		if err := property.Validate(at + "." + name); err != nil {
			return err
		}
	}
	if len(s.Default) > 0 {
		var value any
		if json.Unmarshal(s.Default, &value) != nil {
			return fmt.Errorf("%w: %s default must be JSON", ErrBadProfileItem, at)
		}
		if err := s.Check(value, at+".default"); err != nil {
			return err
		}
	}
	return nil
}

func (s ProfileSchema) Check(value any, at string) error {
	switch s.Type {
	case "string":
		str, ok := value.(string)
		if !ok {
			return fmt.Errorf("%w: %s must be a string", ErrBadProfileItem, at)
		}
		length := utf8.RuneCountInString(str)
		if s.MinLength != nil && length < *s.MinLength {
			return fmt.Errorf("%w: %s is shorter than minLength", ErrBadProfileItem, at)
		}
		if s.MaxLength != nil && length > *s.MaxLength {
			return fmt.Errorf("%w: %s is longer than maxLength", ErrBadProfileItem, at)
		}
		if s.Pattern != "" {
			if matched, _ := regexp.MatchString(s.Pattern, str); !matched {
				return fmt.Errorf("%w: %s does not match its pattern", ErrBadProfileItem, at)
			}
		}
		if (s.Format == "uri" || s.Format == "image") && !strings.HasPrefix(str, "https://") {
			return fmt.Errorf("%w: %s must be an https url", ErrBadProfileItem, at)
		}
	case "number", "integer":
		f, ok := value.(float64)
		if !ok {
			return fmt.Errorf("%w: %s must be a %s", ErrBadProfileItem, at, s.Type)
		}
		if s.Type == "integer" && f != math.Trunc(f) {
			return fmt.Errorf("%w: %s must be an integer", ErrBadProfileItem, at)
		}
		if s.Minimum != nil && f < *s.Minimum {
			return fmt.Errorf("%w: %s is below minimum", ErrBadProfileItem, at)
		}
		if s.Maximum != nil && f > *s.Maximum {
			return fmt.Errorf("%w: %s is above maximum", ErrBadProfileItem, at)
		}
	case "boolean":
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("%w: %s must be a boolean", ErrBadProfileItem, at)
		}
	case "null":
		if value != nil {
			return fmt.Errorf("%w: %s must be null", ErrBadProfileItem, at)
		}
	case "array":
		list, ok := value.([]any)
		if !ok {
			return fmt.Errorf("%w: %s must be an array", ErrBadProfileItem, at)
		}
		if s.Items != nil {
			for n, element := range list {
				if err := s.Items.Check(element, fmt.Sprintf("%s[%d]", at, n)); err != nil {
					return err
				}
			}
		}
	case "object":
		object, ok := value.(map[string]any)
		if !ok {
			return fmt.Errorf("%w: %s must be an object", ErrBadProfileItem, at)
		}
		for name, property := range s.Properties {
			if field, present := object[name]; present {
				if err := property.Check(field, at+"."+name); err != nil {
					return err
				}
			}
		}
		for _, name := range s.Required {
			if _, present := object[name]; !present {
				return fmt.Errorf("%w: %s lacks %s", ErrBadProfileItem, at, name)
			}
		}
	}
	if len(s.Enum) > 0 {
		got, _ := json.Marshal(value)
		found := false
		for _, candidate := range s.Enum {
			var want any
			if json.Unmarshal(candidate, &want) != nil {
				continue
			}
			canonical, _ := json.Marshal(want)
			if string(canonical) == string(got) {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("%w: %s is not one of its enum", ErrBadProfileItem, at)
		}
	}
	return nil
}

func contains(list []string, s string) bool {
	for _, candidate := range list {
		if candidate == s {
			return true
		}
	}
	return false
}

func (b BodySamProfileItem) Validate() error {
	if err := (BodySamProfileItemKey{UserID: b.UserID, Field: &b.Field}).Validate(); err != nil {
		return err
	}
	return b.Item().Validate()
}

func (b BodySamProfileItemKey) Validate() error {
	if b.UserID == uuid.Nil {
		return fmt.Errorf("%w: user_id is required", ErrBadProfileItem)
	}
	if b.Field != nil && !profileFieldPattern.MatchString(*b.Field) {
		return fmt.Errorf("%w: field must be lower case letters, digits, _ and . and start with a letter", ErrBadProfileItem)
	}
	return nil
}

func validateUserName(userName string) error {
	if !userNamePattern.MatchString(userName) {
		return fmt.Errorf("%w: user_name must be 3 to 32 lower case letters, digits, _ and - and start with a letter", ErrBadUserName)
	}
	return nil
}

func (b BodySamUserName) Validate() error {
	if b.UserID == uuid.Nil {
		return fmt.Errorf("%w: user_id is required", ErrBadUserName)
	}
	if b.UserName != nil {
		return validateUserName(*b.UserName)
	}
	return nil
}
