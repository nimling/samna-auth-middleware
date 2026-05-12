package client

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"

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

func PostUser(ctx context.Context, user db.SamnaUser) (db.SamnaUser, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "user")
	if err != nil {
		return db.SamnaUser{}, fmt.Errorf("build url: %w", err)
	}
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
	if db.GetDebugMode() != db.DebugModeNone {
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

	if err := db.Instance().NewRaw(`
		SELECT (claimius.ensure_app_user(?, claimius.get_disciple_app_id(), ?, ?, ?, ?, ?, ?, ?) -> 'samna_user' ->> 'user_id')::uuid
	`, user.UserID, user.FirstName, user.LastName, user.UserName, user.UserImage, user.Email, user.Phone, user.ExternalID).Scan(ctx, &user.UserID); err != nil {
		return user, fmt.Errorf("debug ensure_app_user: %w", err)
	}

	fetched, err := db.Instance().GetUser(ctx, user.UserID)
	if err != nil {
		return user, fmt.Errorf("debug fetch samna_user: %w", err)
	}
	user = *fetched

	claimsJSON, marshalErr := json.Marshal(claims)
	if marshalErr != nil {
		return user, fmt.Errorf("marshal token claims: %w", marshalErr)
	}
	tokenOwnerID := user.UserID
	if payload.OwnerID != nil && *payload.OwnerID != uuid.Nil {
		tokenOwnerID = *payload.OwnerID
	}
	if rootErr := db.Instance().AssertUserTokenSingleRoot(ctx, externalID, idClaim, tokenOwnerID); rootErr != nil {
		return user, rootErr
	}
	token := db.UserToken{
		AppID:      app.ID,
		UserID:     user.UserID,
		IDClaim:    idClaim,
		ExternalID: externalID,
		ClaimID:    payload.ClaimID,
		Data:       claimsJSON,
		OwnerID:    tokenOwnerID,
	}
	if _, err := db.Instance().NewInsert().Model(&token).
		On("CONFLICT (app_id, sa_owner_id, id_claim, external_id) WHERE sa_deleted_at IS NULL DO UPDATE").
		Set("user_id = EXCLUDED.user_id").
		Set("claim_id = EXCLUDED.claim_id").
		Set("data = EXCLUDED.data").
		Set("sa_updated_at = now()").
		Exec(ctx); err != nil {
		return user, fmt.Errorf("debug upsert user_token: %w", err)
	}

	return user, nil
}

func PostUserTokenValidate(ctx context.Context, token string) (db.SamnaUser, uuid.UUID, error) {
	if token == "" {
		return db.SamnaUser{}, uuid.Nil, fmt.Errorf("token is required")
	}
	if db.GetDebugMode() != db.DebugModeNone {
		return postUserTokenValidateDebug(ctx, token)
	}
	c := newClient()
	u, err := joinURL(c.apiURL, "user", "token", "validate")
	if err != nil {
		return db.SamnaUser{}, uuid.Nil, fmt.Errorf("build url: %w", err)
	}
	body := map[string]string{"token": token}
	user, err := DecodeJSON[db.SamnaUser](c.client.Post(ctx, requestBody(body, true), u))
	return user, uuid.Nil, err
}

func postUserTokenValidateDebug(ctx context.Context, token string) (db.SamnaUser, uuid.UUID, error) {
	parsed, _, err := jwt.NewParser().ParseUnverified(token, jwt.MapClaims{})
	if err != nil {
		return db.SamnaUser{}, uuid.Nil, fmt.Errorf("parse token: %w", err)
	}
	claims, ok := parsed.Claims.(jwt.MapClaims)
	if !ok {
		return db.SamnaUser{}, uuid.Nil, fmt.Errorf("invalid token claims")
	}
	claimsJSON, err := json.Marshal(claims)
	if err != nil {
		return db.SamnaUser{}, uuid.Nil, fmt.Errorf("marshal claims: %w", err)
	}

	app, err := db.Instance().GetApp(ctx)
	if err != nil {
		return db.SamnaUser{}, uuid.Nil, err
	}

	var user db.SamnaUser
	var ownerID uuid.UUID
	err = db.Instance().NewRaw(`
		SELECT t.user_id, t.sa_owner_id
		  FROM claimius.user_token t
		 WHERE t.app_id        = ?
		   AND t.sa_deleted_at IS NULL
		   AND t.external_id   = (?::jsonb ->> t.id_claim)
		 ORDER BY t.sa_created_at DESC
		 LIMIT 1
	`, app.ID, string(claimsJSON)).Scan(ctx, &user.UserID, &ownerID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return db.SamnaUser{}, uuid.Nil, nil
		}
		return db.SamnaUser{}, uuid.Nil, fmt.Errorf("debug validate token: %w", err)
	}
	fetched, ferr := db.Instance().GetUser(ctx, user.UserID)
	if ferr != nil {
		if errors.Is(ferr, sql.ErrNoRows) {
			return db.SamnaUser{}, uuid.Nil, nil
		}
		return db.SamnaUser{}, uuid.Nil, fmt.Errorf("debug fetch samna_user: %w", ferr)
	}
	return *fetched, ownerID, nil
}

