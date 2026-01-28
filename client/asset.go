package client

import (
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"

	"github.com/labstack/echo/v4"
)

func (c *SAuthClient) UploadAsset(ctx echo.Context, meta RequestMetadata) (*Asset, error) {
	fileHeader, err := ctx.FormFile("file")
	if err != nil {
		return nil, fmt.Errorf("FormFile: %v", err)
	}

	src, err := fileHeader.Open()
	if err != nil {
		return nil, fmt.Errorf("Open file: %v", err)
	}
	defer src.Close()

	pr, pw := io.Pipe()
	writer := multipart.NewWriter(pw)

	go func() {
		defer pw.Close()

		assetName := ctx.FormValue("name")
		if assetName != "" {
			if err := writer.WriteField("name", assetName); err != nil {
				pw.CloseWithError(fmt.Errorf("write field name: %w", err))
				return
			}
		}

		part, err := writer.CreateFormFile("file", fileHeader.Filename)
		if err != nil {
			pw.CloseWithError(fmt.Errorf("create form file: %w", err))
			return
		}

		if _, err := io.Copy(part, src); err != nil {
			pw.CloseWithError(err)
			return
		}

		if err := writer.Close(); err != nil {
			pw.CloseWithError(err)
		}
	}()

	uploadImageUrl, err := joinURL(c.apiURL, "asset")
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx.Request().Context(), http.MethodPost, uploadImageUrl, pr)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", meta.AccessToken))
	req.Header.Set("X-Request-ID", meta.RequestID)
	req.Header.Set("X-User-IP", meta.UserIP)
	req.Header.Set("SAuth-Client-Slug", c.requestExecutor.clientSlug)
	req.Header.Set("SAuth-Client-Secret", c.requestExecutor.clientSecret)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response body: %w", err)
	}

	var asset Asset
	if err = json.Unmarshal(respBody, &asset); err != nil {
		return nil, fmt.Errorf("unmarshal response body: %w", err)
	}

	return &asset, nil
}

func (c *SAuthClient) GetAsset(ctx echo.Context, meta RequestMetadata) error {
	imageURL, err := joinURL(c.apiURL, "asset", ctx.Param("id"), "data")

	req, err := http.NewRequest(http.MethodGet, imageURL, nil)
	if err != nil {
		return ctx.String(http.StatusInternalServerError, "internal server error")
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", meta.AccessToken))
	req.Header.Set("SAuth-Client-Slug", c.requestExecutor.clientSlug)
	req.Header.Set("SAuth-Client-Secret", c.requestExecutor.clientSecret)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return ctx.String(http.StatusInternalServerError, "Failed to fetch image")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return ctx.String(http.StatusBadGateway, "Remote server error")
	}

	return ctx.Stream(resp.StatusCode, resp.Header.Get("Content-Type"), resp.Body)
}

func (c *SAuthClient) DeleteAsset(ctx echo.Context, meta RequestMetadata) error {
	imageURL, err := joinURL(c.apiURL, "user", ctx.Param("id"), "image", ctx.Param("asset_id"))

	req, err := http.NewRequest(http.MethodDelete, imageURL, nil)
	if err != nil {
		return ctx.String(http.StatusInternalServerError, "internal server error")
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", meta.AccessToken))
	req.Header.Set("SAuth-Client-Slug", c.requestExecutor.clientSlug)
	req.Header.Set("SAuth-Client-Secret", c.requestExecutor.clientSecret)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return ctx.String(http.StatusInternalServerError, "Failed to fetch image")
	}
	defer resp.Body.Close()

	return ctx.NoContent(resp.StatusCode)
}
