package client

import (
	"encoding/json"
	"log"
	"net"
	"net/url"
	"path"

	"github.com/google/uuid"
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
