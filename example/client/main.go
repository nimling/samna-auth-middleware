package main

import (
	"errors"
	"fmt"
	"net/http"
	"os"

	"github.com/google/uuid"
	"github.com/joho/godotenv"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/nimling/samna-auth-middleware/client"
	sauthmw "github.com/nimling/samna-auth-middleware/middleware"
)

func main() {
	// Load environment variables
	_ = godotenv.Load(".env")

	// Initialize the client
	apiURL := os.Getenv("SAMNA_AUTH_SERVER_API_URL")
	clientSlug := os.Getenv("SAMNA_AUTH_CLIENT_SLUG")
	clientSecret := os.Getenv("SAMNA_AUTH_CLIENT_SECRET")

	if apiURL == "" || clientSlug == "" || clientSecret == "" {
		panic("Missing required environment variables: SAMNA_AUTH_SERVER_API_URL, SAMNA_AUTH_CLIENT_SLUG, SAMNA_AUTH_CLIENT_SECRET")
	}

	sauthClient := client.NewSAuthClient(apiURL, clientSlug, clientSecret)

	// Configure echo
	e := echo.New()
	e.Use(middleware.Logger())
	e.Use(middleware.Recover())

	// Initialize auth middleware to get access tokens
	authMiddleware := sauthmw.AuthMiddlewareEnv()

	// Example: Update user
	e.PUT("/user/:id", func(ctx echo.Context) error {
		userID, err := uuid.Parse(ctx.Param("id"))
		if err != nil {
			return ctx.String(http.StatusBadRequest, "Invalid user ID")
		}

		// Get access token from context (set by auth middleware)
		accessToken, ok := ctx.Get(sauthmw.AccessTokenKey).(string)
		if !ok {
			return ctx.String(http.StatusUnauthorized, "Missing access token")
		}

		// Get request ID if available
		requestID := ctx.Request().Header.Get("X-Request-ID")
		if requestID == "" {
			requestID = uuid.New().String()
		}

		// Prepare request metadata
		meta := client.RequestMetadata{
			AccessToken: accessToken,
			RequestID:   requestID,
			UserIP:      ctx.RealIP(),
		}

		// Example: Update user with some fields
		body := client.UpdateUserBody{
			FirstName: stringPtr("John"),
			LastName:  stringPtr("Doe"),
			Email:     stringPtr("john.doe@example.com"),
		}

		err = sauthClient.PutUser(ctx.Request().Context(), meta, userID, body)
		if err != nil {
			return handleClientError(ctx, err)
		}

		return ctx.NoContent(http.StatusNoContent)
	}, authMiddleware)

	// Example: Create organization
	e.POST("/organization", func(ctx echo.Context) error {
		accessToken, ok := ctx.Get(sauthmw.AccessTokenKey).(string)
		if !ok {
			return ctx.String(http.StatusUnauthorized, "Missing access token")
		}

		requestID := ctx.Request().Header.Get("X-Request-ID")
		if requestID == "" {
			requestID = uuid.New().String()
		}

		meta := client.RequestMetadata{
			AccessToken: accessToken,
			RequestID:   requestID,
			UserIP:      ctx.RealIP(),
		}

		// Example: Create a new organization
		orgType := "child"
		orgLevel := 0
		body := client.CreateOrganizationBody{
			Name:        stringPtr("Example Organization"),
			Description: stringPtr("An example organization created via client library"),
			Type:        &orgType,
			Level:       &orgLevel,
		}

		err := sauthClient.PostOrganization(ctx.Request().Context(), meta, body)
		if err != nil {
			return handleClientError(ctx, err)
		}

		return ctx.NoContent(http.StatusCreated)
	}, authMiddleware)

	// Example: Update organization
	e.PUT("/organization/:id", func(ctx echo.Context) error {
		orgID, err := uuid.Parse(ctx.Param("id"))
		if err != nil {
			return ctx.String(http.StatusBadRequest, "Invalid organization ID")
		}

		accessToken, ok := ctx.Get(sauthmw.AccessTokenKey).(string)
		if !ok {
			return ctx.String(http.StatusUnauthorized, "Missing access token")
		}

		requestID := ctx.Request().Header.Get("X-Request-ID")
		if requestID == "" {
			requestID = uuid.New().String()
		}

		meta := client.RequestMetadata{
			AccessToken: accessToken,
			RequestID:   requestID,
			UserIP:      ctx.RealIP(),
		}

		body := client.UpdateOrganizationBody{
			Name:        stringPtr("Updated Organization Name"),
			Description: stringPtr("Updated description"),
		}

		err = sauthClient.PutOrganization(ctx.Request().Context(), meta, orgID, body)
		if err != nil {
			return handleClientError(ctx, err)
		}

		return ctx.NoContent(http.StatusNoContent)
	}, authMiddleware)

	// Example: Delete organization
	e.DELETE("/organization/:id", func(ctx echo.Context) error {
		orgID, err := uuid.Parse(ctx.Param("id"))
		if err != nil {
			return ctx.String(http.StatusBadRequest, "Invalid organization ID")
		}

		accessToken, ok := ctx.Get(sauthmw.AccessTokenKey).(string)
		if !ok {
			return ctx.String(http.StatusUnauthorized, "Missing access token")
		}

		requestID := ctx.Request().Header.Get("X-Request-ID")
		if requestID == "" {
			requestID = uuid.New().String()
		}

		meta := client.RequestMetadata{
			AccessToken: accessToken,
			RequestID:   requestID,
			UserIP:      ctx.RealIP(),
		}

		err = sauthClient.DeleteOrganization(ctx.Request().Context(), meta, orgID)
		if err != nil {
			return handleClientError(ctx, err)
		}

		return ctx.NoContent(http.StatusNoContent)
	}, authMiddleware)

	// Example: Create location
	e.POST("/location", func(ctx echo.Context) error {
		accessToken, ok := ctx.Get(sauthmw.AccessTokenKey).(string)
		if !ok {
			return ctx.String(http.StatusUnauthorized, "Missing access token")
		}

		requestID := ctx.Request().Header.Get("X-Request-ID")
		if requestID == "" {
			requestID = uuid.New().String()
		}

		meta := client.RequestMetadata{
			AccessToken: accessToken,
			RequestID:   requestID,
			UserIP:      ctx.RealIP(),
		}

		// Example: Create a new location
		// Note: OwnerID is required - you would get this from your system
		ownerID := uuid.MustParse("00000000-0000-0000-0000-000000000000") // Replace with actual owner ID
		locationType := "office"
		locationLevel := 0
		longitude := 10.7522
		latitude := 59.9139

		body := client.CreateLocationBody{
			OwnerID:     &ownerID,
			Name:        stringPtr("Example Location"),
			Description: stringPtr("An example location created via client library"),
			Type:        &locationType,
			Level:       &locationLevel,
			Longitude:   &longitude,
			Latitude:    &latitude,
		}

		err := sauthClient.PostLocation(ctx.Request().Context(), meta, body)
		if err != nil {
			return handleClientError(ctx, err)
		}

		return ctx.NoContent(http.StatusCreated)
	}, authMiddleware)

	// Example: Update location
	e.PUT("/location/:id", func(ctx echo.Context) error {
		locID, err := uuid.Parse(ctx.Param("id"))
		if err != nil {
			return ctx.String(http.StatusBadRequest, "Invalid location ID")
		}

		accessToken, ok := ctx.Get(sauthmw.AccessTokenKey).(string)
		if !ok {
			return ctx.String(http.StatusUnauthorized, "Missing access token")
		}

		requestID := ctx.Request().Header.Get("X-Request-ID")
		if requestID == "" {
			requestID = uuid.New().String()
		}

		meta := client.RequestMetadata{
			AccessToken: accessToken,
			RequestID:   requestID,
			UserIP:      ctx.RealIP(),
		}

		body := client.UpdateLocationBody{
			Name:        stringPtr("Updated Location Name"),
			Description: stringPtr("Updated location description"),
		}

		err = sauthClient.PutLocation(ctx.Request().Context(), meta, locID, body)
		if err != nil {
			return handleClientError(ctx, err)
		}

		return ctx.NoContent(http.StatusNoContent)
	}, authMiddleware)

	// Example: Delete location
	e.DELETE("/location/:id", func(ctx echo.Context) error {
		locID, err := uuid.Parse(ctx.Param("id"))
		if err != nil {
			return ctx.String(http.StatusBadRequest, "Invalid location ID")
		}

		accessToken, ok := ctx.Get(sauthmw.AccessTokenKey).(string)
		if !ok {
			return ctx.String(http.StatusUnauthorized, "Missing access token")
		}

		requestID := ctx.Request().Header.Get("X-Request-ID")
		if requestID == "" {
			requestID = uuid.New().String()
		}

		meta := client.RequestMetadata{
			AccessToken: accessToken,
			RequestID:   requestID,
			UserIP:      ctx.RealIP(),
		}

		err = sauthClient.DeleteLocation(ctx.Request().Context(), meta, locID)
		if err != nil {
			return handleClientError(ctx, err)
		}

		return ctx.NoContent(http.StatusNoContent)
	}, authMiddleware)

	// Example: Create claim
	e.POST("/claim", func(ctx echo.Context) error {
		accessToken, ok := ctx.Get(sauthmw.AccessTokenKey).(string)
		if !ok {
			return ctx.String(http.StatusUnauthorized, "Missing access token")
		}

		requestID := ctx.Request().Header.Get("X-Request-ID")
		if requestID == "" {
			requestID = uuid.New().String()
		}

		meta := client.RequestMetadata{
			AccessToken: accessToken,
			RequestID:   requestID,
			UserIP:      ctx.RealIP(),
		}

		// Example: Create a new claim
		ownerID := uuid.MustParse("00000000-0000-0000-0000-000000000000") // Replace with actual owner ID
		claimType := client.CLAIM_TYPE_SAT
		accessLevel := client.ACCESS_LEVEL_WRITE

		body := client.CreateClaimBody{
			Name:        stringPtr("Example Claim"),
			Description: stringPtr("An example claim created via client library"),
			Type:        &claimType,
			AccessLevel: &accessLevel,
			OwnerID:     &ownerID,
		}

		err := sauthClient.PostClaim(ctx.Request().Context(), meta, body)
		if err != nil {
			return handleClientError(ctx, err)
		}

		return ctx.NoContent(http.StatusCreated)
	}, authMiddleware)

	// Example: Update claim
	e.PUT("/claim/:id", func(ctx echo.Context) error {
		claimID, err := uuid.Parse(ctx.Param("id"))
		if err != nil {
			return ctx.String(http.StatusBadRequest, "Invalid claim ID")
		}

		accessToken, ok := ctx.Get(sauthmw.AccessTokenKey).(string)
		if !ok {
			return ctx.String(http.StatusUnauthorized, "Missing access token")
		}

		requestID := ctx.Request().Header.Get("X-Request-ID")
		if requestID == "" {
			requestID = uuid.New().String()
		}

		meta := client.RequestMetadata{
			AccessToken: accessToken,
			RequestID:   requestID,
			UserIP:      ctx.RealIP(),
		}

		accessLevel := client.ACCESS_LEVEL_READ
		body := client.UpdateClaimBody{
			Name:        stringPtr("Updated Claim Name"),
			Description: stringPtr("Updated claim description"),
			AccessLevel: &accessLevel,
		}

		err = sauthClient.PutClaim(ctx.Request().Context(), meta, claimID, body)
		if err != nil {
			return handleClientError(ctx, err)
		}

		return ctx.NoContent(http.StatusNoContent)
	}, authMiddleware)

	// Example: Delete claim
	e.DELETE("/claim/:id", func(ctx echo.Context) error {
		claimID, err := uuid.Parse(ctx.Param("id"))
		if err != nil {
			return ctx.String(http.StatusBadRequest, "Invalid claim ID")
		}

		accessToken, ok := ctx.Get(sauthmw.AccessTokenKey).(string)
		if !ok {
			return ctx.String(http.StatusUnauthorized, "Missing access token")
		}

		requestID := ctx.Request().Header.Get("X-Request-ID")
		if requestID == "" {
			requestID = uuid.New().String()
		}

		meta := client.RequestMetadata{
			AccessToken: accessToken,
			RequestID:   requestID,
			UserIP:      ctx.RealIP(),
		}

		err = sauthClient.DeleteClaim(ctx.Request().Context(), meta, claimID)
		if err != nil {
			return handleClientError(ctx, err)
		}

		return ctx.NoContent(http.StatusNoContent)
	}, authMiddleware)

	fmt.Println("Client library example running on :8080")
	e.Logger.Fatal(e.Start(":8080"))
}

// handleClientError handles errors from the client library and returns appropriate HTTP responses
func handleClientError(ctx echo.Context, err error) error {
	switch {
	case errors.Is(err, client.ErrBadRequest):
		return ctx.String(http.StatusBadRequest, "Bad Request")
	case errors.Is(err, client.ErrForbidden):
		return ctx.String(http.StatusForbidden, "Forbidden")
	case errors.Is(err, client.ErrNotFound):
		return ctx.String(http.StatusNotFound, "Not Found")
	default:
		ctx.Logger().Errorf("Client error: %v", err)
		return ctx.String(http.StatusInternalServerError, "Internal Server Error")
	}
}

// stringPtr is a helper function to create a pointer to a string
func stringPtr(s string) *string {
	return &s
}

