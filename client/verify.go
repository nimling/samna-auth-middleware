package client

import (
	"github.com/nimling/samna-auth-middleware/utils"
)

func VerifyRequest(slug string, secret string) bool {
	expectedSlug := utils.Cfg().AppSlug
	expectedSecret := utils.Cfg().AppSecret

	if expectedSlug == "" || expectedSecret == "" {
		return false
	}

	return slug == expectedSlug && secret == expectedSecret
}
