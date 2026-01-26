package client

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"path"

	"github.com/google/uuid"
)

var ErrBadRequest = errors.New("bad request")
var ErrForbidden = errors.New("forbidden")
var ErrNotFound = errors.New("not found")

type SAuthClient struct {
	apiURL          string
	requestExecutor *httpRequestExecutor
}

func NewSAuthClient(apiURL string, clientSlug, clientSecret string) *SAuthClient {
	return &SAuthClient{
		apiURL:          apiURL,
		requestExecutor: newHTTPRequestExecutor(clientSlug, clientSecret),
	}
}

func (c *SAuthClient) GetUser(ctx context.Context, meta RequestMetadata, userID uuid.UUID) (io.ReadCloser, error) {
	url, err := joinURL(c.apiURL, "user", userID.String())
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}

	return c.requestExecutor.Get(ctx, meta, url)
}

func (c *SAuthClient) PutUser(ctx context.Context, meta RequestMetadata, userID uuid.UUID, body UpdateUserBody) (io.ReadCloser, error) {
	url, err := joinURL(c.apiURL, "user", userID.String())
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}

	return c.requestExecutor.Put(ctx, meta, body, url)
}

func (c *SAuthClient) PostOrganization(ctx context.Context, meta RequestMetadata, body CreateOrganizationBody) error {
	url, err := joinURL(c.apiURL, "organization")
	if err != nil {
		return fmt.Errorf("build url: %w", err)
	}

	_, err = c.requestExecutor.Post(ctx, meta, body, url)
	return err
}

func (c *SAuthClient) PutOrganization(ctx context.Context, meta RequestMetadata, orgID uuid.UUID, body UpdateOrganizationBody) error {
	url, err := joinURL(c.apiURL, "organization", orgID.String())
	if err != nil {
		return fmt.Errorf("build url: %w", err)
	}

	_, err = c.requestExecutor.Put(ctx, meta, body, url)
	return err
}

func (c *SAuthClient) DeleteOrganization(ctx context.Context, meta RequestMetadata, orgID uuid.UUID) error {
	url, err := joinURL(c.apiURL, "organization", orgID.String())
	if err != nil {
		return fmt.Errorf("build url: %w", err)
	}

	_, err = c.requestExecutor.Delete(ctx, meta, url)
	return err
}

func (c *SAuthClient) PostLocation(ctx context.Context, meta RequestMetadata, body CreateLocationBody) error {
	url, err := joinURL(c.apiURL, "location")
	if err != nil {
		return fmt.Errorf("build url: %w", err)
	}

	_, err = c.requestExecutor.Post(ctx, meta, body, url)
	return err
}

func (c *SAuthClient) PutLocation(ctx context.Context, meta RequestMetadata, locID uuid.UUID, body UpdateLocationBody) error {
	url, err := joinURL(c.apiURL, "location", locID.String())
	if err != nil {
		return fmt.Errorf("build url: %w", err)
	}

	_, err = c.requestExecutor.Put(ctx, meta, body, url)
	return err
}

func (c *SAuthClient) DeleteLocation(ctx context.Context, meta RequestMetadata, locID uuid.UUID) error {
	url, err := joinURL(c.apiURL, "location", locID.String())
	if err != nil {
		return fmt.Errorf("build url: %w", err)
	}

	_, err = c.requestExecutor.Delete(ctx, meta, url)
	return err
}

func (c *SAuthClient) PostClaim(ctx context.Context, meta RequestMetadata, body CreateClaimBody) error {
	url, err := joinURL(c.apiURL, "claim")
	if err != nil {
		return fmt.Errorf("build url: %w", err)
	}

	_, err = c.requestExecutor.Post(ctx, meta, body, url)
	return err
}

func (c *SAuthClient) PutClaim(ctx context.Context, meta RequestMetadata, claimID uuid.UUID, body UpdateClaimBody) error {
	url, err := joinURL(c.apiURL, "claim", claimID.String())
	if err != nil {
		return fmt.Errorf("build url: %w", err)
	}

	_, err = c.requestExecutor.Put(ctx, meta, body, url)
	return err
}

func (c *SAuthClient) DeleteClaim(ctx context.Context, meta RequestMetadata, claimID uuid.UUID) error {
	url, err := joinURL(c.apiURL, "claim", claimID.String())
	if err != nil {
		return fmt.Errorf("build url: %w", err)
	}

	_, err = c.requestExecutor.Delete(ctx, meta, url)
	return err
}

func (c *SAuthClient) PostClaimObject(ctx context.Context, meta RequestMetadata, claimID uuid.UUID, body CreateClaimObjectBody) error {
	url, err := joinURL(c.apiURL, "claim", claimID.String(), "object")
	if err != nil {
		return fmt.Errorf("build url: %w", err)
	}

	_, err = c.requestExecutor.Post(ctx, meta, body, url)
	return err
}

func (c *SAuthClient) DeleteClaimObject(ctx context.Context, meta RequestMetadata, claimObjectID uuid.UUID) error {
	url, err := joinURL(c.apiURL, "claim", "object", claimObjectID.String())
	if err != nil {
		return fmt.Errorf("build url: %w", err)
	}

	_, err = c.requestExecutor.Delete(ctx, meta, url)
	return err
}

func (c *SAuthClient) PostClaimUser(ctx context.Context, meta RequestMetadata, claimID uuid.UUID, body CreateClaimUserBody) error {
	url, err := joinURL(c.apiURL, "claim", claimID.String(), "user")
	if err != nil {
		return fmt.Errorf("build url: %w", err)
	}

	_, err = c.requestExecutor.Post(ctx, meta, body, url)
	return err
}

func (c *SAuthClient) DeleteClaimUser(ctx context.Context, meta RequestMetadata, claimUserID uuid.UUID) error {
	url, err := joinURL(c.apiURL, "claim", "user", claimUserID.String())
	if err != nil {
		return fmt.Errorf("build url: %w", err)
	}

	_, err = c.requestExecutor.Delete(ctx, meta, url)
	return err
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
