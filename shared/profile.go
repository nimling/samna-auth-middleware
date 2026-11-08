package shared

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/nimling/samna-auth-middleware/v2/client"
)

var (
	ErrBadProfileItem = errors.New("bad profile item")
	ErrNoProphet      = errors.New("no prophet to send the profile item to")
)

var profileFieldPattern = regexp.MustCompile(`^[a-z][a-z0-9_.]{0,63}$`)

type ProfileItem struct {
	ID         uuid.UUID `bun:"id" json:"id"`
	AppID      uuid.UUID `bun:"app_id" json:"app_id"`
	AppSlug    string    `bun:"app_slug" json:"app_slug,omitempty"`
	AppName    string    `bun:"app_name" json:"app_name,omitempty"`
	UserID     uuid.UUID `bun:"user_id" json:"user_id"`
	ObjectType string    `bun:"object_type" json:"object_type"`
	ObjectID   uuid.UUID `bun:"object_id" json:"object_id"`
	Field      string    `bun:"field" json:"field"`
	Label      string    `bun:"label" json:"label"`
	Value      string    `bun:"value" json:"value"`
	Link       *string   `bun:"link" json:"link,omitempty"`
	Index      int       `bun:"index" json:"index"`
	CreatedAt  time.Time `bun:"sa_created_at" json:"created_at"`
	UpdatedAt  time.Time `bun:"sa_updated_at" json:"updated_at"`
}

type BodySamProfileItem struct {
	UserID     uuid.UUID `json:"user_id"`
	ObjectType string    `json:"object_type"`
	ObjectID   uuid.UUID `json:"object_id"`
	Field      string    `json:"field"`
	Label      string    `json:"label"`
	Value      string    `json:"value"`
	Link       *string   `json:"link,omitempty"`
	Index      int       `json:"index"`
}

type BodySamProfileItemKey struct {
	UserID     uuid.UUID `json:"user_id"`
	ObjectType string    `json:"object_type"`
	ObjectID   uuid.UUID `json:"object_id"`
	Field      *string   `json:"field,omitempty"`
}

func (b BodySamProfileItem) Validate() error {
	if err := (BodySamProfileItemKey{UserID: b.UserID, ObjectType: b.ObjectType, ObjectID: b.ObjectID, Field: &b.Field}).Validate(); err != nil {
		return err
	}
	if n := utf8.RuneCountInString(strings.TrimSpace(b.Label)); n == 0 || n > 80 {
		return fmt.Errorf("%w: label must be 1 to 80 characters", ErrBadProfileItem)
	}
	if n := utf8.RuneCountInString(strings.TrimSpace(b.Value)); n == 0 || n > 500 {
		return fmt.Errorf("%w: value must be 1 to 500 characters", ErrBadProfileItem)
	}
	if b.Link != nil && *b.Link != "" && (!strings.HasPrefix(*b.Link, "https://") || len(*b.Link) > 2048) {
		return fmt.Errorf("%w: link must be an https url of at most 2048 characters", ErrBadProfileItem)
	}
	return nil
}

func (b BodySamProfileItemKey) Validate() error {
	if b.UserID == uuid.Nil || b.ObjectID == uuid.Nil {
		return fmt.Errorf("%w: user_id and object_id are required", ErrBadProfileItem)
	}
	if n := len(b.ObjectType); n == 0 || n > 120 {
		return fmt.Errorf("%w: object_type is required", ErrBadProfileItem)
	}
	if b.Field != nil && !profileFieldPattern.MatchString(*b.Field) {
		return fmt.Errorf("%w: field must be lower case letters, digits, _ and . and start with a letter", ErrBadProfileItem)
	}
	return nil
}

func profileActor(ac *AccessClaim, userID uuid.UUID) error {
	if ac == nil || ac.UserID == uuid.Nil || ac.UserID != userID || ac.Access&ACCESS_READ == 0 {
		return ErrNoAccess
	}
	return nil
}

func (d *DB) PutProfileItem(ctx context.Context, ac *AccessClaim, item BodySamProfileItem) (*ProfileItem, error) {
	if err := profileActor(ac, item.UserID); err != nil {
		return nil, err
	}
	if err := item.Validate(); err != nil {
		return nil, err
	}
	if !d.RouteToProphet {
		return nil, ErrNoProphet
	}
	var out ProfileItem
	if err := client.Put(ctx, "profile", item, &out); err != nil {
		return nil, profileError(err)
	}
	return &out, nil
}

func (d *DB) DeleteProfileItem(ctx context.Context, ac *AccessClaim, key BodySamProfileItemKey) ([]ProfileItem, error) {
	if err := profileActor(ac, key.UserID); err != nil {
		return nil, err
	}
	if err := key.Validate(); err != nil {
		return nil, err
	}
	if !d.RouteToProphet {
		return nil, ErrNoProphet
	}
	out := make([]ProfileItem, 0)
	if err := client.Delete(ctx, "profile", key, &out); err != nil {
		return nil, profileError(err)
	}
	return out, nil
}

func (d *DB) GetProfileItems(ctx context.Context, userID uuid.UUID) ([]ProfileItem, error) {
	if !d.RouteToProphet {
		return nil, ErrNoProphet
	}
	out := make([]ProfileItem, 0)
	if err := client.Get(ctx, "profile?user_id="+userID.String(), &out); err != nil {
		return nil, profileError(err)
	}
	return out, nil
}

func profileError(err error) error {
	switch {
	case errors.Is(err, client.ErrNotFound):
		return fmt.Errorf("%w: %w", ErrNotFound, err)
	case errors.Is(err, client.ErrForbidden):
		return fmt.Errorf("%w: %w", ErrNoAccess, err)
	case errors.Is(err, client.ErrBadRequest):
		return fmt.Errorf("%w: %w", ErrBadProfileItem, err)
	}
	return err
}
