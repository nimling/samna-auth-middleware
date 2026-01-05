package middleware

import (
	"fmt"
	"net/url"
	"os"

	"github.com/google/uuid"
)

func getEnv(envKey string) string {
	envVal, ok := os.LookupEnv(envKey)
	if !ok {
		panic(fmt.Sprintf("Environment variable %s not set", envKey))
	}

	return envVal
}

func getEnvUUID(envKey string) uuid.UUID {
	envVal, ok := os.LookupEnv(envKey)
	if !ok {
		panic(fmt.Sprintf("Environment variable %s not set", envKey))
	}

	id, err := uuid.Parse(envVal)
	if err != nil {
		panic(fmt.Sprintf("Environment variable %s is not a valid UUID", envKey))
	}

	return id
}

func getEnvURL(envKey string) url.URL {
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

func getEnvUUIDOptional(envKey string) *uuid.UUID {
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
