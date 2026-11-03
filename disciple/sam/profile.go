package sam

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"

	"github.com/google/uuid"
	"github.com/nimling/samna-auth-middleware/v2/client"
	sdb "github.com/nimling/samna-auth-middleware/v2/disciple/sdb"
	"github.com/nimling/samna-auth-middleware/v2/shared"
)

var ErrNoProphet = errors.New("no prophet to send the profile item to")

func profileActor(ac *shared.AccessClaim, userID uuid.UUID) error {
	if ac == nil || ac.UserID == uuid.Nil || ac.UserID != userID || ac.Access&shared.ACCESS_READ == 0 {
		return shared.ErrNoAccess
	}
	return nil
}

func PutProfileItem(ctx context.Context, ac *shared.AccessClaim, item shared.BodySamProfileItem) (json.RawMessage, error) {
	if err := profileActor(ac, item.UserID); err != nil {
		return nil, err
	}
	if err := item.Validate(); err != nil {
		return nil, err
	}
	if !sdb.Instance().RouteToProphet {
		return nil, ErrNoProphet
	}
	var profile json.RawMessage
	if err := client.Put(ctx, "profile", item, &profile); err != nil {
		return nil, profileError(err)
	}
	return profile, nil
}

func DeleteProfileItem(ctx context.Context, ac *shared.AccessClaim, key shared.BodySamProfileItemKey) (json.RawMessage, error) {
	if err := profileActor(ac, key.UserID); err != nil {
		return nil, err
	}
	if err := key.Validate(); err != nil {
		return nil, err
	}
	if !sdb.Instance().RouteToProphet {
		return nil, ErrNoProphet
	}
	var profile json.RawMessage
	if err := client.Delete(ctx, "profile", key, &profile); err != nil {
		return nil, profileError(err)
	}
	return profile, nil
}

func profileError(err error) error {
	switch {
	case errors.Is(err, client.ErrNotFound):
		return fmt.Errorf("%w: %w", shared.ErrNotFound, err)
	case errors.Is(err, client.ErrForbidden):
		return fmt.Errorf("%w: %w", shared.ErrNoAccess, err)
	case errors.Is(err, client.ErrBadRequest):
		return fmt.Errorf("%w: %w", shared.ErrBadProfileItem, err)
	}
	return err
}

func GetUserNameAvailable(ctx context.Context, ac *shared.AccessClaim, userName string) (*shared.UserNameAvailable, error) {
	if ac == nil || ac.UserID == uuid.Nil {
		return nil, shared.ErrNoAccess
	}
	if err := (shared.BodySamUserName{UserID: ac.UserID, UserName: &userName}).Validate(); err != nil {
		return nil, err
	}
	if !sdb.Instance().RouteToProphet {
		return nil, ErrNoProphet
	}
	var out shared.UserNameAvailable
	if err := client.Get(ctx, "user_name?user_name="+url.QueryEscape(userName), &out); err != nil {
		return nil, userNameError(err)
	}
	return &out, nil
}

func PutUserName(ctx context.Context, ac *shared.AccessClaim, body shared.BodySamUserName) (*string, error) {
	if err := profileActor(ac, body.UserID); err != nil {
		return nil, err
	}
	if err := body.Validate(); err != nil {
		return nil, err
	}
	if !sdb.Instance().RouteToProphet {
		return nil, ErrNoProphet
	}
	var out shared.BodySamUserName
	if err := client.Put(ctx, "user_name", body, &out); err != nil {
		return nil, userNameError(err)
	}
	return out.UserName, nil
}

func userNameError(err error) error {
	switch {
	case errors.Is(err, client.ErrConflict):
		return fmt.Errorf("%w: %w", shared.ErrUserNameTaken, err)
	case errors.Is(err, client.ErrNotFound):
		return fmt.Errorf("%w: %w", shared.ErrNotFound, err)
	case errors.Is(err, client.ErrForbidden):
		return fmt.Errorf("%w: %w", shared.ErrNoAccess, err)
	case errors.Is(err, client.ErrBadRequest):
		return fmt.Errorf("%w: %w", shared.ErrBadUserName, err)
	}
	return err
}
