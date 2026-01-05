package main

import (
	"context"
	"crypto/rsa"
	"fmt"
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/nimling/samna-auth-middleware/hook"
)

type MockRepository struct{}

func (r *MockRepository) UpsertOrganization(ctx context.Context, org hook.Organization) error {
	fmt.Printf("[DB] Upserting Organization: ID=%s\n", org.ID)
	return nil
}

func (r *MockRepository) DeleteOrganization(ctx context.Context, org hook.Organization) error {
	fmt.Printf("[DB] Deleting Organization: ID=%s\n", org.ID)
	return nil
}

func (r *MockRepository) CreateLocation(ctx context.Context, loc hook.Location) error {
	fmt.Printf("[DB] Creating location: ID=%s\n", loc.ID)
	return nil
}

func (r *MockRepository) UpdateLocation(ctx context.Context, loc hook.Location) error {
	fmt.Printf("[DB] Updating location: ID=%s\n", loc.ID)
	return nil
}

type WebhookController struct {
	repo *MockRepository
}

func (c *WebhookController) HandleWebhook(ctx echo.Context) error {
	var p hook.Payload
	if err := ctx.Bind(&p); err != nil {
		return ctx.String(http.StatusBadRequest, "Malformed payload")
	}

	handlers := hook.Handlers{
		Organization: hook.ActionHandler[hook.Organization]{
			OnCreate: c.repo.UpsertOrganization,
			OnUpdate: c.repo.UpsertOrganization,
			OnDelete: c.repo.DeleteOrganization,
		},
		Location: hook.ActionHandler[hook.Location]{
			OnCreate: c.repo.CreateLocation,
			OnUpdate: c.repo.UpdateLocation,
		},
		// If no other handler func is set, UnmarshalAndHandle will just do nothing
	}

	err := p.UnmarshalAndHandle(ctx.Request().Context(), handlers)

	if err != nil {
		return ctx.String(http.StatusInternalServerError, "Internal Server Error")
	}

	return ctx.String(http.StatusOK, "OK")
}

func main() {
	e := echo.New()
	e.Use(middleware.Logger())
	e.Use(middleware.Recover())

	ctrl := &WebhookController{
		repo: &MockRepository{},
	}

	publicKey := &rsa.PublicKey{} // get actual Client's Public Key

	e.POST("/webhook", ctrl.HandleWebhook, hook.WebhookAuthMiddleware(publicKey))

	fmt.Println("Webhook consumer example running on :8080")
	e.Logger.Fatal(e.Start(":8080"))
}
