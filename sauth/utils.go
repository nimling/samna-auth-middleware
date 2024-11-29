package sauth

import (
	"net/url"
	"os"
)

func getEnvURL(envKey string) *url.URL {
	envVal, ok := os.LookupEnv(envKey)
	if !ok {
		panic("No authURL specified for AuthMiddleware")
	}

	parsedVal, err := url.Parse(envVal)
	if err != nil {
		panic("The authURL sat from env via SAMNA_AUTH_SERVER is malformed")
	}

	return parsedVal
}
