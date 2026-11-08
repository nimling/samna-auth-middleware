//go:build dbtest

package smw_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	sdb "github.com/nimling/samna-auth-middleware/disciple/sdb"
	"github.com/nimling/samna-auth-middleware/smw"
	"golang.org/x/crypto/bcrypt"
)

func postClient(t *testing.T, clientID uuid.UUID, secret string) {
	t.Helper()
	if sdb.Instance() == nil {
		if _, err := sdb.Init(echo.New().Logger); err != nil {
			t.Fatalf("connect: %v", err)
		}
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(secret), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("hash secret: %v", err)
	}
	owner := uuid.New()
	if _, err := sdb.Instance().ExecContext(context.Background(), `
		INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
		VALUES (?0, 'Client owner', 'company', ?0, ?0, 0, ?0);
		INSERT INTO claimius.samna_client (name, client_id, secret_hash, sa_owner_id) VALUES ('client auth', ?1, ?2, ?0);
	`, owner, clientID, string(hash)); err != nil {
		t.Fatalf("seed client: %v", err)
	}
}

func putClient(t *testing.T, clientID uuid.UUID, set string, value any) {
	t.Helper()
	if _, err := sdb.Instance().ExecContext(context.Background(),
		`UPDATE claimius.samna_client SET `+set+` = ? WHERE client_id = ?`, value, clientID); err != nil {
		t.Fatalf("update client: %v", err)
	}
}

func getClientStatus(clientID uuid.UUID, secret string) int {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.SetBasicAuth(clientID.String(), secret)
	rec := httptest.NewRecorder()
	handler := smw.ClientAuthMiddleware()(func(c echo.Context) error { return c.NoContent(http.StatusOK) })
	if err := handler(e.NewContext(req, rec)); err != nil {
		e.HTTPErrorHandler(err, e.NewContext(req, rec))
	}
	return rec.Code
}

func TestClientAuthStopsARotatedOrRevokedSecretOnTheNextRequest(t *testing.T) {
	clientID := uuid.New()
	postClient(t, clientID, "first")

	for range 2 {
		if code := getClientStatus(clientID, "first"); code != http.StatusOK {
			t.Fatalf("the stored secret must authenticate, got %d", code)
		}
	}

	hash, err := bcrypt.GenerateFromPassword([]byte("second"), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("hash secret: %v", err)
	}
	putClient(t, clientID, "secret_hash", string(hash))
	if code := getClientStatus(clientID, "first"); code == http.StatusOK {
		t.Fatal("a rotated secret must stop working on the next request")
	}
	if code := getClientStatus(clientID, "second"); code != http.StatusOK {
		t.Fatalf("the new secret must authenticate, got %d", code)
	}

	putClient(t, clientID, "status", "revoked")
	if code := getClientStatus(clientID, "second"); code == http.StatusOK {
		t.Fatal("a revoked client must stop working on the next request")
	}
}
