//go:build dbtest

package smw_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/nimling/samna-auth-middleware/v2/analytics"
	sdb "github.com/nimling/samna-auth-middleware/v2/disciple/sdb"
	"github.com/nimling/samna-auth-middleware/v2/shared"
	"github.com/nimling/samna-auth-middleware/v2/smw"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

type codeFixture struct {
	owner   uuid.UUID
	org     uuid.UUID
	hidden  uuid.UUID
	readers uuid.UUID
	ownerAC *shared.AccessClaim
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
	t.Cleanup(func() {
		db.RouteToProphet = routes
		if _, err := db.ExecContext(ctx, `UPDATE claimius.user_claim SET sa_deleted_at = now() WHERE user_id = ? AND sa_deleted_at IS NULL`, shared.AnonymousUserID); err != nil {
			t.Errorf("end public grants: %v", err)
		}
		shared.DeletePublicClaimCache()
	})

	f := codeFixture{owner: uuid.New(), org: uuid.New(), hidden: uuid.New()}
	if _, err := db.ExecContext(ctx, `SELECT claimius.upsert_app_user(p_user_id => ?, p_first_name => 'Owner')`, f.owner); err != nil {
		t.Fatalf("owner: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
		VALUES (?0, 'Shared', 'customer', ?0, ?0, 0, ?1), (?2, 'Hidden', 'customer', ?2, ?2, 0, ?1)
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
		SELECT (claimius.insert_claim(p_name => 'Readers', p_description => NULL, p_sa_access => 4,
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

func (f codeFixture) grantPublic(t *testing.T, claimID uuid.UUID, options *shared.ClaimOptions, ends *time.Time) (string, uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	db := sdb.Instance()
	value := ""
	var raw any
	if options != nil {
		plain, err := options.Seal()
		if err != nil {
			t.Fatalf("seal: %v", err)
		}
		value = plain
		encoded, _ := json.Marshal(options)
		raw = string(encoded)
	}
	var grantID uuid.UUID
	if err := db.NewRaw(`SELECT (claimius.insert_user_claim(p_claim_id => ?, p_user_id => ?, p_sa_owner_id => ?, p_sa_created_by => ?, p_ends_at => ?, p_options => ?::jsonb) -> 'user_claim' ->> 'id')::UUID`,
		claimID, shared.AnonymousUserID, f.org, *f.ownerAC.UserClaimID, ends, raw).Scan(ctx, &grantID); err != nil {
		t.Fatalf("public grant: %v", err)
	}
	shared.DeletePublicClaimCache()
	return value, grantID
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
	UserID      uuid.UUID  `json:"user_id"`
	UserClaimID *uuid.UUID `json:"user_claim_id"`
	Access      int        `json:"access"`
	Anonymous   bool       `json:"anonymous"`
	Session     string     `json:"session"`
	Query       string     `json:"query"`
	Header      string     `json:"header"`
}

func serveGuard(t *testing.T, object uuid.UUID, required shared.ClaimAccess, req *http.Request) (*httptest.ResponseRecorder, guardAnswer) {
	t.Helper()
	server := newAuthServerWithFailingRefresh(t)
	t.Cleanup(server.Close)
	_ = userAuthHandlerAgainst(t, server, nil)
	mw := smw.UserAuthMiddleware(nil)
	handler := mw(func(ctx echo.Context) error {
		userID, _ := ctx.Get(smw.UserIDKey).(uuid.UUID)
		ac, err := sdb.Instance().MutationAccess(ctx.Request().Context(), userID, object, shared.OBJECT_TYPE_ORGANIZATION, required)
		if err != nil {
			return ctx.String(http.StatusForbidden, "Forbidden")
		}
		anonymous, _ := ctx.Get(smw.AnonymousKey).(string)
		return ctx.JSON(http.StatusOK, guardAnswer{
			UserID:      ac.UserID,
			UserClaimID: ac.UserClaimID,
			Access:      int(ac.Access),
			Anonymous:   anonymous != "",
			Session:     anonymous,
			Query:       ctx.Request().URL.RawQuery + "|" + ctx.Request().RequestURI + "|" + ctx.QueryParam(smw.CodeParam),
			Header:      ctx.Request().Header.Get(smw.CodeHeader),
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

func (f codeFixture) assertPublic(t *testing.T, answer guardAnswer, userID uuid.UUID, access shared.ClaimAccess) {
	t.Helper()
	if answer.UserID != userID {
		t.Fatalf("the request acts as %s, got %s", userID, answer.UserID)
	}
	if answer.Access != int(access) {
		t.Fatalf("a public grant holds what its claim grants, %d, got access %d", access, answer.Access)
	}
	if !answer.Anonymous {
		t.Fatal("the request is marked anonymous")
	}
	if answer.UserClaimID == nil || *answer.UserClaimID != *f.ownerAC.UserClaimID {
		t.Fatalf("a write under a public grant is stamped with the grant of the user who made it public, %s, got %v", *f.ownerAC.UserClaimID, answer.UserClaimID)
	}
}

func TestACodeIsStoredByItsHashAndNeverByItsValue(t *testing.T) {
	f := getCodeFixture(t)
	created := f.postCode(t, shared.BodyCode{Kind: shared.CODE_KIND_INVITE, Path: "/board"})

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
	if _, err := sdb.Instance().PostCode(context.Background(), f.ownerAC, f.readers, shared.BodyCode{Kind: "guest"}, ""); !errors.Is(err, shared.ErrBadCode) {
		t.Fatalf("invite is the only kind of code, got %v", err)
	}
}

func TestACodeResolvesOnlyInsideItsWindowCountAndUntilRevoked(t *testing.T) {
	f := getCodeFixture(t)
	ctx := context.Background()
	db := sdb.Instance()

	future := time.Now().Add(time.Hour)
	later := future.Add(time.Hour)
	early := f.postCode(t, shared.BodyCode{Kind: shared.CODE_KIND_INVITE, StartsAt: &future, EndsAt: &later})
	if _, err := db.GetCodeAccess(ctx, early.Value); err != shared.ErrCodeInvalid {
		t.Fatalf("a code before its window must not resolve, got %v", err)
	}

	past := time.Now().Add(-time.Minute)
	ended := f.postCode(t, shared.BodyCode{Kind: shared.CODE_KIND_INVITE, EndsAt: &past})
	if _, err := db.GetCodeAccess(ctx, ended.Value); err != shared.ErrCodeInvalid {
		t.Fatalf("a code after its window must not resolve, got %v", err)
	}

	once := f.postCode(t, shared.BodyCode{Kind: shared.CODE_KIND_INVITE, Count: 1})
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

	live := f.postCode(t, shared.BodyCode{Kind: shared.CODE_KIND_INVITE})
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

func TestTheAnonymousUserIsReservedInTheDatabase(t *testing.T) {
	getCodeFixture(t)
	ctx := context.Background()
	var reserved uuid.UUID
	if err := sdb.Instance().NewRaw(`SELECT claimius.anonymous_user_id()`).Scan(ctx, &reserved); err != nil || reserved != shared.AnonymousUserID {
		t.Fatalf("shared.AnonymousUserID names the reserved row, got %s %v", reserved, err)
	}
	var row struct {
		Type      string  `bun:"type"`
		FirstName *string `bun:"first_name"`
		Email     *string `bun:"email"`
	}
	if err := sdb.Instance().NewRaw(`SELECT type, first_name, email FROM claimius.samna_user WHERE user_id = ? AND sa_deleted_at IS NULL`, shared.AnonymousUserID).Scan(ctx, &row); err != nil || row.Type != string(shared.USER_TYPE_ANONYMOUS) || row.FirstName == nil || *row.FirstName != "Public" || (row.Email != nil && *row.Email != "") {
		t.Fatalf("the anonymous user is a samna_user of type anonymous named Public with no email, written by the bootstrap, got %+v %v", row, err)
	}
}

func TestAPublicCodeOnANavigationIsStrippedWithA303AndAHostCookie(t *testing.T) {
	f := getCodeFixture(t)
	ends := time.Now().Add(time.Hour)
	value, _ := f.grantPublic(t, f.readers, &shared.ClaimOptions{Samk: &shared.ClaimOptionsSamk{Query: true, Cookie: true}}, &ends)
	var stored string
	if err := sdb.Instance().NewRaw(`SELECT options -> 'samk' ->> 'hash' FROM claimius.user_claim WHERE claim_id = ? AND user_id = ? AND sa_deleted_at IS NULL`, f.readers, shared.AnonymousUserID).Scan(context.Background(), &stored); err != nil || stored != shared.GetCodeHash(value) {
		t.Fatalf("the public grant stores the sha256 of the code, got %q %v", stored, err)
	}

	req := httptest.NewRequest(http.MethodGet, "/board?view=1&samk="+value, nil)
	req.Header.Set("Accept", "text/html")
	rec, _ := serveGuard(t, f.org, shared.ACCESS_READ, req)
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
	if cookie == nil || cookie.Value != value || !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteLaxMode || cookie.Path != "/" || cookie.Domain != "" {
		t.Fatalf("the code moves to a host only HttpOnly Secure SameSite=Lax cookie, got %+v", cookie)
	}
	if cookie.MaxAge <= 0 || cookie.MaxAge > 3600 {
		t.Fatalf("the cookie expires with the public grant, got max age %d", cookie.MaxAge)
	}

	follow := httptest.NewRequest(http.MethodGet, "/board?view=1", nil)
	follow.AddCookie(&http.Cookie{Name: smw.CodeCookie, Value: value})
	rec, answer := serveGuard(t, f.org, shared.ACCESS_READ, follow)
	if rec.Code != http.StatusOK {
		t.Fatalf("the cookie carries the code, got %d %s", rec.Code, rec.Body.String())
	}
	f.assertPublic(t, answer, shared.AnonymousUserID, shared.ACCESS_READ)
}

func TestAPublicCodeInTheHeaderHoldsTheClaimAndIsTakenOffTheRequest(t *testing.T) {
	f := getCodeFixture(t)
	value, _ := f.grantPublic(t, f.readers, &shared.ClaimOptions{Samk: &shared.ClaimOptionsSamk{Header: true}}, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/board", nil)
	req.Header.Set(smw.CodeHeader, value)
	rec, answer := serveGuard(t, f.org, shared.ACCESS_READ, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("an api call with SAuth-Code is served, got %d %s", rec.Code, rec.Body.String())
	}
	f.assertPublic(t, answer, shared.AnonymousUserID, shared.ACCESS_READ)
	if answer.Header != "" {
		t.Fatal("the guard takes SAuth-Code off the request before the handler and any logger read it")
	}
	if rec.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Fatal("an answer to a request carrying a code sends Referrer-Policy no-referrer")
	}

	write := httptest.NewRequest(http.MethodGet, "/api/board", nil)
	write.Header.Set(smw.CodeHeader, value)
	if rec, _ := serveGuard(t, f.org, shared.ACCESS_WRITE, write); rec.Code != http.StatusUnauthorized {
		t.Fatalf("a public READ claim refuses a write, got %d", rec.Code)
	}

	query := httptest.NewRequest(http.MethodGet, "/api/board?samk="+value, nil)
	if rec, _ := serveGuard(t, f.org, shared.ACCESS_READ, query); rec.Code != http.StatusUnauthorized {
		t.Fatalf("a carrier the claim does not accept is refused, got %d", rec.Code)
	}

	plain := httptest.NewRequest(http.MethodGet, "/api/board", nil)
	if rec, _ := serveGuard(t, f.org, shared.ACCESS_READ, plain); rec.Code != http.StatusUnauthorized {
		t.Fatalf("a claim with a code serves no request without it, got %d", rec.Code)
	}
}

func TestTheGuardRedactsTheCodeFromTheRequestAndTheSpan(t *testing.T) {
	f := getCodeFixture(t)
	value, _ := f.grantPublic(t, f.readers, &shared.ClaimOptions{Samk: &shared.ClaimOptionsSamk{Query: true}}, nil)

	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	t.Cleanup(func() { otel.SetTracerProvider(previous) })

	server := newAuthServerWithFailingRefresh(t)
	t.Cleanup(server.Close)
	_ = userAuthHandlerAgainst(t, server, nil)
	var seen guardAnswer
	handler := smw.TelemetryMiddleware()(smw.UserAuthMiddleware(nil)(func(ctx echo.Context) error {
		seen.Query = ctx.Request().URL.RawQuery + "|" + ctx.Request().RequestURI + "|" + ctx.QueryParam(smw.CodeParam)
		return ctx.NoContent(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/api/board?page=2&samk="+value, nil)
	req.Header.Set("Accept", "application/json")
	rec := httptest.NewRecorder()
	ctx := echo.New().NewContext(req, rec)
	if err := handler(ctx); err != nil {
		t.Fatalf("guard: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("an api call with samk is served, got %d", rec.Code)
	}
	if strings.Contains(seen.Query, value) || strings.Contains(seen.Query, "samk") {
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
			if strings.Contains(attr.Value.Emit(), value) {
				t.Fatalf("span attribute %s carries the code value", attr.Key)
			}
		}
		if strings.Contains(span.Name(), value) {
			t.Fatal("the span name carries the code value")
		}
	}
}

func TestARequestWithoutACredentialAnswers401WhenTheAppHoldsNoPublicClaim(t *testing.T) {
	f := getCodeFixture(t)
	if rec, _ := serveGuard(t, f.org, shared.ACCESS_READ, httptest.NewRequest(http.MethodGet, "/api/board", nil)); rec.Code != http.StatusUnauthorized {
		t.Fatalf("no public claim, no credential: 401, got %d", rec.Code)
	}
	nav := httptest.NewRequest(http.MethodGet, "/board", nil)
	nav.Header.Set("Accept", "text/html")
	if rec, _ := serveGuard(t, f.org, shared.ACCESS_READ, nav); rec.Code != http.StatusFound || rec.Header().Get("Location") != "/api/auth/login" {
		t.Fatalf("a navigation without a credential goes to the login, got %d %q", rec.Code, rec.Header().Get("Location"))
	}
	wrong := httptest.NewRequest(http.MethodGet, "/api/board", nil)
	wrong.Header.Set(smw.CodeHeader, "not-a-code")
	rec, _ := serveGuard(t, f.org, shared.ACCESS_READ, wrong)
	if rec.Code != http.StatusUnauthorized || rec.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Fatalf("an unknown code answers 401 with Referrer-Policy no-referrer, got %d %q", rec.Code, rec.Header().Get("Referrer-Policy"))
	}
}

func TestAnOpenPublicGrantServesEveryRoute(t *testing.T) {
	f := getCodeFixture(t)
	f.grantPublic(t, f.readers, nil, nil)

	rec, answer := serveGuard(t, f.org, shared.ACCESS_READ, httptest.NewRequest(http.MethodGet, "/api/board", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("an object covered by an open public grant is read without a credential, got %d %s", rec.Code, rec.Body.String())
	}
	f.assertPublic(t, answer, shared.AnonymousUserID, shared.ACCESS_READ)
	var session *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == analytics.AnonymousCookie {
			session = c
		}
	}
	if session == nil || session.Value != answer.Session || !session.HttpOnly {
		t.Fatalf("the first anonymous request mints the session key and sets it as an HttpOnly cookie, got %+v for %q", session, answer.Session)
	}
	again := httptest.NewRequest(http.MethodGet, "/api/board", nil)
	again.AddCookie(&http.Cookie{Name: analytics.AnonymousCookie, Value: answer.Session})
	if _, next := serveGuard(t, f.org, shared.ACCESS_READ, again); next.Session != answer.Session {
		t.Fatalf("the cookie carries the same session key back, got %q after %q", next.Session, answer.Session)
	}

	if rec, _ := serveGuard(t, f.hidden, shared.ACCESS_READ, httptest.NewRequest(http.MethodGet, "/api/board", nil)); rec.Code != http.StatusUnauthorized {
		t.Fatalf("an object no public grant covers answers 401, got %d", rec.Code)
	}
	for _, required := range []shared.ClaimAccess{shared.ACCESS_WRITE, shared.ACCESS_EXECUTE} {
		if rec, _ := serveGuard(t, f.org, required, httptest.NewRequest(http.MethodPost, "/api/booking", nil)); rec.Code != http.StatusUnauthorized {
			t.Fatalf("a public READ claim refuses access %d, got %d", required, rec.Code)
		}
	}
	var listed []uuid.UUID
	if err := sdb.Instance().NewRaw(`SELECT object_id FROM claimius.get_objects(?, 'claimius.organization', 4)`, shared.AnonymousUserID).Scan(context.Background(), &listed); err != nil || len(listed) != 1 || listed[0] != f.org {
		t.Fatalf("an anonymous listing holds what the public grants reach, got %v %v", listed, err)
	}
}

func TestAnOpenPublicGrantWithExecuteLetsAnyoneBook(t *testing.T) {
	f := getCodeFixture(t)
	bookers, _ := f.postClaim(t, "Bookers", shared.ACCESS_READ|shared.ACCESS_EXECUTE, f.org)
	f.grantPublic(t, bookers, nil, nil)

	rec, answer := serveGuard(t, f.org, shared.ACCESS_EXECUTE, httptest.NewRequest(http.MethodPost, "/api/booking", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("an open READ|EXECUTE claim lets anyone book, got %d %s", rec.Code, rec.Body.String())
	}
	if !answer.Anonymous || answer.Access != int(shared.ACCESS_READ|shared.ACCESS_EXECUTE) {
		t.Fatalf("the public grant holds what its claim grants, READ|EXECUTE, got anonymous %v access %d", answer.Anonymous, answer.Access)
	}
	if rec, _ := serveGuard(t, f.org, shared.ACCESS_WRITE, httptest.NewRequest(http.MethodPut, "/api/booking", nil)); rec.Code != http.StatusUnauthorized {
		t.Fatalf("WRITE the claim does not grant is refused, got %d", rec.Code)
	}
}

func (f codeFixture) postClaim(t *testing.T, name string, access shared.ClaimAccess, object uuid.UUID) (uuid.UUID, *shared.AccessClaim) {
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
	`, id, f.org, f.org, f.owner, object); err != nil {
		t.Fatalf("bind %s: %v", name, err)
	}
	ac, err := db.MutationAccess(ctx, f.owner, id, shared.OBJECT_TYPE_CLAIM, shared.ACCESS_OWNER)
	if err != nil {
		t.Fatalf("the owner must own claim %s: %v", name, err)
	}
	return id, ac
}

func TestAPublicCodeOnAManageClaimLetsItsHolderCancel(t *testing.T) {
	f := getCodeFixture(t)
	booking := f.hidden
	manage, _ := f.postClaim(t, "Manage booking", shared.ACCESS_READ|shared.ACCESS_WRITE|shared.ACCESS_EXECUTE, booking)
	value, _ := f.grantPublic(t, manage, &shared.ClaimOptions{Samk: &shared.ClaimOptionsSamk{Header: true}}, nil)

	cancel := httptest.NewRequest(http.MethodDelete, "/api/booking", nil)
	cancel.Header.Set(smw.CodeHeader, value)
	rec, answer := serveGuard(t, booking, shared.ACCESS_WRITE, cancel)
	if rec.Code != http.StatusOK {
		t.Fatalf("the holder of the code of a manage claim bound to the booking cancels it, got %d %s", rec.Code, rec.Body.String())
	}
	if answer.Access != int(shared.ACCESS_READ|shared.ACCESS_WRITE|shared.ACCESS_EXECUTE) {
		t.Fatalf("the code holds what its claim grants, got access %d", answer.Access)
	}

	other := httptest.NewRequest(http.MethodDelete, "/api/booking", nil)
	other.Header.Set(smw.CodeHeader, value)
	if rec, _ := serveGuard(t, f.org, shared.ACCESS_WRITE, other); rec.Code != http.StatusUnauthorized {
		t.Fatalf("the code reaches only what its claim is bound to, got %d", rec.Code)
	}
}

func TestAPublicGrantIsHeldByTheAnonymousUserAloneAndBehindItsCode(t *testing.T) {
	f := getCodeFixture(t)
	ctx := context.Background()
	db := sdb.Instance()
	viewer := uuid.New()
	if _, err := db.ExecContext(ctx, `SELECT claimius.upsert_app_user(p_user_id => ?, p_first_name => 'Viewer')`, viewer); err != nil {
		t.Fatalf("viewer: %v", err)
	}
	_, grantID := f.grantPublic(t, f.readers, nil, nil)
	if _, err := db.MutationAccess(ctx, viewer, f.org, shared.OBJECT_TYPE_ORGANIZATION, shared.ACCESS_READ); !errors.Is(err, shared.ErrNoAccess) {
		t.Fatalf("a public grant is the anonymous user's and no one else's, got %v", err)
	}
	ac, err := db.MutationAccess(ctx, shared.AnonymousUserID, f.org, shared.OBJECT_TYPE_ORGANIZATION, shared.ACCESS_READ)
	if err != nil {
		t.Fatalf("the anonymous user is answered like any user: %v", err)
	}
	if ac.Access != shared.ACCESS_READ || ac.UserClaimID == nil || *ac.UserClaimID != *f.ownerAC.UserClaimID {
		t.Fatalf("the grant carries the claim's mask and the token of the user who made it public, got %d %v", ac.Access, ac.UserClaimID)
	}
	var pairs int
	if err := db.NewRaw(`SELECT count(*) FROM claimius.user_object WHERE user_id = ? AND object_id = ?`, shared.AnonymousUserID, f.org).Scan(ctx, &pairs); err != nil || pairs != 1 {
		t.Fatalf("the anonymous user settles its pairs like any user, got %d %v", pairs, err)
	}

	options := &shared.ClaimOptions{Samk: &shared.ClaimOptionsSamk{Header: true}}
	value, err := options.Seal()
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	raw, _ := json.Marshal(options)
	if _, err := db.ExecContext(ctx, `SELECT claimius.update_user_claim(?, p_options => ?::jsonb)`, grantID, string(raw)); err != nil {
		t.Fatalf("grant options: %v", err)
	}
	shared.DeletePublicClaimCache()
	if _, err := db.MutationAccess(ctx, shared.AnonymousUserID, f.org, shared.OBJECT_TYPE_ORGANIZATION, shared.ACCESS_READ); !errors.Is(err, shared.ErrNoAccess) {
		t.Fatalf("a grant behind a code counts only while the request presents it, got %v", err)
	}
	presented := shared.WithCodeHash(ctx, shared.GetCodeHash(value))
	if ac, err := db.MutationAccess(presented, shared.AnonymousUserID, f.org, shared.OBJECT_TYPE_ORGANIZATION, shared.ACCESS_READ); err != nil || ac.Access != shared.ACCESS_READ {
		t.Fatalf("the presented code opens the grant in the walk, got %v %v", ac, err)
	}
	var listed []uuid.UUID
	if err := db.NewRaw(`SELECT object_id FROM claimius.get_objects(?, 'claimius.organization', 4)`, shared.AnonymousUserID).Scan(presented, &listed); err != nil || len(listed) != 1 || listed[0] != f.org {
		t.Fatalf("a listing with the code holds the object, got %v %v", listed, err)
	}
	if err := db.NewRaw(`SELECT object_id FROM claimius.get_objects(?, 'claimius.organization', 4)`, shared.AnonymousUserID).Scan(ctx, &listed); err != nil || len(listed) != 0 {
		t.Fatalf("a listing without the code holds nothing, got %v %v", listed, err)
	}
	if _, err := db.ExecContext(ctx, `SELECT claimius.update_user_claim(?, p_options => '{"samk": {"hash": "short", "header": true}}'::jsonb)`, grantID); err == nil || !strings.Contains(err.Error(), "22023") {
		t.Fatalf("options outside the shape are refused with 22023, got %v", err)
	}
	if _, err := db.ExecContext(ctx, `SELECT claimius.insert_user_claim(p_claim_id => ?, p_user_id => ?, p_sa_owner_id => ?, p_sa_created_by => ?, p_options => ?::jsonb)`, f.readers, viewer, f.org, *f.ownerAC.UserClaimID, string(raw)); err == nil || !strings.Contains(err.Error(), "only on a grant of the anonymous user") {
		t.Fatalf("options on another user's grant are refused, got %v", err)
	}
}

func TestAnAnonymousRequestIsRateLimitedFromTheEnvironment(t *testing.T) {
	f := getCodeFixture(t)
	t.Setenv("SAUTH_RATE_LIMIT", "3")
	t.Setenv("SAUTH_RATE_PERIOD", "2m")
	f.grantPublic(t, f.readers, nil, nil)
	ip := fmt.Sprintf("198.51.100.%d", time.Now().UnixNano()%250+1)
	request := func(value string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/api/board", nil)
		if value != "" {
			req.Header.Set(smw.CodeHeader, value)
		}
		req.Header.Set("X-Real-IP", ip)
		rec, _ := serveGuard(t, f.org, shared.ACCESS_READ, req)
		return rec
	}
	for i := range 3 {
		if rec := request(""); rec.Code != http.StatusOK {
			t.Fatalf("request %d under the limit is served, got %d", i, rec.Code)
		}
	}
	rec := request("")
	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") != "120" {
		t.Fatalf("past SAUTH_RATE_LIMIT requests per SAUTH_RATE_PERIOD an address gets 429 with Retry-After, got %d %q", rec.Code, rec.Header().Get("Retry-After"))
	}
	if rec := request("wrong"); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("a code resolution counts against the same limit, got %d", rec.Code)
	}
}

func TestAnInviteWithoutALoginKeepsTheCodeThroughTheLogin(t *testing.T) {
	f := getCodeFixture(t)
	invite := f.postCode(t, shared.BodyCode{Kind: shared.CODE_KIND_INVITE})

	req := httptest.NewRequest(http.MethodGet, "/board?samk="+invite.Value, nil)
	req.Header.Set("Accept", "text/html")
	rec, _ := serveGuard(t, f.org, shared.ACCESS_READ, req)
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
	if rec, _ := serveGuard(t, f.org, shared.ACCESS_READ, api); rec.Code != http.StatusUnauthorized {
		t.Fatalf("an api call with an invite and no login answers 401, got %d", rec.Code)
	}
}

func TestTheBuilderWritesAndChangesTheAnonymousUsersGrantWithItsCode(t *testing.T) {
	f := getCodeFixture(t)
	ctx := context.Background()
	db := sdb.Instance()

	var grant shared.UserClaim
	if err := db.NewInsert().Model(&shared.UserClaim{ClaimID: f.readers, UserID: shared.AnonymousUserID, Options: json.RawMessage(`{"samk": {"header": true}}`)}).Access(f.ownerAC).Scan(ctx, &grant); err != nil {
		t.Fatalf("grant the anonymous user through the builder: %v", err)
	}
	var minted shared.ClaimOptions
	if err := json.Unmarshal(grant.Options, &minted); err != nil || minted.Samk == nil || minted.Samk.Value == "" || minted.Samk.Hash != shared.GetCodeHash(minted.Samk.Value) {
		t.Fatalf("the answer carries the minted value once beside its hash, got %s %v", grant.Options, err)
	}
	var stored string
	if err := db.NewRaw(`SELECT options -> 'samk' ->> 'hash' FROM claimius.user_claim WHERE id = ?`, grant.ID).Scan(ctx, &stored); err != nil || stored != minted.Samk.Hash {
		t.Fatalf("the table holds the hash, got %q %v", stored, err)
	}
	var plain int
	if err := db.NewRaw(`SELECT count(*) FROM claimius.user_claim WHERE to_jsonb(user_claim)::TEXT LIKE '%' || ? || '%'`, minted.Samk.Value).Scan(ctx, &plain); err != nil || plain != 0 {
		t.Fatalf("no column holds the value, got %d %v", plain, err)
	}

	ends := time.Now().Add(time.Hour).Truncate(time.Second)
	var changed shared.UserClaim
	if err := db.NewUpdate().Model(&shared.UserClaim{ID: grant.ID, ClaimID: f.readers, EndsAt: &ends, Options: json.RawMessage(`{"samk": {"value": "4711", "query": true}}`)}).Column("ends_at", "options").Access(f.ownerAC).Scan(ctx, &changed); err != nil {
		t.Fatalf("change the grant through the builder: %v", err)
	}
	var chosen shared.ClaimOptions
	if err := json.Unmarshal(changed.Options, &chosen); err != nil || chosen.Samk == nil || chosen.Samk.Value != "4711" || !chosen.Samk.Query || chosen.Samk.Header {
		t.Fatalf("the update answers the chosen value once with the new carriers, got %s %v", changed.Options, err)
	}
	if changed.EndsAt == nil || !changed.EndsAt.Equal(ends) {
		t.Fatalf("the update sets ends_at, got %v", changed.EndsAt)
	}
	if err := db.NewRaw(`SELECT options -> 'samk' ->> 'hash' FROM claimius.user_claim WHERE id = ?`, grant.ID).Scan(ctx, &stored); err != nil || stored != shared.GetCodeHash("4711") {
		t.Fatalf("the table holds the new hash, got %q %v", stored, err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/board?samk=4711", nil)
	if rec, answer := serveGuard(t, f.org, shared.ACCESS_READ, req); rec.Code != http.StatusOK || answer.UserID != shared.AnonymousUserID {
		t.Fatalf("the changed code opens the grant in the query carrier, got %d %s", rec.Code, rec.Body.String())
	}

	var cleared shared.UserClaim
	if err := db.NewUpdate().Model(&shared.UserClaim{ID: grant.ID, ClaimID: f.readers, Options: json.RawMessage(`{}`)}).Column("options").Access(f.ownerAC).Scan(ctx, &cleared); err != nil || (len(cleared.Options) != 0 && string(cleared.Options) != "null") {
		t.Fatalf("{} clears the code, got %s %v", cleared.Options, err)
	}

	viewer := uuid.New()
	if _, err := db.ExecContext(ctx, `SELECT claimius.upsert_app_user(p_user_id => ?, p_first_name => 'Viewer')`, viewer); err != nil {
		t.Fatalf("viewer: %v", err)
	}
	var personal shared.UserClaim
	if err := db.NewInsert().Model(&shared.UserClaim{ClaimID: f.readers, UserID: viewer}).Access(f.ownerAC).Scan(ctx, &personal); err != nil {
		t.Fatalf("grant the viewer: %v", err)
	}
	if err := db.NewUpdate().Model(&shared.UserClaim{ID: personal.ID, ClaimID: f.readers, Options: json.RawMessage(`{"samk": {"header": true}}`)}).Column("options").Access(f.ownerAC).Scan(ctx, &personal); err == nil || !strings.Contains(err.Error(), "only on a grant of the anonymous user") {
		t.Fatalf("options on another user's grant are refused through the builder, got %v", err)
	}
}

func TestAnonymousMiddlewareResolvesOnlyWhenNoActorIsSet(t *testing.T) {
	f := getCodeFixture(t)
	value, _ := f.grantPublic(t, f.readers, &shared.ClaimOptions{Samk: &shared.ClaimOptionsSamk{Header: true}}, nil)
	server := newAuthServerWithFailingRefresh(t)
	t.Cleanup(server.Close)
	_ = userAuthHandlerAgainst(t, server, nil)
	handler := smw.AnonymousMiddleware(func(ctx echo.Context) error {
		userID, _ := ctx.Get(smw.UserIDKey).(uuid.UUID)
		ac, err := sdb.Instance().MutationAccess(ctx.Request().Context(), userID, f.org, shared.OBJECT_TYPE_ORGANIZATION, shared.ACCESS_READ)
		if err != nil {
			return ctx.String(http.StatusForbidden, "Forbidden")
		}
		return ctx.JSON(http.StatusOK, guardAnswer{UserID: ac.UserID, Access: int(ac.Access)})
	})

	req := httptest.NewRequest(http.MethodGet, "/api/board", nil)
	req.Header.Set(smw.CodeHeader, value)
	rec := httptest.NewRecorder()
	if err := handler(echo.New().NewContext(req, rec)); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("with no actor set the middleware resolves the anonymous user and its code, got %d %v %s", rec.Code, err, rec.Body.String())
	}
	var answer guardAnswer
	if err := json.Unmarshal(rec.Body.Bytes(), &answer); err != nil || answer.UserID != shared.AnonymousUserID || answer.Access != int(shared.ACCESS_READ) {
		t.Fatalf("the handler sees the anonymous user with the grant behind the code, got %+v %v", answer, err)
	}

	plain := httptest.NewRequest(http.MethodGet, "/api/board", nil)
	rec = httptest.NewRecorder()
	if err := handler(echo.New().NewContext(plain, rec)); err != nil || rec.Code != http.StatusUnauthorized {
		t.Fatalf("without the code the handler's 403 is answered 401, got %d %v", rec.Code, err)
	}

	preset := httptest.NewRequest(http.MethodGet, "/api/board", nil)
	preset.Header.Set(smw.CodeHeader, value)
	rec = httptest.NewRecorder()
	ctx := echo.New().NewContext(preset, rec)
	ctx.Set(smw.UserIDKey, f.owner)
	if err := handler(ctx); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("an actor the host already set passes through, got %d %v", rec.Code, err)
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &answer); err != nil || answer.UserID != f.owner || preset.Header.Get(smw.CodeHeader) != value {
		t.Fatalf("the middleware touches nothing when an actor is set, got %+v header %q", answer, preset.Header.Get(smw.CodeHeader))
	}
}
