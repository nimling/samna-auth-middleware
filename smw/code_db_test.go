//go:build dbtest

package smw_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	sdb "github.com/nimling/samna-auth-middleware/v2/disciple/sdb"
	"github.com/nimling/samna-auth-middleware/v2/shared"
	"github.com/nimling/samna-auth-middleware/v2/smw"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

type codeFixture struct {
	owner     uuid.UUID
	org       uuid.UUID
	hidden    uuid.UUID
	readers   uuid.UUID
	ownerAC   *shared.AccessClaim
	guestUser uuid.UUID
}

func getCodeFixture(t *testing.T) codeFixture {
	t.Helper()
	ctx := context.Background()
	if sdb.Instance() == nil {
		if _, err := sdb.Init(echo.New().Logger); err != nil {
			t.Fatalf("connect: %v", err)
		}
	}
	db := sdb.Instance()
	routes := db.RouteToProphet
	db.RouteToProphet = false
	t.Cleanup(func() { db.RouteToProphet = routes })

	f := codeFixture{owner: uuid.New(), org: uuid.New(), hidden: uuid.New()}
	if _, err := db.ExecContext(ctx, `SELECT claimius.upsert_app_user(p_user_id => ?, p_first_name => 'Owner')`, f.owner); err != nil {
		t.Fatalf("owner: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
		VALUES (?0, 'Shared', 'company', ?0, ?0, 0, ?1), (?2, 'Hidden', 'company', ?2, ?2, 0, ?1)
	`, f.org, f.owner, f.hidden); err != nil {
		t.Fatalf("organizations: %v", err)
	}
	var owners uuid.UUID
	if err := db.NewRaw(`
		SELECT (claimius.insert_claim(p_name => 'Owners', p_description => NULL, p_sa_access => 15,
		        p_sa_owner_id => ?0, p_sa_root_id => ?0, p_sa_created_by => ?1) -> 'claim' ->> 'id')::UUID
	`, f.org, f.owner).Scan(ctx, &owners); err != nil {
		t.Fatalf("owner claim: %v", err)
	}
	if err := db.NewRaw(`
		SELECT (claimius.insert_claim(p_name => 'Readers', p_description => NULL, p_sa_access => 15,
		        p_sa_owner_id => ?0, p_sa_root_id => ?0, p_sa_created_by => ?1) -> 'claim' ->> 'id')::UUID
	`, f.org, f.owner).Scan(ctx, &f.readers); err != nil {
		t.Fatalf("reader claim: %v", err)
	}
	for _, c := range []uuid.UUID{owners, f.readers} {
		if _, err := db.ExecContext(ctx, `
			SELECT claimius.insert_claim_object(p_claim_id => ?, p_sa_owner_id => ?, p_sa_root_id => ?, p_sa_created_by => ?,
			       p_object_id => ?, p_object_type => 'claimius.organization', p_inherits => TRUE)
		`, c, f.org, f.org, f.owner, f.org); err != nil {
			t.Fatalf("bind: %v", err)
		}
	}
	if _, err := db.ExecContext(ctx, `SELECT claimius.insert_user_claim(p_claim_id => ?, p_user_id => ?, p_sa_owner_id => ?, p_sa_created_by => ?)`,
		owners, f.owner, f.org, f.owner); err != nil {
		t.Fatalf("grant owner: %v", err)
	}
	ac, err := db.MutationAccess(ctx, f.owner, f.readers, shared.OBJECT_TYPE_CLAIM, shared.ACCESS_OWNER)
	if err != nil {
		t.Fatalf("the owner must own the reader claim: %v", err)
	}
	f.ownerAC = ac
	return f
}

func (f codeFixture) putGuest(t *testing.T, mode shared.GuestMode) {
	t.Helper()
	if _, err := sdb.Instance().PutClaimGuest(context.Background(), f.ownerAC, f.readers, mode); err != nil {
		t.Fatalf("guest claim %s: %v", mode, err)
	}
}

func (f codeFixture) postCode(t *testing.T, body shared.BodyCode) *shared.CodeCreated {
	t.Helper()
	created, err := sdb.Instance().PostCode(context.Background(), f.ownerAC, f.readers, body, "https://svc.example.test")
	if err != nil {
		t.Fatalf("post code: %v", err)
	}
	return created
}

type guardAnswer struct {
	UserID    uuid.UUID `json:"user_id"`
	Access    int       `json:"access"`
	Anonymous bool      `json:"anonymous"`
	Query     string    `json:"query"`
	Header    string    `json:"header"`
}

func serveGuard(t *testing.T, opts *smw.UserAuthOptions, object uuid.UUID, required shared.ClaimAccess, req *http.Request) (*httptest.ResponseRecorder, guardAnswer) {
	t.Helper()
	server := newAuthServerWithFailingRefresh(t)
	t.Cleanup(server.Close)
	_ = userAuthHandlerAgainst(t, server, opts)
	mw := smw.UserAuthMiddleware(opts)
	handler := mw(func(ctx echo.Context) error {
		userID, _ := ctx.Get(smw.UserIDKey).(uuid.UUID)
		ac, err := sdb.Instance().MutationAccess(ctx.Request().Context(), userID, object, shared.OBJECT_TYPE_ORGANIZATION, required)
		if err != nil {
			return ctx.String(http.StatusForbidden, "Forbidden")
		}
		anonymous, _ := ctx.Get(smw.AnonymousKey).(bool)
		return ctx.JSON(http.StatusOK, guardAnswer{
			UserID:    ac.UserID,
			Access:    int(ac.Access),
			Anonymous: anonymous,
			Query:     ctx.Request().URL.RawQuery + "|" + ctx.Request().RequestURI + "|" + ctx.QueryParam(smw.CodeParam),
			Header:    ctx.Request().Header.Get(smw.CodeHeader),
		})
	})
	rec := httptest.NewRecorder()
	if err := handler(echo.New().NewContext(req, rec)); err != nil {
		t.Fatalf("guard: %v", err)
	}
	var answer guardAnswer
	if rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), &answer); err != nil {
			t.Fatalf("answer: %v", err)
		}
	}
	return rec, answer
}

func getCodeCookie(rec *httptest.ResponseRecorder) *http.Cookie {
	for _, c := range rec.Result().Cookies() {
		if c.Name == smw.CodeCookie {
			return c
		}
	}
	return nil
}

func TestACodeIsStoredByItsHashAndNeverByItsValue(t *testing.T) {
	f := getCodeFixture(t)
	f.putGuest(t, shared.GUEST_MODE_CODE)
	created := f.postCode(t, shared.BodyCode{Kind: shared.CODE_KIND_GUEST, Path: "/board"})

	if len(created.Value) != 43 {
		t.Fatalf("a code value is 32 random bytes base64url, 43 characters, got %d", len(created.Value))
	}
	if !strings.HasPrefix(created.URL, "https://svc.example.test/board?samk=") || !strings.HasSuffix(created.URL, created.Value) {
		t.Fatalf("the url carries the value in samk on the given path, got %s", created.URL)
	}
	var stored, plain int
	if err := sdb.Instance().NewRaw(`SELECT count(*) FROM claimius.code WHERE id = ? AND hash = ?`, created.ID, shared.GetCodeHash(created.Value)).Scan(context.Background(), &stored); err != nil || stored != 1 {
		t.Fatalf("the code must be stored by the sha256 of its value, got %d %v", stored, err)
	}
	if err := sdb.Instance().NewRaw(`SELECT count(*) FROM claimius.code WHERE to_jsonb(code)::TEXT LIKE '%' || ? || '%'`, created.Value).Scan(context.Background(), &plain); err != nil || plain != 0 {
		t.Fatalf("no column may hold the value, got %d %v", plain, err)
	}
	if encoded, _ := json.Marshal(created.Code); strings.Contains(string(encoded), shared.GetCodeHash(created.Value)) {
		t.Fatal("the hash stays off the wire of the service's api")
	}
}

func TestACodeResolvesOnlyInsideItsWindowCountAndUntilRevoked(t *testing.T) {
	f := getCodeFixture(t)
	f.putGuest(t, shared.GUEST_MODE_CODE)
	ctx := context.Background()
	db := sdb.Instance()

	future := time.Now().Add(time.Hour)
	later := future.Add(time.Hour)
	early := f.postCode(t, shared.BodyCode{Kind: shared.CODE_KIND_GUEST, StartsAt: &future, EndsAt: &later})
	if _, err := db.GetCodeAccess(ctx, early.Value); err != shared.ErrCodeInvalid {
		t.Fatalf("a code before its window must not resolve, got %v", err)
	}

	past := time.Now().Add(-time.Minute)
	ended := f.postCode(t, shared.BodyCode{Kind: shared.CODE_KIND_GUEST, EndsAt: &past})
	if _, err := db.GetCodeAccess(ctx, ended.Value); err != shared.ErrCodeInvalid {
		t.Fatalf("a code after its window must not resolve, got %v", err)
	}

	once := f.postCode(t, shared.BodyCode{Kind: shared.CODE_KIND_GUEST, Count: 1})
	if _, err := db.GetCodeAccess(ctx, once.Value); err != nil {
		t.Fatalf("a fresh single use code resolves, got %v", err)
	}
	if _, err := db.PutCodeUsed(ctx, once.Value); err != nil {
		t.Fatalf("the first use passes, got %v", err)
	}
	if _, err := db.PutCodeUsed(ctx, once.Value); err != shared.ErrCodeInvalid {
		t.Fatalf("the second use of a single use code must fail, got %v", err)
	}
	if _, err := db.GetCodeAccess(ctx, once.Value); err != shared.ErrCodeInvalid {
		t.Fatalf("a used up code must not resolve, got %v", err)
	}

	live := f.postCode(t, shared.BodyCode{Kind: shared.CODE_KIND_GUEST})
	if _, err := db.GetCodeAccess(ctx, live.Value); err != nil {
		t.Fatalf("a live code resolves, got %v", err)
	}
	if _, err := db.DeleteCode(ctx, f.ownerAC, live.ID); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if _, err := db.GetCodeAccess(ctx, live.Value); err != shared.ErrCodeInvalid {
		t.Fatalf("a revoked code must not resolve, even from the cache, got %v", err)
	}
	codes, err := db.GetCodes(ctx, f.readers)
	if err != nil || len(codes) != 4 {
		t.Fatalf("the claim lists its four codes, revoked ones included, got %d %v", len(codes), err)
	}
}

func TestAGuestCodeNeedsAGuestClaim(t *testing.T) {
	f := getCodeFixture(t)
	_, err := sdb.Instance().PostCode(context.Background(), f.ownerAC, f.readers, shared.BodyCode{Kind: shared.CODE_KIND_GUEST}, "")
	if err == nil || !strings.Contains(err.Error(), "not a guest claim") {
		t.Fatalf("a guest code on a claim whose guest is off must be refused, got %v", err)
	}
}

func TestOnlyAnOwnerOfTheClaimCreatesACode(t *testing.T) {
	f := getCodeFixture(t)
	stranger := &shared.AccessClaim{UserID: uuid.New()}
	if _, err := sdb.Instance().PostCode(context.Background(), stranger, f.readers, shared.BodyCode{Kind: shared.CODE_KIND_INVITE}, ""); err == nil {
		t.Fatal("a user who does not own the claim must not create a code on it")
	}
}

func TestAnInviteRedeemsOnceIntoAGrantOfItsClaim(t *testing.T) {
	f := getCodeFixture(t)
	ctx := context.Background()
	db := sdb.Instance()
	invite := f.postCode(t, shared.BodyCode{Kind: shared.CODE_KIND_INVITE, Count: 1})

	first, second := uuid.New(), uuid.New()
	for _, u := range []uuid.UUID{first, second} {
		if _, err := db.ExecContext(ctx, `SELECT claimius.upsert_app_user(p_user_id => ?, p_first_name => 'Invited')`, u); err != nil {
			t.Fatalf("user: %v", err)
		}
	}
	results := make(chan error, 2)
	for _, u := range []uuid.UUID{first, second} {
		go func(u uuid.UUID) {
			_, err := db.PostCodeRedeem(ctx, invite.Value, u)
			results <- err
		}(u)
	}
	passed := 0
	for range 2 {
		if err := <-results; err == nil {
			passed++
		} else if err != shared.ErrCodeInvalid {
			t.Fatalf("a losing redeem fails as an invalid code, got %v", err)
		}
	}
	if passed != 1 {
		t.Fatalf("two concurrent redeems of a single use invite must grant exactly once, got %d", passed)
	}
	var grants int
	if err := db.NewRaw(`SELECT count(*) FROM claimius.user_claim WHERE claim_id = ? AND user_id IN (?, ?) AND sa_deleted_at IS NULL`, f.readers, first, second).Scan(ctx, &grants); err != nil || grants != 1 {
		t.Fatalf("exactly one grant of the claim, got %d %v", grants, err)
	}
	var audited int
	if err := db.NewRaw(`SELECT count(*) FROM claimius.audit WHERE object_id = ? AND operation = 'redeem code' AND message LIKE '%' || ? || '%' AND message NOT LIKE '%' || ? || '%'`, f.readers, invite.ID.String(), invite.Value).Scan(ctx, &audited); err != nil || audited != 1 {
		t.Fatalf("the redeem is audited by the code id, never the value, got %d %v", audited, err)
	}
}

func TestAGuestCodeOnANavigationIsStrippedWithA303AndAHostCookie(t *testing.T) {
	f := getCodeFixture(t)
	f.putGuest(t, shared.GUEST_MODE_CODE)
	ends := time.Now().Add(time.Hour)
	code := f.postCode(t, shared.BodyCode{Kind: shared.CODE_KIND_GUEST, EndsAt: &ends})

	req := httptest.NewRequest(http.MethodGet, "/board?view=1&samk="+code.Value, nil)
	req.Header.Set("Accept", "text/html")
	rec, _ := serveGuard(t, guestRoute, f.org, shared.ACCESS_READ, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("a navigation carrying samk answers 303, got %d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/board?view=1" {
		t.Fatalf("the 303 points at the same url without samk, got %q", loc)
	}
	if rp := rec.Header().Get("Referrer-Policy"); rp != "no-referrer" {
		t.Fatalf("an answer carrying samk sends Referrer-Policy no-referrer, got %q", rp)
	}
	cookie := getCodeCookie(rec)
	if cookie == nil || cookie.Value != code.Value || !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteLaxMode || cookie.Path != "/" || cookie.Domain != "" {
		t.Fatalf("the code moves to a host only HttpOnly Secure SameSite=Lax cookie, got %+v", cookie)
	}
	if cookie.MaxAge <= 0 || cookie.MaxAge > 3600 {
		t.Fatalf("the cookie expires with the code, got max age %d", cookie.MaxAge)
	}

	follow := httptest.NewRequest(http.MethodGet, "/board?view=1", nil)
	follow.AddCookie(&http.Cookie{Name: smw.CodeCookie, Value: code.Value})
	rec, answer := serveGuard(t, guestRoute, f.org, shared.ACCESS_READ, follow)
	if rec.Code != http.StatusOK {
		t.Fatalf("the cookie carries the guest, got %d %s", rec.Code, rec.Body.String())
	}
	f.assertGuest(t, answer)
}

func (f codeFixture) assertGuest(t *testing.T, answer guardAnswer) {
	t.Helper()
	var guest uuid.UUID
	if err := sdb.Instance().NewRaw(`
		SELECT uc.user_id FROM claimius.user_claim uc JOIN claimius.samna_user u ON u.user_id = uc.user_id AND u.type = 'guest'
		WHERE uc.claim_id = ? AND uc.sa_deleted_at IS NULL`, f.readers).Scan(context.Background(), &guest); err != nil {
		t.Fatalf("guest: %v", err)
	}
	if answer.UserID != guest {
		t.Fatalf("the request acts as the guest of the claim %s, got %s", guest, answer.UserID)
	}
	if answer.Access != int(shared.ACCESS_READ) {
		t.Fatalf("under the default guest ceiling a guest holds READ only, got access %d from a claim holding 15", answer.Access)
	}
}

func TestAGuestCodeInTheHeaderActsAsTheGuestAndIsAudited(t *testing.T) {
	f := getCodeFixture(t)
	f.putGuest(t, shared.GUEST_MODE_CODE)
	code := f.postCode(t, shared.BodyCode{Kind: shared.CODE_KIND_GUEST})

	req := httptest.NewRequest(http.MethodGet, "/api/board", nil)
	req.Header.Set(smw.CodeHeader, code.Value)
	rec, answer := serveGuard(t, guestRoute, f.org, shared.ACCESS_READ, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("an api call with SAuth-Code is served, got %d", rec.Code)
	}
	f.assertGuest(t, answer)
	if answer.Header != "" {
		t.Fatal("the guard takes SAuth-Code off the request before the handler and any logger read it")
	}
	if rec.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Fatal("an answer to a request carrying a code sends Referrer-Policy no-referrer")
	}
	var audited int
	if err := sdb.Instance().NewRaw(`
		SELECT count(*) FROM claimius.audit
		WHERE object_id = ? AND operation = 'guest request'
		  AND message LIKE '%' || ? || '%' AND message LIKE '%' || ? || '%' AND message NOT LIKE '%' || ? || '%'`,
		f.readers, f.readers.String(), code.ID.String(), code.Value).Scan(context.Background(), &audited); err != nil || audited != 1 {
		t.Fatalf("the guest request is audited with claim and code id and never the value, got %d %v", audited, err)
	}

	write := httptest.NewRequest(http.MethodGet, "/api/board", nil)
	write.Header.Set(smw.CodeHeader, code.Value)
	if rec, _ := serveGuard(t, guestRoute, f.org, shared.ACCESS_WRITE, write); rec.Code != http.StatusForbidden {
		t.Fatalf("under the default guest ceiling a guest write is refused, got %d", rec.Code)
	}
}

func TestTheGuardRedactsTheCodeFromTheRequestAndTheSpan(t *testing.T) {
	f := getCodeFixture(t)
	f.putGuest(t, shared.GUEST_MODE_CODE)
	code := f.postCode(t, shared.BodyCode{Kind: shared.CODE_KIND_GUEST})

	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	t.Cleanup(func() { otel.SetTracerProvider(previous) })

	server := newAuthServerWithFailingRefresh(t)
	t.Cleanup(server.Close)
	_ = userAuthHandlerAgainst(t, server, guestRoute)
	var seen guardAnswer
	handler := smw.TelemetryMiddleware()(smw.UserAuthMiddleware(guestRoute)(func(ctx echo.Context) error {
		seen.Query = ctx.Request().URL.RawQuery + "|" + ctx.Request().RequestURI + "|" + ctx.QueryParam(smw.CodeParam)
		return ctx.NoContent(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/api/board?page=2&samk="+code.Value, nil)
	req.Header.Set("Accept", "application/json")
	rec := httptest.NewRecorder()
	ctx := echo.New().NewContext(req, rec)
	if err := handler(ctx); err != nil {
		t.Fatalf("guard: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("an api call with samk is served, got %d", rec.Code)
	}
	if strings.Contains(seen.Query, code.Value) || strings.Contains(seen.Query, "samk") {
		t.Fatalf("the handler and any request logger see the url without samk, got %q", seen.Query)
	}
	if err := provider.ForceFlush(context.Background()); err != nil {
		t.Fatalf("flush: %v", err)
	}
	spans := recorder.Ended()
	if len(spans) == 0 {
		t.Fatal("the telemetry middleware records a span")
	}
	for _, span := range spans {
		for _, attr := range span.Attributes() {
			if strings.Contains(attr.Value.Emit(), code.Value) {
				t.Fatalf("span attribute %s carries the code value", attr.Key)
			}
		}
		if strings.Contains(span.Name(), code.Value) {
			t.Fatal("the span name carries the code value")
		}
	}
}

func TestAnOpenGuestClaimServesARouteThatAllowsGuests(t *testing.T) {
	f := getCodeFixture(t)
	f.putGuest(t, shared.GUEST_MODE_OPEN)

	rec, answer := serveGuard(t, &smw.UserAuthOptions{Guest: true}, f.org, shared.ACCESS_READ, httptest.NewRequest(http.MethodGet, "/api/board", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("an object covered by an open guest claim is read without a credential, got %d", rec.Code)
	}
	if !answer.Anonymous {
		t.Fatal("the request is marked anonymous")
	}
	f.assertGuest(t, answer)

	if rec, _ := serveGuard(t, &smw.UserAuthOptions{Guest: true}, f.hidden, shared.ACCESS_READ, httptest.NewRequest(http.MethodGet, "/api/board", nil)); rec.Code != http.StatusUnauthorized {
		t.Fatalf("an object no open guest claim covers answers 401 as before, got %d", rec.Code)
	}
	if rec, _ := serveGuard(t, nil, f.org, shared.ACCESS_READ, httptest.NewRequest(http.MethodGet, "/api/board", nil)); rec.Code != http.StatusUnauthorized {
		t.Fatalf("a route without Guest is unaffected by an open guest claim, got %d", rec.Code)
	}
	if rec, _ := serveGuard(t, &smw.UserAuthOptions{Guest: true}, f.org, shared.ACCESS_WRITE, httptest.NewRequest(http.MethodGet, "/api/board", nil)); rec.Code != http.StatusUnauthorized {
		t.Fatalf("under the default guest ceiling an open guest write is refused, got %d", rec.Code)
	}

	f.putGuest(t, shared.GUEST_MODE_CODE)
	if rec, _ := serveGuard(t, &smw.UserAuthOptions{Guest: true}, f.org, shared.ACCESS_READ, httptest.NewRequest(http.MethodGet, "/api/board", nil)); rec.Code != http.StatusUnauthorized {
		t.Fatalf("a guest claim in code mode serves no anonymous request, got %d", rec.Code)
	}
}

func (f codeFixture) putGuestAccess(t *testing.T, access shared.ClaimAccess) {
	t.Helper()
	ctx := context.Background()
	if _, err := sdb.Instance().ExecContext(ctx, `SELECT claimius.upsert_disciple_state()`); err != nil {
		t.Fatalf("disciple state: %v", err)
	}
	if err := sdb.Instance().PutGuestAccess(ctx, access); err != nil {
		t.Fatalf("guest access %d: %v", access, err)
	}
	t.Cleanup(func() {
		if err := sdb.Instance().PutGuestAccess(context.Background(), shared.GuestAccessEnv()); err != nil {
			t.Errorf("reset guest access: %v", err)
		}
	})
}

func (f codeFixture) postClaim(t *testing.T, name string, access shared.ClaimAccess) (uuid.UUID, *shared.AccessClaim) {
	t.Helper()
	ctx := context.Background()
	db := sdb.Instance()
	var id uuid.UUID
	if err := db.NewRaw(`
		SELECT (claimius.insert_claim(p_name => ?2, p_description => NULL, p_sa_access => ?3,
		        p_sa_owner_id => ?0, p_sa_root_id => ?0, p_sa_created_by => ?1) -> 'claim' ->> 'id')::UUID
	`, f.org, f.owner, name, int(access)).Scan(ctx, &id); err != nil {
		t.Fatalf("claim %s: %v", name, err)
	}
	if _, err := db.ExecContext(ctx, `
		SELECT claimius.insert_claim_object(p_claim_id => ?, p_sa_owner_id => ?, p_sa_root_id => ?, p_sa_created_by => ?,
		       p_object_id => ?, p_object_type => 'claimius.organization', p_inherits => TRUE)
	`, id, f.org, f.org, f.owner, f.org); err != nil {
		t.Fatalf("bind %s: %v", name, err)
	}
	ac, err := db.MutationAccess(ctx, f.owner, id, shared.OBJECT_TYPE_CLAIM, shared.ACCESS_OWNER)
	if err != nil {
		t.Fatalf("the owner must own claim %s: %v", name, err)
	}
	return id, ac
}

func TestARaisedGuestCeilingLetsAnOpenGuestExecuteWhatItsClaimGrants(t *testing.T) {
	f := getCodeFixture(t)
	f.putGuestAccess(t, shared.ACCESS_READ|shared.ACCESS_EXECUTE)
	bookers, ac := f.postClaim(t, "Bookers", shared.ACCESS_READ|shared.ACCESS_EXECUTE)
	if _, err := sdb.Instance().PutClaimGuest(context.Background(), ac, bookers, shared.GUEST_MODE_OPEN); err != nil {
		t.Fatalf("open guest claim: %v", err)
	}

	rec, answer := serveGuard(t, guestRoute, f.org, shared.ACCESS_EXECUTE, httptest.NewRequest(http.MethodPost, "/api/booking", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("an open guest claim with EXECUTE under a READ|EXECUTE ceiling books, got %d", rec.Code)
	}
	if !answer.Anonymous || answer.Access != int(shared.ACCESS_READ|shared.ACCESS_EXECUTE) {
		t.Fatalf("the guest holds what its claim grants, READ|EXECUTE, got anonymous %v access %d", answer.Anonymous, answer.Access)
	}
	if rec, _ := serveGuard(t, guestRoute, f.org, shared.ACCESS_WRITE, httptest.NewRequest(http.MethodPut, "/api/booking", nil)); rec.Code != http.StatusUnauthorized {
		t.Fatalf("a requirement past the ceiling is refused, got %d", rec.Code)
	}
}

func TestARaisedGuestCeilingKeepsAReadOnlyOpenGuestFromWriting(t *testing.T) {
	f := getCodeFixture(t)
	f.putGuestAccess(t, shared.ACCESS_READ|shared.ACCESS_EXECUTE)
	viewers, ac := f.postClaim(t, "Viewers", shared.ACCESS_READ)
	if _, err := sdb.Instance().PutClaimGuest(context.Background(), ac, viewers, shared.GUEST_MODE_OPEN); err != nil {
		t.Fatalf("open guest claim: %v", err)
	}

	if rec, answer := serveGuard(t, guestRoute, f.org, shared.ACCESS_READ, httptest.NewRequest(http.MethodGet, "/api/board", nil)); rec.Code != http.StatusOK || answer.Access != int(shared.ACCESS_READ) {
		t.Fatalf("a READ claim still reads, got %d access %d", rec.Code, answer.Access)
	}
	if rec, _ := serveGuard(t, guestRoute, f.org, shared.ACCESS_EXECUTE, httptest.NewRequest(http.MethodPost, "/api/booking", nil)); rec.Code != http.StatusUnauthorized {
		t.Fatalf("a READ only open guest claim refuses EXECUTE under a raised ceiling, got %d", rec.Code)
	}
}

func TestARaisedGuestCeilingLetsACodeGuestActOnWhatItsClaimGrants(t *testing.T) {
	f := getCodeFixture(t)
	f.putGuestAccess(t, shared.ACCESS_ALL)
	f.putGuest(t, shared.GUEST_MODE_CODE)
	code := f.postCode(t, shared.BodyCode{Kind: shared.CODE_KIND_GUEST})

	req := httptest.NewRequest(http.MethodDelete, "/api/booking", nil)
	req.Header.Set(smw.CodeHeader, code.Value)
	rec, answer := serveGuard(t, guestRoute, f.org, shared.ACCESS_WRITE, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("the holder of a code on a claim holding 15 under an ALL ceiling writes, got %d", rec.Code)
	}
	if answer.Access != int(shared.ACCESS_ALL) {
		t.Fatalf("the code guest holds what its claim grants, got access %d", answer.Access)
	}
}

func TestRepeatedFailedCodesAnswer429(t *testing.T) {
	f := getCodeFixture(t)
	f.putGuest(t, shared.GUEST_MODE_CODE)
	code := f.postCode(t, shared.BodyCode{Kind: shared.CODE_KIND_GUEST})
	ip := fmt.Sprintf("198.51.100.%d", time.Now().UnixNano()%250+1)
	request := func(value string) int {
		req := httptest.NewRequest(http.MethodGet, "/api/board", nil)
		req.Header.Set(smw.CodeHeader, value)
		req.Header.Set("X-Real-IP", ip)
		rec, _ := serveGuard(t, nil, f.org, shared.ACCESS_READ, req)
		return rec.Code
	}
	for i := range 20 {
		if status := request(fmt.Sprintf("wrong-%d", i)); status != http.StatusUnauthorized {
			t.Fatalf("a wrong code answers 401, got %d on attempt %d", status, i)
		}
	}
	if status := request("wrong-again"); status != http.StatusTooManyRequests {
		t.Fatalf("past twenty failed resolves an address gets 429, got %d", status)
	}
	if status := request(code.Value); status != http.StatusTooManyRequests {
		t.Fatalf("the limit fails closed: even a valid code waits out the window, got %d", status)
	}
}

func TestAnInviteWithoutALoginKeepsTheCodeThroughTheLogin(t *testing.T) {
	f := getCodeFixture(t)
	invite := f.postCode(t, shared.BodyCode{Kind: shared.CODE_KIND_INVITE})

	req := httptest.NewRequest(http.MethodGet, "/board?samk="+invite.Value, nil)
	req.Header.Set("Accept", "text/html")
	rec, _ := serveGuard(t, nil, f.org, shared.ACCESS_READ, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("an invite without a login sends the browser to the login, got %d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/api/auth/login?next=%2Fboard" {
		t.Fatalf("the login returns to the url without samk, got %q", loc)
	}
	cookie := getCodeCookie(rec)
	if cookie == nil || cookie.Value != invite.Value || !cookie.HttpOnly || !cookie.Secure || cookie.MaxAge != 600 {
		t.Fatalf("the code waits for the login in a short HttpOnly Secure cookie, got %+v", cookie)
	}

	api := httptest.NewRequest(http.MethodGet, "/api/board", nil)
	api.Header.Set(smw.CodeHeader, invite.Value)
	if rec, _ := serveGuard(t, nil, f.org, shared.ACCESS_READ, api); rec.Code != http.StatusUnauthorized {
		t.Fatalf("an api call with an invite and no login answers 401, got %d", rec.Code)
	}
}

var guestRoute = &smw.UserAuthOptions{Guest: true}

func TestAGuestCodeIsNoIdentityOnARouteWithoutGuest(t *testing.T) {
	f := getCodeFixture(t)
	f.putGuest(t, shared.GUEST_MODE_CODE)
	code := f.postCode(t, shared.BodyCode{Kind: shared.CODE_KIND_GUEST, Count: 1})

	api := httptest.NewRequest(http.MethodGet, "/api/board", nil)
	api.Header.Set(smw.CodeHeader, code.Value)
	if rec, _ := serveGuard(t, nil, f.org, shared.ACCESS_READ, api); rec.Code != http.StatusUnauthorized {
		t.Fatalf("an api call with SAuth-Code on a route without Guest answers 401, got %d", rec.Code)
	}

	cookie := httptest.NewRequest(http.MethodGet, "/api/board", nil)
	cookie.AddCookie(&http.Cookie{Name: smw.CodeCookie, Value: code.Value})
	rec, _ := serveGuard(t, nil, f.org, shared.ACCESS_READ, cookie)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("the code cookie on a route without Guest answers 401, got %d", rec.Code)
	}
	if getCodeCookie(rec) != nil {
		t.Fatal("a route without Guest leaves the code cookie alone")
	}

	nav := httptest.NewRequest(http.MethodGet, "/board?samk="+code.Value, nil)
	nav.Header.Set("Accept", "text/html")
	rec, _ = serveGuard(t, nil, f.org, shared.ACCESS_READ, nav)
	if rec.Code != http.StatusFound {
		t.Fatalf("a navigation with samk on a route without Guest goes to the login, got %d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/api/auth/login" || strings.Contains(loc, code.Value) {
		t.Fatalf("the login redirect carries no code, got %q", loc)
	}
	if getCodeCookie(rec) != nil {
		t.Fatal("a route without Guest writes no code cookie")
	}
	if rec.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Fatal("an answer to a request carrying a code sends Referrer-Policy no-referrer")
	}

	var audited int
	if err := sdb.Instance().NewRaw(`SELECT count(*) FROM claimius.audit WHERE object_id = ? AND operation = 'guest request'`, f.readers).Scan(context.Background(), &audited); err != nil || audited != 0 {
		t.Fatalf("no guest request is audited on a route without Guest, got %d %v", audited, err)
	}

	guest := httptest.NewRequest(http.MethodGet, "/api/board", nil)
	guest.Header.Set(smw.CodeHeader, code.Value)
	rec, answer := serveGuard(t, guestRoute, f.org, shared.ACCESS_READ, guest)
	if rec.Code != http.StatusOK {
		t.Fatalf("the single use code is still unused and serves a route with Guest, got %d", rec.Code)
	}
	f.assertGuest(t, answer)
}
