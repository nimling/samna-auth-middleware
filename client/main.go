package client

import (
	"github.com/nimling/samna-auth-middleware/utils"
)

func newClient() *SAuthClient {
	endpoint := utils.Cfg().Endpoint
	slug := utils.Cfg().AppSlug
	secret := utils.Cfg().AppSecret

	return &SAuthClient{
		apiURL: endpoint + "/sam",
		client: newHTTPClient(slug, secret),
	}
}
