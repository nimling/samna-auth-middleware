package analytics

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/nimling/samna-auth-middleware/v2/shared"
	"github.com/nimling/samna-auth-middleware/v2/utils"
)

const (
	policyVersion   = "1"
	policyPath      = "/legal/cookie-policy"
	AnonymousCookie = "sauth_anonymous_id"
)

func PolicyVersion() string {
	return policyVersion
}

func PolicyURL() string {
	return strings.TrimRight(utils.Cfg().Endpoint, "/") + policyPath
}

func AnonymousID(ctx echo.Context) string {
	id := ""
	if cookie, err := ctx.Cookie(AnonymousCookie); err == nil && cookie.Value != "" {
		id = cookie.Value
	}
	if id == "" {
		id = ctx.QueryParam("anonymous_id")
	}
	if id == "" {
		id = uuid.NewString()
	}
	WriteAnonymousID(ctx, id)
	return id
}

func WriteAnonymousID(ctx echo.Context, value string) {
	if value == "" {
		return
	}
	if cookie, err := ctx.Cookie(AnonymousCookie); err == nil && cookie.Value == value {
		return
	}
	utils.WriteCookie(ctx, &http.Cookie{
		Name:   AnonymousCookie,
		Value:  value,
		Path:   "/",
		MaxAge: 60 * 60 * 24 * 365,
	})
}

func GetConsent(ctx context.Context, appID uuid.UUID, userID *uuid.UUID, anonymousID string) (*shared.ConsentState, error) {
	state := &shared.ConsentState{
		AnonymousID:   anonymousID,
		PolicyVersion: PolicyVersion(),
		PolicyURL:     PolicyURL(),
		Categories:    shared.ConsentCategories{Necessary: true},
	}
	r := current()
	if r == nil {
		return nil, ErrNotStarted
	}
	var row shared.Consent
	err := r.db.NewRaw(
		"SELECT * FROM claimius.get_consent(?, ?, ?)",
		appID, userID, nullString(anonymousID),
	).Scan(ctx, &row)
	if errors.Is(err, sql.ErrNoRows) {
		return state, nil
	}
	if err != nil {
		return state, err
	}
	if row.PolicyVersion != state.PolicyVersion {
		return state, nil
	}
	if len(row.Categories) > 0 {
		if unmarshalErr := json.Unmarshal(row.Categories, &state.Categories); unmarshalErr != nil {
			return state, unmarshalErr
		}
	}
	state.Categories.Necessary = true
	state.Decided = true
	state.GrantedAt = &row.GrantedAt
	return state, nil
}

func PostConsent(ctx context.Context, appID uuid.UUID, in shared.ConsentRequest, userID *uuid.UUID, ip, userAgent string) (*shared.ConsentState, error) {
	in.Categories.Necessary = true
	categories, err := json.Marshal(in.Categories)
	if err != nil {
		return nil, err
	}
	r := current()
	if r == nil {
		return nil, ErrNotStarted
	}
	var result string
	if err := r.db.NewRaw(
		"SELECT claimius.insert_consent(?, ?, ?::jsonb, ?, ?, ?::inet, ?)",
		appID, in.AnonymousID, json.RawMessage(categories), in.PolicyVersion, userID, nullString(ip), nullString(userAgent),
	).Scan(ctx, &result); err != nil {
		return nil, err
	}
	var wrapper struct {
		Consent shared.Consent `json:"consent"`
	}
	if err := json.Unmarshal([]byte(result), &wrapper); err != nil {
		return nil, err
	}
	return &shared.ConsentState{
		AnonymousID:   wrapper.Consent.AnonymousID,
		PolicyVersion: wrapper.Consent.PolicyVersion,
		PolicyURL:     PolicyURL(),
		Decided:       true,
		Categories:    in.Categories,
		GrantedAt:     &wrapper.Consent.GrantedAt,
	}, nil
}
