package client

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/url"
	"path"

	"github.com/google/uuid"
	"github.com/nimling/samna-auth-middleware/db"
)

func generateRequestID() string {
	return uuid.New().String()
}

func localIP() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return "unknown"
	}
	for _, addr := range addrs {
		if ipNet, ok := addr.(*net.IPNet); ok && !ipNet.IP.IsLoopback() && ipNet.IP.To4() != nil {
			return ipNet.IP.String()
		}
	}
	return "unknown"
}

func requestBody(model any, excludeID bool) map[string]any {
	data, _ := json.Marshal(model)
	var m map[string]any
	json.Unmarshal(data, &m)
	delete(m, "created_at")
	delete(m, "updated_at")
	delete(m, "deleted_at")
	if excludeID {
		delete(m, "id")
	}
	return m
}

func syncResponse[T any](ctx context.Context, body io.ReadCloser, err error) (T, error) {
	result, err := DecodeJSON[T](body, err)
	if err != nil {
		var zero T
		return zero, err
	}
	_, upsertErr := db.Instance().NewInsert().
		Model(&result).
		On("CONFLICT (id) DO UPDATE").
		Set("sa_updated_at = EXCLUDED.sa_updated_at").
		Set("sa_deleted_at = EXCLUDED.sa_deleted_at").
		Exec(ctx)
	if upsertErr != nil {
		var zero T
		return zero, fmt.Errorf("sync local db: %w", upsertErr)
	}
	return result, nil
}

func joinURL(baseURL string, segments ...string) (string, error) {
	base, err := url.Parse(baseURL)
	if err != nil {
		return "unknown", err
	}

	allSegments := append([]string{base.Path}, segments...)
	base.Path = path.Join(allSegments...)

	return base.String(), nil
}
