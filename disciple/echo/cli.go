package echo

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	_ "embed"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"html/template"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/nimling/samna-auth-middleware/v2/client"
	"github.com/nimling/samna-auth-middleware/v2/disciple/sdb"
	"github.com/nimling/samna-auth-middleware/v2/shared"
	"github.com/nimling/samna-auth-middleware/v2/utils"
)

const (
	CliPath             = "/api/auth/cli"
	CliSessionPath      = CliPath + "/session"
	CliTokenPath        = CliPath + "/token"
	CliRefreshPath      = CliPath + "/refresh"
	CliLogoutPath       = CliPath + "/logout"
	cliLoginPath        = "/api/auth/login"
	cliCSRFHeader       = "SAuth-CSRF-Token"
	cliAccessCookie     = "sauth_access_token"
	cliRefreshCookie    = "sauth_refresh_token"
	cliCSRFCookie       = "sauth_csrf_token"
	cliStatusApproved   = "approved"
	cliStatusPending    = "pending"
	cliLoginLifetime    = 10 * time.Minute
	cliLoginInterval    = 2
	cliUserCodeGroups   = 2
	cliUserCodeLength   = 4
	cliUserCodeChars    = "BCDFGHJKLMNPQRSTVWXZ"
	cliSecretBytes      = 32
	cliDeviceCodeJoin   = "."
	cliHashHandle       = "handle"
	cliHashUserCode     = "user_code"
	cliHashDeviceCode   = "device_code"
	cliHashDeriveCode   = "derive_user_code"
	cliDebugRefusal     = "this server runs in debug mode and authenticates every request as the debug user, so there is no login to complete"
	cliRetry            = "run the cli login again"
	cliLoginNoneMessage = "no open login carries that link, it may have expired, " + cliRetry
	cliInternalError    = "Internal Server Error"
)

//go:embed cli/approve.html
var cliApprovalPage string

var cliApprovalTemplate = template.Must(template.New("approve").Parse(cliApprovalPage))

type BodyCliToken struct {
	DeviceCode string `json:"device_code"`
}

type BodyCliApproval struct {
	Login string `json:"login"`
	Code  string `json:"code"`
}

type BodyCliRefresh struct {
	RefreshToken string `json:"refresh_token"`
}

type ResponseCliSession struct {
	DeviceCode               string `json:"device_code"`
	UserCode                 string `json:"user_code"`
	VerificationPath         string `json:"verification_path"`
	VerificationPathComplete string `json:"verification_path_complete"`
	ExpiresIn                int    `json:"expires_in"`
	Interval                 int    `json:"interval"`
}

type ResponseCliPending struct {
	Status       string `json:"status"`
	Interval     int    `json:"interval,omitempty"`
	RefreshToken string `json:"refresh_token,omitempty"`
}

type ResponseCliToken struct {
	AccessToken      string `json:"access_token"`
	RefreshToken     string `json:"refresh_token"`
	AccessExpiresIn  int    `json:"access_expires_in"`
	RefreshExpiresIn int    `json:"refresh_expires_in"`
}

type TemplateCliApproval struct {
	App          string
	Login        string
	Code         string
	ApprovalPath string
	CSRFCookie   string
	CSRFHeader   string
}

func PostCliSession(ctx echo.Context) error {
	if shared.GetDebugMode() != shared.DebugModeNone {
		return respondError(ctx, http.StatusConflict, cliDebugRefusal)
	}

	key, err := getCliKey()
	if err != nil {
		ctx.Logger().Errorf("PostCliSession key: %v", err)
		return respondError(ctx, http.StatusInternalServerError, cliInternalError)
	}

	handle, err := getCliSecret()
	if err != nil {
		ctx.Logger().Errorf("PostCliSession handle: %v", err)
		return respondError(ctx, http.StatusInternalServerError, cliInternalError)
	}

	deviceSecret, err := getCliSecret()
	if err != nil {
		ctx.Logger().Errorf("PostCliSession device secret: %v", err)
		return respondError(ctx, http.StatusInternalServerError, cliInternalError)
	}

	userCode := getCliUserCode(key, handle)

	if err := sdb.Instance().DeleteExpiredCliLogins(ctx.Request().Context()); err != nil {
		ctx.Logger().Warnf("PostCliSession expired: %v", err)
	}
	id, err := sdb.Instance().PostCliLogin(ctx.Request().Context(),
		getCliHash(key, cliHashDeviceCode, deviceSecret),
		getCliHash(key, cliHashUserCode, userCode),
		getCliHash(key, cliHashHandle, handle),
		time.Now().Add(cliLoginLifetime))
	if err != nil {
		ctx.Logger().Errorf("PostCliSession insert: %v", err)
		return respondError(ctx, http.StatusInternalServerError, cliInternalError)
	}

	return respondJSON(ctx, http.StatusCreated, ResponseCliSession{
		DeviceCode:               id.String() + cliDeviceCodeJoin + deviceSecret,
		UserCode:                 userCode,
		VerificationPath:         CliPath,
		VerificationPathComplete: CliPath + "?login=" + handle,
		ExpiresIn:                int(cliLoginLifetime.Seconds()),
		Interval:                 cliLoginInterval,
	}, 1)
}

func GetCli(ctx echo.Context) error {
	if shared.GetDebugMode() != shared.DebugModeNone {
		return respondError(ctx, http.StatusConflict, cliDebugRefusal)
	}

	handle := strings.TrimSpace(ctx.QueryParam("login"))

	if cookie, err := ctx.Cookie(cliAccessCookie); err != nil || cookie.Value == "" {
		next := CliPath
		if handle != "" {
			next += "?login=" + url.QueryEscape(handle)
		}
		return Redirect(ctx, http.StatusFound, cliLoginPath+"?next="+url.QueryEscape(next))
	}

	if handle == "" {
		return respondError(ctx, http.StatusBadRequest, "login is required")
	}

	key, err := getCliKey()
	if err != nil {
		ctx.Logger().Errorf("GetCli key: %v", err)
		return respondError(ctx, http.StatusInternalServerError, cliInternalError)
	}

	open, err := sdb.Instance().GetCliLoginOpen(ctx.Request().Context(), getCliHash(key, cliHashHandle, handle))
	if err != nil {
		ctx.Logger().Errorf("GetCli open: %v", err)
		return respondError(ctx, http.StatusInternalServerError, cliInternalError)
	}
	if !open {
		return respondError(ctx, http.StatusNotFound, cliLoginNoneMessage)
	}

	ctx.Response().Header().Set(echo.HeaderContentType, echo.MIMETextHTMLCharsetUTF8)
	ctx.Response().WriteHeader(http.StatusOK)
	return cliApprovalTemplate.Execute(ctx.Response(), TemplateCliApproval{
		App:          utils.Cfg().AppSlug,
		Login:        handle,
		Code:         getCliUserCode(key, handle),
		ApprovalPath: CliPath,
		CSRFCookie:   cliCSRFCookie,
		CSRFHeader:   cliCSRFHeader,
	})
}

func PostCli(ctx echo.Context) error {
	if shared.GetDebugMode() != shared.DebugModeNone {
		return respondError(ctx, http.StatusConflict, cliDebugRefusal)
	}

	csrf := getCliCSRFToken(ctx)
	if csrf == "" || subtle.ConstantTimeCompare([]byte(csrf), []byte(ctx.Request().Header.Get(cliCSRFHeader))) != 1 {
		ctx.Logger().Warnf("PostCli csrf mismatch")
		return respondError(ctx, http.StatusForbidden, "the approval did not carry this browser's own token, so nothing was approved")
	}

	if cookie, err := ctx.Cookie(cliAccessCookie); err != nil || cookie.Value == "" {
		return respondError(ctx, http.StatusUnauthorized, "sign in first")
	}

	refresh, err := ctx.Cookie(cliRefreshCookie)
	if err != nil || refresh.Value == "" {
		return respondError(ctx, http.StatusConflict, "this browser session carries no refresh token, sign out and back in and "+cliRetry)
	}

	var body BodyCliApproval
	if err := ctx.Bind(&body); err != nil {
		return respondError(ctx, http.StatusBadRequest, "Invalid JSON body")
	}
	handle := strings.TrimSpace(body.Login)
	if handle == "" {
		return respondError(ctx, http.StatusBadRequest, "login is required")
	}
	code := getNormalizedCliUserCode(body.Code)
	if code == "" {
		return respondError(ctx, http.StatusBadRequest, "code is required")
	}

	key, err := getCliKey()
	if err != nil {
		ctx.Logger().Errorf("PostCli key: %v", err)
		return respondError(ctx, http.StatusInternalServerError, cliInternalError)
	}

	approved, err := sdb.Instance().PutCliLoginApproval(ctx.Request().Context(), getCliHash(key, cliHashHandle, handle), getCliHash(key, cliHashUserCode, code), refresh.Value)
	if err != nil {
		ctx.Logger().Errorf("PostCli approval: %v", err)
		return respondError(ctx, http.StatusInternalServerError, cliInternalError)
	}
	if !approved {
		return respondError(ctx, http.StatusNotFound, cliLoginNoneMessage)
	}

	return respondJSON(ctx, http.StatusOK, ResponseCliPending{Status: cliStatusApproved}, 1)
}

func PostCliToken(ctx echo.Context) error {
	var body BodyCliToken
	if err := ctx.Bind(&body); err != nil {
		return respondError(ctx, http.StatusBadRequest, "Invalid JSON body")
	}
	rawID, deviceSecret, found := strings.Cut(strings.TrimSpace(body.DeviceCode), cliDeviceCodeJoin)
	loginID, parseErr := uuid.Parse(rawID)
	if !found || parseErr != nil || deviceSecret == "" {
		return respondError(ctx, http.StatusBadRequest, "device_code is required")
	}

	key, err := getCliKey()
	if err != nil {
		ctx.Logger().Errorf("PostCliToken key: %v", err)
		return respondError(ctx, http.StatusInternalServerError, cliInternalError)
	}

	gone := "the login request expired or was already collected, " + cliRetry
	login, err := sdb.Instance().GetCliLogin(ctx.Request().Context(), loginID)
	if err != nil {
		ctx.Logger().Errorf("PostCliToken read: %v", err)
		return respondError(ctx, http.StatusInternalServerError, cliInternalError)
	}
	if login == nil || !hmac.Equal([]byte(login.DeviceCodeHash), []byte(getCliHash(key, cliHashDeviceCode, deviceSecret))) {
		return respondError(ctx, http.StatusGone, gone)
	}
	if !login.Approved {
		return respondJSON(ctx, http.StatusOK, ResponseCliPending{Status: cliStatusPending, Interval: cliLoginInterval}, 1)
	}

	refreshToken, collected, err := sdb.Instance().DeleteCliLogin(ctx.Request().Context(), login.ID, login.DeviceCodeHash)
	if err != nil {
		ctx.Logger().Errorf("PostCliToken collect: %v", err)
		return respondError(ctx, http.StatusInternalServerError, cliInternalError)
	}
	if !collected {
		return respondError(ctx, http.StatusGone, gone)
	}

	return respondJSON(ctx, http.StatusOK, ResponseCliPending{Status: cliStatusApproved, RefreshToken: refreshToken}, 1)
}

func PostCliRefresh(ctx echo.Context) error {
	tokens, err := postCliRefreshToken(ctx)
	if err != nil || tokens == nil {
		return err
	}

	return respondJSON(ctx, http.StatusOK, ResponseCliToken{
		AccessToken:      tokens.AccessToken,
		RefreshToken:     tokens.RefreshToken,
		AccessExpiresIn:  tokens.AccessExpiresIn,
		RefreshExpiresIn: tokens.RefreshExpiresIn,
	}, 1)
}

func PostCliLogout(ctx echo.Context) error {
	tokens, err := postCliRefreshToken(ctx)
	if err != nil || tokens == nil {
		return err
	}

	statusCode, err := client.GetLogout(ctx.Request().Context(), tokens.AccessToken)
	if err != nil {
		ctx.Logger().Errorf("PostCliLogout: %v", err)
		return respondError(ctx, http.StatusInternalServerError, cliInternalError)
	}
	if statusCode >= http.StatusBadRequest {
		ctx.Logger().Errorf("PostCliLogout: auth server returned %d", statusCode)
		return respondError(ctx, http.StatusBadGateway, "the auth server refused the sign out")
	}

	return ctx.NoContent(http.StatusNoContent)
}

func postCliRefreshToken(ctx echo.Context) (*client.ResponseToken, error) {
	if shared.GetDebugMode() != shared.DebugModeNone {
		return nil, respondError(ctx, http.StatusConflict, cliDebugRefusal)
	}

	var body BodyCliRefresh
	if err := ctx.Bind(&body); err != nil {
		return nil, respondError(ctx, http.StatusBadRequest, "Invalid JSON body")
	}
	if strings.TrimSpace(body.RefreshToken) == "" {
		return nil, respondError(ctx, http.StatusBadRequest, "refresh_token is required")
	}

	slug := utils.Cfg().AppSlug
	tokenURL, err := client.TokenURLFor(slug)
	if err != nil {
		ctx.Logger().Errorf("postCliRefreshToken token url: %v", err)
		return nil, respondError(ctx, http.StatusInternalServerError, cliInternalError)
	}

	tokens, err := client.PostRefreshToken(ctx.Request().Context(), tokenURL, slug, utils.Cfg().AppSecret, body.RefreshToken)
	if err != nil {
		ctx.Logger().Warnf("postCliRefreshToken: %v", err)
		return nil, respondError(ctx, http.StatusUnauthorized, "the stored login is no longer valid, "+cliRetry)
	}

	return tokens, nil
}

func getCliKey() ([]byte, error) {
	secret := utils.Cfg().AppSecret
	if secret == "" {
		return nil, errors.New("SAUTH_APP_SECRET is not set, so a cli login cannot be hashed")
	}
	return []byte(secret), nil
}

func getCliSecret() (string, error) {
	buf := make([]byte, cliSecretBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func getCliHash(key []byte, purpose string, value string) string {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(purpose))
	mac.Write([]byte{0})
	mac.Write([]byte(value))
	return hex.EncodeToString(mac.Sum(nil))
}

func getCliUserCode(key []byte, handle string) string {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(cliHashDeriveCode))
	mac.Write([]byte{0})
	mac.Write([]byte(handle))
	remaining := new(big.Int).SetBytes(mac.Sum(nil))
	base := big.NewInt(int64(len(cliUserCodeChars)))
	digit := new(big.Int)
	groups := make([]string, 0, cliUserCodeGroups)
	for group := 0; group < cliUserCodeGroups; group++ {
		var builder strings.Builder
		for position := 0; position < cliUserCodeLength; position++ {
			remaining.DivMod(remaining, base, digit)
			builder.WriteByte(cliUserCodeChars[digit.Int64()])
		}
		groups = append(groups, builder.String())
	}
	return strings.Join(groups, "-")
}

func getNormalizedCliUserCode(value string) string {
	trimmed := strings.ToUpper(strings.TrimSpace(value))
	stripped := strings.ReplaceAll(strings.ReplaceAll(trimmed, "-", ""), " ", "")
	if len(stripped) != cliUserCodeGroups*cliUserCodeLength {
		return trimmed
	}
	groups := make([]string, 0, cliUserCodeGroups)
	for group := 0; group < cliUserCodeGroups; group++ {
		groups = append(groups, stripped[group*cliUserCodeLength:(group+1)*cliUserCodeLength])
	}
	return strings.Join(groups, "-")
}

func getCliCSRFToken(ctx echo.Context) string {
	cookie, err := ctx.Cookie(cliCSRFCookie)
	if err != nil {
		return ""
	}
	return cookie.Value
}
