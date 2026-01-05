package hook

import (
	"bytes"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"

	"github.com/labstack/echo/v4"
)

func WebhookAuthMiddleware(pubKey *rsa.PublicKey) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(ctx echo.Context) error {
			if pubKey == nil {
				panic("public key is required")
			}

			signature := ctx.Request().Header.Get("X-Signature-RSA")
			if signature == "" {
				ctx.Logger().Error("Missing signature")
				return ctx.String(http.StatusUnauthorized, "Unauthorized")
			}

			bodyBytes, err := io.ReadAll(ctx.Request().Body)
			if err != nil {
				ctx.Logger().Errorf("Failed to read body: %v", err)
				return ctx.JSON(http.StatusInternalServerError, "Internal Server Error")
			}

			// set body in request to then read it again in handler
			ctx.Request().Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

			if err = verifySignature(bodyBytes, signature, pubKey); err != nil {
				ctx.Logger().Errorf("Failed to verify signature: %v", err)
				return ctx.String(http.StatusForbidden, "Forbidden")
			}

			return next(ctx)
		}
	}
}

func verifySignature(payload []byte, signatureHex string, publicKey *rsa.PublicKey) error {
	sig, err := hex.DecodeString(signatureHex)
	if err != nil {
		return fmt.Errorf("decode signature hex: %w", err)
	}

	hashed := sha256.Sum256(payload)

	err = rsa.VerifyPKCS1v15(publicKey, crypto.SHA256, hashed[:], sig)
	if err != nil {
		return fmt.Errorf("signature verification: %w", err)
	}

	return nil
}
