package smw

import (
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	discipledb "github.com/nimling/samna-auth-middleware/disciple/sdb"
	"github.com/nimling/samna-auth-middleware/dpop"
)

func authenticateBearer(ctx echo.Context, config *AuthMiddlewareConfig, keyFunc jwt.Keyfunc) error {
	authHeader := ctx.Request().Header.Get(echo.HeaderAuthorization)
	if authHeader == "" || !strings.HasPrefix(authHeader, "Bearer ") {
		return ErrorCredentialMissing
	}
	token := strings.TrimPrefix(authHeader, "Bearer ")

	claimUser, err := ValidateToken(ctx.Request(), token, config, keyFunc)
	if err != nil {
		ctx.Logger().Errorf("Failed to validate the bearer access_token: %v", err)
		return fmt.Errorf("validate bearer token: %w: %w", err, ErrorUnauthorized)
	}

	ctx.Set(UserIDKey, claimUser.ID)
	ctx.Set(AccessTokenKey, token)
	return nil
}

func ValidateToken(r *http.Request, tokenString string, config *AuthMiddlewareConfig, keyFunc jwt.Keyfunc) (*ClaimUser, error) {
	var claims TokenClaims
	token, err := jwt.ParseWithClaims(tokenString, &claims, keyFunc)

	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return nil, ErrorTokenExpired
		}

		return nil, fmt.Errorf("parse token: %v", err)
	}

	if !token.Valid {
		return nil, errors.New("invalid token")
	}

	if claims.Issuer != config.ExpectedTokenIssuer {
		return nil, fmt.Errorf("unexpected token issuer: [%v], expected: [%v]", claims.Issuer, config.ExpectedTokenIssuer)
	}

	if !slices.Contains(claims.Audience, config.ClientSlug) {
		return nil, fmt.Errorf("token audience %v does not include %q", claims.Audience, config.ClientSlug)
	}

	if claims.CNF != nil {
		if claims.CNF.JKT != "" {
			proof, proofErr := dpop.Verify(r, r.Header.Get(dpop.Header), tokenString, dpop.PostgresReplayStore{DB: discipledb.Instance().DB.DB})
			if proofErr != nil {
				return nil, fmt.Errorf("dpop proof: %w", proofErr)
			}
			if proof == nil {
				return nil, errors.New("cnf.jkt requires a dpop proof")
			}
			if proof.JKT != claims.CNF.JKT {
				return nil, errors.New("dpop jkt does not match cnf.jkt")
			}
		} else if claims.CNF.X5tS256 != "" {
			if dpop.TLSCertThumbprint(r) != claims.CNF.X5tS256 {
				return nil, errors.New("mtls thumbprint does not match cnf.x5t#S256")
			}
		}
	}

	userID, err := uuid.Parse(claims.Subject)
	if err != nil {
		return nil, fmt.Errorf("claims subject is not a valid UUID: %v", err)
	}

	user := &ClaimUser{
		ID: userID,
	}

	return user, nil
}

func claimsFromTokenUnsafe(token string) (*ClaimUser, error) {
	var claims TokenClaims
	_, _, err := new(jwt.Parser).ParseUnverified(token, &claims)
	if err != nil {
		return nil, fmt.Errorf("parse token: %w", err)
	}

	userID, err := uuid.Parse(claims.Subject)
	if err != nil {
		return nil, fmt.Errorf("parse user id: %w", err)
	}

	return &ClaimUser{ID: userID}, nil
}
