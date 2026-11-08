package echo_test

import (
	"net/http"
	"testing"

	pecho "github.com/nimling/samna-auth-middleware/v2/prophet/echo"
)

func TestPostClaimRequiresRootID(t *testing.T) {
	body := `{"name":"Reader","access":4,"owner_id":"10000000-0000-4000-8000-000000000001"}`
	if code := postJSON(t, body, pecho.PostClaim); code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 when root_id is absent, because sa_root_id references an organization", code)
	}
}
