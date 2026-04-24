package utils

import (
	"fmt"
	"net/url"
	"os"

	"github.com/google/uuid"
)

func GetEnv(envKey string) string {
	envVal, ok := os.LookupEnv(envKey)
	if !ok {
		panic(fmt.Sprintf("Environment variable %s not set", envKey))
	}
	return envVal
}

func GetEnvURL(envKey string) url.URL {
	envVal, ok := os.LookupEnv(envKey)
	if !ok {
		panic(fmt.Sprintf("Environment variable %s not set", envKey))
	}
	parsedVal, err := url.Parse(envVal)
	if err != nil {
		panic(fmt.Sprintf("Environment variable %s is not a valid URL", envKey))
	}
	return *parsedVal
}

func CookieDomainFromURI(uri string) string {
	parsed, err := url.Parse(uri)
	if err != nil {
		return ""
	}
	host := parsed.Hostname()
	if host == "localhost" {
		return ""
	}
	return host
}

func GetEnvUUIDOptional(envKey string) *uuid.UUID {
	envVal, ok := os.LookupEnv(envKey)
	if !ok || envVal == "" {
		return nil
	}
	id, err := uuid.Parse(envVal)
	if err != nil {
		panic(fmt.Sprintf("Environment variable %s is not a valid UUID", envKey))
	}
	return &id
}
