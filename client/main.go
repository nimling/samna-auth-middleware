package client

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"

	"github.com/google/uuid"
	"github.com/nimling/samna-auth-middleware/v2/utils"
)

var (
	boundEndpoint atomic.Value
	boundProphet  atomic.Value
)

func Bind(endpoint, prophet string) {
	boundEndpoint.Store(endpoint)
	boundProphet.Store(prophet)
}

func GetProphet() string {
	prophet, _ := boundProphet.Load().(string)
	return prophet
}

func newClient(ctx context.Context) *SAuthClient {
	endpoint := utils.Cfg().Endpoint
	slug := utils.Cfg().AppSlug
	secret := utils.Cfg().AppSecret
	if bound, ok := boundEndpoint.Load().(string); ok && bound != "" {
		endpoint = bound
	}
	if t, ok := ctx.Value(targetKey{}).(Target); ok {
		endpoint, slug, secret = t.Endpoint, t.Slug, t.Secret
	}

	return &SAuthClient{
		apiURL: endpoint + "/sam",
		client: newHTTPClient(slug, secret),
	}
}

type AppSeed struct {
	PrivateSeed string    `json:"private_seed"`
	OwnerID     uuid.UUID `json:"owner_id"`
}

var appSeed atomic.Pointer[AppSeed]

func GetAppSeed(ctx context.Context) (*AppSeed, error) {
	if seed := appSeed.Load(); seed != nil {
		return seed, nil
	}
	var delivered AppSeed
	if err := Get(ctx, "app", &delivered); err != nil {
		return nil, fmt.Errorf("get app seed from the prophet: %w", err)
	}
	if delivered.PrivateSeed == "" {
		return nil, errors.New("the prophet delivered no app seed")
	}
	appSeed.Store(&delivered)
	return &delivered, nil
}

func DeleteAppSeed() {
	appSeed.Store(nil)
}
