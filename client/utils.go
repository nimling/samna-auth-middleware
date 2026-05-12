package client

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net"
	"net/url"
	"path"
	"strconv"

	"github.com/google/uuid"
	"github.com/nimling/samna-auth-middleware/db"
)

func jsonReader(v any) (io.ReadCloser, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

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
	data, err := json.Marshal(model)
	if err != nil {
		log.Printf("samna_auth_middleware: requestBody marshal failed: %v", err)
		return map[string]any{}
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		log.Printf("samna_auth_middleware: requestBody unmarshal failed: %v", err)
		return map[string]any{}
	}
	delete(m, "created_at")
	delete(m, "updated_at")
	delete(m, "deleted_at")
	if excludeID {
		delete(m, "id")
	}
	return m
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

func appendGraphQuery(u string, startID *uuid.UUID, depth int, requiredAccess db.ClaimAccess, limit int) string {
	q := url.Values{}
	if startID != nil {
		q.Set("start_id", startID.String())
	}
	if depth != 0 {
		q.Set("depth", strconv.Itoa(depth))
	}
	q.Set("required_access", strconv.Itoa(int(requiredAccess)))
	if limit > 0 {
		q.Set("limit", strconv.Itoa(limit))
	}
	encoded := q.Encode()
	if encoded == "" {
		return u
	}
	if parsed, err := url.Parse(u); err == nil {
		parsed.RawQuery = encoded
		return parsed.String()
	}
	return u + "?" + encoded
}
