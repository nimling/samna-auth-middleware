package client

import (
	"context"
	"sync/atomic"

	"github.com/nimling/samna-auth-middleware/utils"
)

var boundEndpoint atomic.Value

// Bind points every request without a target at endpoint, the prophet a
// disciple is bound to, in place of the configured endpoint. An empty
// endpoint returns to the configured one.
func Bind(endpoint string) {
	boundEndpoint.Store(endpoint)
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
