package client

import (
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

func joinURL(baseURL string, segments ...string) (string, error) {
	base, err := url.Parse(baseURL)
	if err != nil {
		return "", err
	}

	allSegments := append([]string{base.Path}, segments...)
	base.Path = path.Join(allSegments...)

	return base.String(), nil
}
