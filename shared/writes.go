package shared

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"github.com/nimling/samna-auth-middleware/v2/client"
)

func (d *DB) ValidateToken(ctx context.Context, token string) (*SamnaUser, uuid.UUID, error) {
	if token == "" {
		return nil, uuid.Nil, fmt.Errorf("token is required")
	}
	if GetDebugMode() != DebugModeNone {
		return d.validateTokenDebug(ctx, token)
	}
	body := map[string]string{"token": token}
	var user SamnaUser
	if err := client.Post(ctx, "user/token/validate", body, &user); err != nil {
		return nil, uuid.Nil, err
	}
	if user.OwnerID == nil || *user.OwnerID == uuid.Nil {
		return nil, uuid.Nil, fmt.Errorf("token validation carried no owner")
	}
	return &user, *user.OwnerID, nil
}

func (d *DB) validateTokenDebug(ctx context.Context, token string) (*SamnaUser, uuid.UUID, error) {
	parsed, _, err := jwt.NewParser().ParseUnverified(token, jwt.MapClaims{})
	if err != nil {
		return nil, uuid.Nil, fmt.Errorf("parse token: %w", err)
	}
	claims, ok := parsed.Claims.(jwt.MapClaims)
	if !ok {
		return nil, uuid.Nil, fmt.Errorf("invalid token claims")
	}
	claimsJSON, err := json.Marshal(claims)
	if err != nil {
		return nil, uuid.Nil, fmt.Errorf("marshal claims: %w", err)
	}
	var userID uuid.UUID
	var ownerID uuid.UUID
	err = d.NewRaw(`
		SELECT t.user_id, t.sa_owner_id
		  FROM claimius.user_token t
		 WHERE t.sa_deleted_at IS NULL
		   AND t.external_id   = (?::jsonb ->> t.id_claim)
		 ORDER BY t.sa_created_at DESC
		 LIMIT 1
	`, string(claimsJSON)).Scan(ctx, &userID, &ownerID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, uuid.Nil, nil
		}
		return nil, uuid.Nil, fmt.Errorf("debug validate token: %w", err)
	}
	fetched, ferr := d.GetUser(ctx, userID, userID)
	if ferr != nil {
		if errors.Is(ferr, sql.ErrNoRows) {
			return nil, uuid.Nil, nil
		}
		return nil, uuid.Nil, fmt.Errorf("debug fetch samna_user: %w", ferr)
	}
	return fetched, ownerID, nil
}

func (d *DB) ResolveExternalUser(ctx context.Context, callerID uuid.UUID, externalID string) (*SamnaUser, error) {
	if externalID == "" {
		return nil, fmt.Errorf("external_id is required")
	}
	if d.RouteToProphet {
		var user SamnaUser
		if err := client.Post(ctx, "user/token/validate", map[string]string{"external_id": externalID}, &user); err != nil {
			if errors.Is(err, client.ErrUnauthorized) {
				return nil, nil
			}
			return nil, fmt.Errorf("resolve external user: %w", err)
		}
		if user.OwnerID == nil || *user.OwnerID == uuid.Nil || callerID == uuid.Nil || !d.HasAccess(ctx, callerID, *user.OwnerID, OBJECT_TYPE_ORGANIZATION, ACCESS_READ) {
			return nil, nil
		}
		return &user, nil
	}
	var userID uuid.UUID
	var ownerID uuid.UUID
	err := d.NewRaw(`
		SELECT t.user_id, t.sa_owner_id
		  FROM claimius.user_token t
		 WHERE t.type          = 'external'
		   AND t.external_id   = ?
		   AND t.sa_deleted_at IS NULL
		 ORDER BY t.sa_created_at DESC
		 LIMIT 1
	`, externalID).Scan(ctx, &userID, &ownerID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("resolve external user: %w", err)
	}
	if ownerID == uuid.Nil || callerID == uuid.Nil || !d.HasAccess(ctx, callerID, ownerID, OBJECT_TYPE_ORGANIZATION, ACCESS_READ) {
		return nil, nil
	}
	fetched, ferr := d.GetUser(ctx, userID, userID)
	if ferr != nil {
		if errors.Is(ferr, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("fetch samna_user: %w", ferr)
	}
	return fetched, nil
}
