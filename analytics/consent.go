package analytics

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/nimling/samna-auth-middleware/shared"
	"github.com/nimling/samna-auth-middleware/utils"
)

func PolicyVersion() string {
	return utils.Cfg().ConsentPolicyVersion
}

func PolicyURL() string {
	c := utils.Cfg()
	return strings.TrimRight(c.Endpoint, "/") + c.ConsentPolicyPath
}

func GetConsent(ctx context.Context, appID uuid.UUID, userID *uuid.UUID, anonymousID string) (*shared.ConsentState, error) {
	state := &shared.ConsentState{
		AnonymousID:   anonymousID,
		PolicyVersion: PolicyVersion(),
		PolicyURL:     PolicyURL(),
		Categories:    shared.ConsentCategories{Necessary: true},
	}
	if run == nil {
		return state, nil
	}
	var row shared.Consent
	err := run.db.NewRaw(
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
	if run == nil {
		return &shared.ConsentState{
			AnonymousID:   in.AnonymousID,
			PolicyVersion: PolicyVersion(),
			PolicyURL:     PolicyURL(),
			Decided:       true,
			Categories:    in.Categories,
		}, nil
	}
	var result string
	if err := run.db.NewRaw(
		"SELECT claimius.post_consent(?, ?, ?::jsonb, ?, ?, ?::inet, ?)",
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
