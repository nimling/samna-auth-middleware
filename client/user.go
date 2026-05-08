package client

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/nimling/samna-auth-middleware/db"
)

func GetUserAudit(ctx context.Context, userID uuid.UUID) (io.ReadCloser, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "user", userID.String(), "audit")
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	return c.client.Get(ctx, u)
}

func GetUserAssets(ctx context.Context, userID uuid.UUID) (io.ReadCloser, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "user", userID.String(), "asset")
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	return c.client.Get(ctx, u)
}

func GetUsers(ctx context.Context) (io.ReadCloser, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "user")
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	return c.client.Get(ctx, u)
}

func GetUser(ctx context.Context, userID uuid.UUID) (io.ReadCloser, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "user", userID.String())
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	return c.client.Get(ctx, u)
}

func GetUserClaims(ctx context.Context, userID uuid.UUID) (io.ReadCloser, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "user", userID.String(), "claim")
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	return c.client.Get(ctx, u)
}

func GetUserConnections(ctx context.Context, userID uuid.UUID) (io.ReadCloser, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "user", userID.String(), "user")
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	return c.client.Get(ctx, u)
}

func GetUserOrganizations(ctx context.Context, userID uuid.UUID) (io.ReadCloser, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "user", userID.String(), "organization")
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	return c.client.Get(ctx, u)
}

func GetUserLocations(ctx context.Context, userID uuid.UUID) (io.ReadCloser, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "user", userID.String(), "location")
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	return c.client.Get(ctx, u)
}

func PostUser(ctx context.Context, userID uuid.UUID, user db.SamnaUser) (db.SamnaUser, error) {
	if getDebugUserID() != nil {
		app, err := db.Instance().GetApp(ctx)
		if err != nil {
			return user, err
		}
		user.AppID = app.ID
		user.UserID = userID
		_, err = db.Instance().NewInsert().Model(&user).Returning("*").Exec(ctx)
		if err != nil {
			return user, fmt.Errorf("debug insert user: %w", err)
		}
		return user, nil
	}
	c := newClient()
	u, err := joinURL(c.apiURL, "user")
	if err != nil {
		return db.SamnaUser{}, fmt.Errorf("build url: %w", err)
	}
	user.UserID = userID
	result, err := DecodeJSON[db.SamnaUser](c.client.Post(ctx, requestBody(user, true), u))
	if err != nil {
		return result, err
	}
	_, err = db.Instance().NewInsert().Model(&result).On("CONFLICT (id) DO UPDATE").Set("sa_updated_at = EXCLUDED.sa_updated_at").Set("sa_deleted_at = EXCLUDED.sa_deleted_at").Exec(ctx)
	if err != nil {
		return result, err
	}
	return result, nil
}

func PutUser(ctx context.Context, userID uuid.UUID, user db.SamnaUser) (db.SamnaUser, error) {
	if getDebugUserID() != nil {
		user.UpdatedAt = time.Now()
		_, err := db.Instance().NewUpdate().Model(&user).Where("user_id = ?", userID).OmitZero().Exec(ctx)
		if err != nil {
			return user, fmt.Errorf("debug update user: %w", err)
		}
		return user, nil
	}
	c := newClient()
	u, err := joinURL(c.apiURL, "user", userID.String())
	if err != nil {
		return db.SamnaUser{}, fmt.Errorf("build url: %w", err)
	}
	result, err := DecodeJSON[db.SamnaUser](c.client.Put(ctx, requestBody(user, false), u))
	if err != nil {
		return result, err
	}
	_, err = db.Instance().NewInsert().Model(&result).On("CONFLICT (id) DO UPDATE").Set("sa_updated_at = EXCLUDED.sa_updated_at").Set("sa_deleted_at = EXCLUDED.sa_deleted_at").Exec(ctx)
	if err != nil {
		return result, err
	}
	return result, nil
}

func DeleteUser(ctx context.Context, userID uuid.UUID) (db.SamnaUser, error) {
	if getDebugUserID() != nil {
		var result db.SamnaUser
		_, err := db.Instance().NewUpdate().Model(&result).Set("sa_deleted_at = NOW()").Where("user_id = ?", userID).Where("sa_deleted_at IS NULL").Returning("*").Exec(ctx)
		if err != nil {
			return result, fmt.Errorf("debug delete user: %w", err)
		}
		return result, nil
	}
	c := newClient()
	u, err := joinURL(c.apiURL, "user", userID.String())
	if err != nil {
		return db.SamnaUser{}, fmt.Errorf("build url: %w", err)
	}
	result, err := DecodeJSON[db.SamnaUser](c.client.Delete(ctx, u))
	if err != nil {
		return result, err
	}
	_, err = db.Instance().NewInsert().Model(&result).On("CONFLICT (id) DO UPDATE").Set("sa_updated_at = EXCLUDED.sa_updated_at").Set("sa_deleted_at = EXCLUDED.sa_deleted_at").Exec(ctx)
	if err != nil {
		return result, err
	}
	return result, nil
}

func PostUserToken(ctx context.Context, payload db.SamnaUserToken) (db.SamnaUser, error) {
	if getDebugUserID() != nil {
		return postUserTokenDebug(ctx, payload)
	}
	c := newClient()
	u, err := joinURL(c.apiURL, "user", "token")
	if err != nil {
		return db.SamnaUser{}, fmt.Errorf("build url: %w", err)
	}
	result, err := DecodeJSON[db.SamnaUser](c.client.Post(ctx, requestBody(payload, true), u))
	if err != nil {
		return result, err
	}
	_, err = db.Instance().NewInsert().Model(&result).On("CONFLICT (id) DO UPDATE").Set("sa_updated_at = EXCLUDED.sa_updated_at").Set("sa_deleted_at = EXCLUDED.sa_deleted_at").Exec(ctx)
	if err != nil {
		return result, err
	}
	return result, nil
}

func postUserTokenDebug(ctx context.Context, payload db.SamnaUserToken) (db.SamnaUser, error) {
	if payload.Token == "" {
		return db.SamnaUser{}, fmt.Errorf("token is required")
	}
	parsed, _, err := jwt.NewParser().ParseUnverified(payload.Token, jwt.MapClaims{})
	if err != nil {
		return db.SamnaUser{}, fmt.Errorf("parse token: %w", err)
	}
	claims, ok := parsed.Claims.(jwt.MapClaims)
	if !ok {
		return db.SamnaUser{}, fmt.Errorf("invalid token claims")
	}

	idClaim := "sub"
	if payload.IDClaim != nil && *payload.IDClaim != "" {
		idClaim = *payload.IDClaim
	}
	rawID, present := claims[idClaim]
	if !present {
		return db.SamnaUser{}, fmt.Errorf("id claim missing in token")
	}
	tokenID, ok := rawID.(string)
	if !ok || tokenID == "" {
		return db.SamnaUser{}, fmt.Errorf("id claim not a string")
	}

	var userID uuid.UUID
	if payload.User != nil && payload.User.UserID != uuid.Nil {
		userID = payload.User.UserID
	} else if parsedID, parseErr := uuid.Parse(tokenID); parseErr == nil {
		userID = parsedID
	}

	externalID := tokenID
	if payload.User != nil && payload.User.ExternalID != nil && *payload.User.ExternalID != "" {
		externalID = *payload.User.ExternalID
	}

	app, err := db.Instance().GetApp(ctx)
	if err != nil {
		return db.SamnaUser{}, err
	}

	user := db.SamnaUser{
		UserID:     userID,
		AppID:      app.ID,
		ExternalID: &externalID,
	}
	if payload.User != nil {
		user.FirstName = payload.User.FirstName
		user.LastName = payload.User.LastName
		user.UserName = payload.User.UserName
		user.UserImage = payload.User.UserImage
		user.Phone = payload.User.Phone
		user.Email = payload.User.Email
	}

	_, err = db.Instance().NewInsert().Model(&user).
		On("CONFLICT (user_id, app_id) WHERE sa_deleted_at IS NULL DO UPDATE").
		Set("first_name = EXCLUDED.first_name").
		Set("last_name = EXCLUDED.last_name").
		Set("user_name = EXCLUDED.user_name").
		Set("user_image = EXCLUDED.user_image").
		Set("phone = EXCLUDED.phone").
		Set("email = EXCLUDED.email").
		Set("external_id = EXCLUDED.external_id").
		Set("sa_updated_at = EXCLUDED.sa_updated_at").
		Returning("*").
		Exec(ctx)
	if err != nil {
		return user, fmt.Errorf("debug upsert samna_user: %w", err)
	}

	if payload.AppID != nil && payload.ClaimID != nil {
		claimsJSON, marshalErr := json.Marshal(claims)
		if marshalErr != nil {
			return user, fmt.Errorf("marshal token claims: %w", marshalErr)
		}
		notrust := db.NotrustIdentity{
			AppID:      *payload.AppID,
			UserID:     user.UserID,
			ExternalID: externalID,
			IDClaim:    idClaim,
			ClaimID:    *payload.ClaimID,
			Reason:     payload.Reason,
			Data:       claimsJSON,
		}
		_, err := db.Instance().NewInsert().Model(&notrust).
			On("CONFLICT (app_id, user_id) WHERE sa_deleted_at IS NULL DO UPDATE").
			Set("external_id = EXCLUDED.external_id").
			Set("id_claim = EXCLUDED.id_claim").
			Set("claim_id = EXCLUDED.claim_id").
			Set("reason = EXCLUDED.reason").
			Set("data = EXCLUDED.data").
			Set("sa_updated_at = now()").
			Exec(ctx)
		if err != nil {
			return user, fmt.Errorf("debug upsert notrust_identity: %w", err)
		}
	}

	return user, nil
}

func PostUserTokenValidate(ctx context.Context, token string) (db.SamnaUser, error) {
	if token == "" {
		return db.SamnaUser{}, fmt.Errorf("token is required")
	}
	if getDebugUserID() != nil {
		return postUserTokenValidateDebug(ctx, token)
	}
	c := newClient()
	u, err := joinURL(c.apiURL, "user", "token", "validate")
	if err != nil {
		return db.SamnaUser{}, fmt.Errorf("build url: %w", err)
	}
	body := map[string]string{"token": token}
	return DecodeJSON[db.SamnaUser](c.client.Post(ctx, requestBody(body, true), u))
}

func postUserTokenValidateDebug(ctx context.Context, token string) (db.SamnaUser, error) {
	parsed, _, err := jwt.NewParser().ParseUnverified(token, jwt.MapClaims{})
	if err != nil {
		return db.SamnaUser{}, fmt.Errorf("parse token: %w", err)
	}
	claims, ok := parsed.Claims.(jwt.MapClaims)
	if !ok {
		return db.SamnaUser{}, fmt.Errorf("invalid token claims")
	}
	claimsJSON, err := json.Marshal(claims)
	if err != nil {
		return db.SamnaUser{}, fmt.Errorf("marshal claims: %w", err)
	}

	app, err := db.Instance().GetApp(ctx)
	if err != nil {
		return db.SamnaUser{}, err
	}

	var user db.SamnaUser
	if err := db.Instance().NewRaw(
		"SELECT * FROM public.get_notrust_user(?, ?::jsonb)",
		app.ID, string(claimsJSON),
	).Scan(ctx, &user); err != nil {
		return db.SamnaUser{}, fmt.Errorf("debug validate token: %w", err)
	}
	return user, nil
}

