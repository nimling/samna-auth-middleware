package sam

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/nimling/samna-auth-middleware/v2/shared"
)

func getRoutedModels() []any {
	claimID := uuid.New()
	return []any{
		&shared.Claim{},
		&shared.ClaimObject{ClaimID: claimID},
		&shared.UserClaim{ClaimID: claimID},
		&shared.SamnaUser{},
		&shared.Organization{},
		&shared.Location{},
		&shared.SamnaSecret{},
		&shared.UserRelation{},
	}
}

func TestEveryRoutedWriteReachesTheProphet(t *testing.T) {
	e := echo.New()
	RegisterEndpoints(e.Group("/sam"))
	registered := make(map[string]bool)
	for _, r := range e.Routes() {
		registered[r.Method+" "+r.Path] = true
	}
	for _, model := range getRoutedModels() {
		insert, err := shared.EndpointFor(model, uuid.Nil)
		if err != nil {
			t.Fatalf("%T: %v", model, err)
		}
		record, err := shared.EndpointFor(model, uuid.New())
		if err != nil {
			t.Fatalf("%T: %v", model, err)
		}
		cases := []struct {
			method string
			path   string
		}{
			{http.MethodPost, insert},
			{http.MethodPatch, record},
			{http.MethodDelete, record},
		}
		for _, c := range cases {
			rec := getRoutePath(e, c.method, "/sam/"+c.path)
			if !registered[c.method+" "+rec] {
				t.Errorf("%T: disciple sends %s /sam/%s, the prophet registers no such route", model, c.method, c.path)
			}
		}
	}
}

func getRoutePath(e *echo.Echo, method, path string) string {
	ctx := e.NewContext(nil, nil)
	e.Router().Find(method, path, ctx)
	return ctx.Path()
}
