package shared_test

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
	sdb "github.com/nimling/samna-auth-middleware/v2/shared"
)

func boundOrganization(t *testing.T, body string) *sdb.Organization {
	t.Helper()
	req := httptest.NewRequest("POST", "/organization", strings.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	org := &sdb.Organization{}
	if err := org.Bind(echo.New().NewContext(req, httptest.NewRecorder())); err != nil {
		t.Fatalf("bind: %v", err)
	}
	return org
}

func TestOrganizationValidateAcceptsKind(t *testing.T) {
	if err := boundOrganization(t, `{"name":"Acme","kind":"group"}`).ValidatePost(); err != nil {
		t.Fatalf("expected kind to be accepted on create, got %v", err)
	}
	if err := boundOrganization(t, `{"name":"Acme","kind":"group"}`).ValidatePut(); err != nil {
		t.Fatalf("expected kind to be accepted on update, got %v", err)
	}
	if err := boundOrganization(t, `{"kind":null}`).ValidatePatch(); err != nil {
		t.Fatalf("expected kind to be cleared on patch, got %v", err)
	}
}

func TestOrganizationValidateRejectsType(t *testing.T) {
	for _, value := range []string{"entity", "company", "system"} {
		body := `{"name":"Acme","type":"` + value + `"}`
		if err := boundOrganization(t, body).ValidatePost(); err == nil || err.Error() != "type not allowed" {
			t.Fatalf("expected type %s to be refused on create, got %v", value, err)
		}
		if err := boundOrganization(t, body).ValidatePut(); err == nil || err.Error() != "type not allowed" {
			t.Fatalf("expected type %s to be refused on update, got %v", value, err)
		}
		if err := boundOrganization(t, body).ValidatePatch(); err == nil || err.Error() != "type not allowed" {
			t.Fatalf("expected type %s to be refused on patch, got %v", value, err)
		}
	}
}

func TestSealClaimOptionsHashesTheValueAndShowsItOnce(t *testing.T) {
	sealed, value, err := sdb.SealClaimOptions([]byte(`{"samk": {"header": true}}`))
	if err != nil || value == "" {
		t.Fatalf("a samk without a value is minted and handed back once, got %q %v", value, err)
	}
	var options sdb.ClaimOptions
	if err := json.Unmarshal(sealed, &options); err != nil || options.Samk == nil || options.Samk.Value != "" || options.Samk.Hash != sdb.GetCodeHash(value) || !options.Samk.Header {
		t.Fatalf("the sealed options carry the sha256 of the value and never the value, got %s %v", sealed, err)
	}
	shown := sdb.ShowClaimOptions(sealed, value)
	if !strings.Contains(string(shown), value) || strings.Contains(string(sealed), value) {
		t.Fatalf("the value is shown on the answer and nowhere else, got %s", shown)
	}
	chosen, plain, err := sdb.SealClaimOptions([]byte(`{"samk": {"value": "1234", "query": true}}`))
	if err != nil || plain != "1234" || !strings.Contains(string(chosen), sdb.GetCodeHash("1234")) || strings.Contains(string(chosen), `"value"`) {
		t.Fatalf("a chosen value is hashed the same way, got %s %q %v", chosen, plain, err)
	}
	if none, plain, err := sdb.SealClaimOptions(nil); err != nil || none != nil || plain != "" {
		t.Fatalf("no options seal to nothing, got %s %q %v", none, plain, err)
	}
}
