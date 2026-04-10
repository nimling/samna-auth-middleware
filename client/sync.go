package client

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/nimling/samna-auth-middleware/db"
)

func init() {
	db.RegisterSyncFunc(Sync)
}

type syncResponse[T any] struct {
	Records []T `json:"data"`
}

func Sync(ctx context.Context) error {
	if err := SyncOrganizations(ctx); err != nil {
		return fmt.Errorf("sync organizations: %w", err)
	}
	if err := SyncLocations(ctx); err != nil {
		return fmt.Errorf("sync locations: %w", err)
	}
	if err := SyncSamnaUsers(ctx); err != nil {
		return fmt.Errorf("sync samna_users: %w", err)
	}
	if err := SyncClaims(ctx); err != nil {
		return fmt.Errorf("sync claims: %w", err)
	}
	if err := SyncUserClaims(ctx); err != nil {
		return fmt.Errorf("sync user_claims: %w", err)
	}
	if err := SyncClaimObjects(ctx); err != nil {
		return fmt.Errorf("sync claim_objects: %w", err)
	}
	return nil
}

func SyncOrganizations(ctx context.Context) error {
	return syncResource[db.Organization](ctx, "organization", "/organization")
}

func SyncLocations(ctx context.Context) error {
	return syncResource[db.Location](ctx, "location", "/location")
}

func SyncSamnaUsers(ctx context.Context) error {
	return syncResource[db.SamnaUser](ctx, "samna_user", "/user")
}

func SyncClaims(ctx context.Context) error {
	return syncResource[db.Claim](ctx, "claim", "/claim")
}

func SyncUserClaims(ctx context.Context) error {
	return syncResource[db.UserClaim](ctx, "user_claim", "/user-claim")
}

func SyncClaimObjects(ctx context.Context) error {
	return syncResource[db.ClaimObject](ctx, "claim_object", "/claim-object")
}

func syncResource[T any](ctx context.Context, objectType string, path string) error {
	endpoint := os.Getenv("SAUTH_ENDPOINT")
	slug := os.Getenv("SAUTH_CLIENT_SLUG")
	secret := os.Getenv("SAUTH_CLIENT_SECRET")

	syncURL, err := url.Parse(endpoint + "/sam" + path)
	if err != nil {
		return fmt.Errorf("build url: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, syncURL.String(), nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("SAuth-App-Slug", slug)
	req.Header.Set("SAuth-App-Secret", secret)

	httpClient := &http.Client{Timeout: 30 * time.Second}
	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("fetch %s: %w", objectType, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("sync %s returned %d: %s", objectType, resp.StatusCode, string(body))
	}

	var syncResp syncResponse[T]
	if err := json.NewDecoder(resp.Body).Decode(&syncResp); err != nil {
		return fmt.Errorf("decode %s: %w", objectType, err)
	}

	if len(syncResp.Records) == 0 {
		return nil
	}

	for _, record := range syncResp.Records {
		_, err := db.Instance().NewInsert().
			Model(&record).
			On("CONFLICT (id) DO UPDATE").
			Set("sa_updated_at = EXCLUDED.sa_updated_at").
			Set("sa_deleted_at = EXCLUDED.sa_deleted_at").
			Exec(ctx)
		if err != nil {
			return fmt.Errorf("upsert %s: %w", objectType, err)
		}
	}

	return nil
}
