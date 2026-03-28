package client

import (
	"encoding/json"
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

func joinURL(baseURL string, segments ...string) (string, error) {
	base, err := url.Parse(baseURL)
	if err != nil {
		return "unknown", err
	}

	allSegments := append([]string{base.Path}, segments...)
	base.Path = path.Join(allSegments...)

	return base.String(), nil
}
