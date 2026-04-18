package client

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"
)

func GetSearch(ctx context.Context, query string, objectTypes []string, limit int, offset int) (io.ReadCloser, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "search")
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}

	parsed, err := url.Parse(u)
	if err != nil {
		return nil, fmt.Errorf("parse url: %w", err)
	}

	q := parsed.Query()
	q.Set("q", query)
	if len(objectTypes) > 0 {
		q.Set("object_types", strings.Join(objectTypes, ","))
	}
	if limit > 0 {
		q.Set("limit", strconv.Itoa(limit))
	}
	if offset > 0 {
		q.Set("offset", strconv.Itoa(offset))
	}
	parsed.RawQuery = q.Encode()

	return c.client.Get(ctx, parsed.String())
}
