package client

import (
	"context"
	"sync/atomic"

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
