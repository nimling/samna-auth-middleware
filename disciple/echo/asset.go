package echo

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/MicahParks/keyfunc/v3"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/nimling/samna-auth-middleware/v2/client"
	sdb "github.com/nimling/samna-auth-middleware/v2/disciple/sdb"
	"github.com/nimling/samna-auth-middleware/v2/shared"
	"github.com/nimling/samna-auth-middleware/v2/smw"
	"github.com/nimling/samna-auth-middleware/v2/utils"
)

var (
	prophetKeysMu sync.Mutex
	prophetKeys   = map[string]jwt.Keyfunc{}
)

type AssetResponse struct {
	Status int
	Header http.Header
	Body   io.ReadCloser
}

type AssetCache interface {
	Get(ctx context.Context, linkID uuid.UUID, conditional http.Header) (*AssetResponse, error)
	Put(ctx context.Context, linkID uuid.UUID, header http.Header, body io.Reader) error
}

type Options struct {
	AssetSource func(ctx context.Context, assetID uuid.UUID) (*AssetResponse, error)
	AssetCache  AssetCache
}

var options Options

func PutOptions(o Options) {
	options = o
}

func GetAssetData(ctx echo.Context) error {
	return getAssetData(ctx, options.AssetSource)
}

func GetObjectAssetData(ctx echo.Context, linkID uuid.UUID) error {
	userID, _ := ctx.Get(smw.UserIDKey).(uuid.UUID)
	db := sdb.Instance()
	if _, err := db.GetObjectAsset(ctx.Request().Context(), userID, linkID); err != nil {
		if errors.Is(err, shared.ErrNotFound) {
			return respondError(ctx, http.StatusNotFound, "Not found")
		}
		ctx.Logger().Errorf("GetObjectAssetData: %v", err)
		return respondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	return getProphetAssetData(ctx, linkID, options.AssetCache)
}

func getAssetData(ctx echo.Context, source func(context.Context, uuid.UUID) (*AssetResponse, error)) error {
	if err := getProphetToken(ctx.Request()); err != nil {
		ctx.Logger().Warnf("asset data: %v", err)
		return respondError(ctx, http.StatusUnauthorized, "Unauthorized")
	}
	assetID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return respondError(ctx, http.StatusBadRequest, "id must be a uuid")
	}
	if source == nil {
		return respondError(ctx, http.StatusNotFound, "Not found")
	}
	res, err := source(ctx.Request().Context(), assetID)
	if err != nil {
		ctx.Logger().Errorf("asset source %s: %v", assetID, err)
		return respondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	if res == nil {
		return respondError(ctx, http.StatusNotFound, "Not found")
	}
	return putAssetResponse(ctx, res, ctx.Request().Header)
}

func getProphetToken(r *http.Request) error {
	c := utils.Cfg()
	prophet := client.GetProphet()
	if c.AuthDisabled || c.JWKSURL.String() == "" || prophet == "" {
		return errors.New("no prophet bound to check the token against")
	}
	raw, ok := strings.CutPrefix(r.Header.Get(echo.HeaderAuthorization), "Bearer ")
	if !ok || raw == "" {
		return errors.New("bearer token missing")
	}
	keys, err := getProphetKeys(c.JWKSURL.String())
	if err != nil {
		return err
	}
	if _, err := smw.ValidateToken(r, raw, &smw.AuthMiddlewareConfig{ClientSlug: c.AppSlug, ExpectedTokenIssuer: c.Issuer}, keys); err != nil {
		return err
	}
	var claims smw.TokenClaims
	if _, _, err := jwt.NewParser().ParseUnverified(raw, &claims); err != nil {
		return err
	}
	if claims.ClientID != prophet {
		return errors.New("token not issued to the prophet " + prophet)
	}
	if claims.ExpiresAt == nil {
		return errors.New("token without expiry")
	}
	return nil
}

func getProphetKeys(jwksURL string) (jwt.Keyfunc, error) {
	prophetKeysMu.Lock()
	defer prophetKeysMu.Unlock()
	if keys, ok := prophetKeys[jwksURL]; ok {
		return keys, nil
	}
	k, err := keyfunc.NewDefault([]string{jwksURL})
	if err != nil {
		return nil, err
	}
	prophetKeys[jwksURL] = k.Keyfunc
	return k.Keyfunc, nil
}

func getProphetAssetData(ctx echo.Context, linkID uuid.UUID, cache AssetCache) error {
	reqCtx := ctx.Request().Context()
	conditional := http.Header{}
	for _, name := range client.ConditionalHeaders {
		if v := ctx.Request().Header.Get(name); v != "" {
			conditional.Set(name, v)
		}
	}
	request := conditional.Clone()
	for _, name := range client.RangeHeaders {
		if v := ctx.Request().Header.Get(name); v != "" {
			request.Set(name, v)
		}
	}
	path := "object_asset/" + linkID.String() + "/data"

	if cache != nil {
		cached, err := cache.Get(reqCtx, linkID, conditional)
		if err != nil {
			ctx.Logger().Warnf("asset cache get %s: %v", linkID, err)
			cached = nil
		}
		if cached != nil {
			if getAssetFresh(cached.Header, time.Now()) {
				return putAssetResponse(ctx, cached, request)
			}
			validators := http.Header{}
			if v := cached.Header.Get("ETag"); v != "" {
				validators.Set("If-None-Match", v)
			}
			if v := cached.Header.Get(echo.HeaderLastModified); v != "" {
				validators.Set("If-Modified-Since", v)
			}
			resp, err := client.GetStream(reqCtx, path, validators)
			if err != nil {
				if cached.Body != nil {
					cached.Body.Close()
				}
				return putProphetError(ctx, err)
			}
			if resp.StatusCode == http.StatusNotModified {
				resp.Body.Close()
				for _, name := range client.AssetHeaders {
					if v := resp.Header.Get(name); v != "" && name != echo.HeaderContentLength && name != echo.HeaderContentType && name != "Content-Range" {
						cached.Header.Set(name, v)
					}
				}
				return putAssetResponse(ctx, cached, request)
			}
			if cached.Body != nil {
				cached.Body.Close()
			}
			return putProphetResponse(ctx, linkID, cache, resp, request)
		}
		resp, err := client.GetStream(reqCtx, path, conditional)
		if err != nil {
			return putProphetError(ctx, err)
		}
		return putProphetResponse(ctx, linkID, cache, resp, request)
	}

	resp, err := client.GetStream(reqCtx, path, request)
	if err != nil {
		return putProphetError(ctx, err)
	}
	return putProphetResponse(ctx, linkID, nil, resp, request)
}

func putProphetResponse(ctx echo.Context, linkID uuid.UUID, cache AssetCache, resp *http.Response, request http.Header) error {
	defer resp.Body.Close()
	for _, name := range client.AssetHeaders {
		if v := resp.Header.Get(name); v != "" {
			ctx.Response().Header().Set(name, v)
		}
	}
	if resp.StatusCode != http.StatusOK {
		ctx.Response().WriteHeader(resp.StatusCode)
		if resp.StatusCode == http.StatusNotModified || resp.StatusCode == http.StatusRequestedRangeNotSatisfiable {
			return nil
		}
		_, err := io.Copy(ctx.Response(), resp.Body)
		return err
	}
	if cache == nil || !getAssetStorable(resp.Header) {
		return putAssetRange(ctx, resp.Header, resp.Body, request)
	}
	reader, writer := io.Pipe()
	done := make(chan struct{})
	header := resp.Header.Clone()
	go func() {
		defer close(done)
		if err := cache.Put(context.WithoutCancel(ctx.Request().Context()), linkID, header, reader); err != nil {
			ctx.Logger().Warnf("asset cache put %s: %v", linkID, err)
		}
		_, _ = io.Copy(io.Discard, reader)
	}()
	whole := io.TeeReader(resp.Body, writer)
	err := putAssetRange(ctx, resp.Header, whole, request)
	if err == nil {
		_, err = io.Copy(io.Discard, whole)
	}
	writer.CloseWithError(err)
	<-done
	return err
}

func putProphetError(ctx echo.Context, err error) error {
	switch {
	case errors.Is(err, client.ErrNotFound):
		return respondError(ctx, http.StatusNotFound, "Not found")
	case errors.Is(err, client.ErrForbidden):
		return respondError(ctx, http.StatusForbidden, "Forbidden")
	}
	ctx.Logger().Errorf("asset from prophet: %v", err)
	return respondError(ctx, http.StatusBadGateway, "Bad gateway")
}

func putAssetResponse(ctx echo.Context, res *AssetResponse, request http.Header) error {
	if res.Body != nil {
		defer res.Body.Close()
	}
	status := res.Status
	if status == 0 {
		status = http.StatusOK
	}
	for _, name := range client.AssetHeaders {
		if v := res.Header.Get(name); v != "" {
			ctx.Response().Header().Set(name, v)
		}
	}
	if status == http.StatusOK && getAssetNotModified(res.Header, request) {
		ctx.Response().Header().Del(echo.HeaderContentLength)
		ctx.Response().WriteHeader(http.StatusNotModified)
		return nil
	}
	if status == http.StatusOK && res.Body != nil {
		return putAssetRange(ctx, res.Header, res.Body, request)
	}
	ctx.Response().WriteHeader(status)
	if status == http.StatusNotModified || res.Body == nil {
		return nil
	}
	_, err := io.Copy(ctx.Response(), res.Body)
	return err
}

func putAssetRange(ctx echo.Context, header http.Header, body io.Reader, request http.Header) error {
	size, err := strconv.ParseInt(header.Get(echo.HeaderContentLength), 10, 64)
	if err != nil {
		size = -1
	}
	out := ctx.Response()
	if size >= 0 {
		out.Header().Set("Accept-Ranges", "bytes")
	}
	r := client.GetAssetRange(request, size, header.Get("ETag"), header.Get(echo.HeaderLastModified))
	switch r.Status {
	case http.StatusRequestedRangeNotSatisfiable:
		out.Header().Set("Content-Range", r.GetContentRange())
		out.Header().Set(echo.HeaderContentLength, "0")
		out.WriteHeader(r.Status)
		return nil
	case http.StatusPartialContent:
		if seeker, ok := body.(io.Seeker); ok {
			if _, err := seeker.Seek(r.Start, io.SeekStart); err != nil {
				return err
			}
		} else if _, err := io.CopyN(io.Discard, body, r.Start); err != nil {
			return err
		}
		out.Header().Set("Content-Range", r.GetContentRange())
		out.Header().Set(echo.HeaderContentLength, strconv.FormatInt(r.Length, 10))
		out.WriteHeader(r.Status)
		_, err := io.CopyN(out, body, r.Length)
		return err
	}
	out.WriteHeader(http.StatusOK)
	_, err = io.Copy(out, body)
	return err
}

func getAssetNotModified(header, conditional http.Header) bool {
	if match := conditional.Get("If-None-Match"); match != "" {
		etag := header.Get("ETag")
		if etag == "" {
			return false
		}
		for _, candidate := range strings.Split(match, ",") {
			candidate = strings.TrimSpace(candidate)
			if candidate == "*" || strings.TrimPrefix(candidate, "W/") == strings.TrimPrefix(etag, "W/") {
				return true
			}
		}
		return false
	}
	since, err := http.ParseTime(conditional.Get("If-Modified-Since"))
	if err != nil {
		return false
	}
	modified, err := http.ParseTime(header.Get(echo.HeaderLastModified))
	return err == nil && !modified.After(since)
}

func getAssetStorable(header http.Header) bool {
	directives := getCacheDirectives(header)
	_, noStore := directives["no-store"]
	_, private := directives["private"]
	return !noStore && !private
}

func getAssetFresh(header http.Header, now time.Time) bool {
	directives := getCacheDirectives(header)
	for _, name := range []string{"no-store", "no-cache", "private"} {
		if _, ok := directives[name]; ok {
			return false
		}
	}
	lifetime, ok := directives["s-maxage"]
	if !ok {
		lifetime, ok = directives["max-age"]
	}
	if !ok {
		return false
	}
	seconds, err := strconv.Atoi(lifetime)
	if err != nil {
		return false
	}
	date, err := http.ParseTime(header.Get("Date"))
	if err != nil {
		return false
	}
	age := now.Sub(date)
	if v, err := strconv.Atoi(header.Get("Age")); err == nil {
		age += time.Duration(v) * time.Second
	}
	return age < time.Duration(seconds)*time.Second
}

func getCacheDirectives(header http.Header) map[string]string {
	directives := map[string]string{}
	for _, part := range strings.Split(header.Get(echo.HeaderCacheControl), ",") {
		name, value, _ := strings.Cut(strings.TrimSpace(part), "=")
		if name == "" {
			continue
		}
		directives[strings.ToLower(name)] = strings.Trim(value, `"`)
	}
	return directives
}
