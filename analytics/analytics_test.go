package analytics_test

import (
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/nimling/samna-auth-middleware/v2/analytics"
	"github.com/nimling/samna-auth-middleware/v2/shared"
)

func TestInitWithoutDatabaseIsRefused(t *testing.T) {
	if err := analytics.Init(t.Context()); !errors.Is(err, shared.ErrNotConnected) {
		t.Fatalf("init = %v, want ErrNotConnected", err)
	}
}

func TestCallsBeforeInitAnswerNotStarted(t *testing.T) {
	ctx := t.Context()
	app := uuid.New()
	if _, err := analytics.PostSession(ctx, app, "anon", "session", nil, nil, "", ""); !errors.Is(err, analytics.ErrNotStarted) {
		t.Fatalf("PostSession = %v, want ErrNotStarted", err)
	}
	if _, err := analytics.GetConsent(ctx, app, nil, "anon"); !errors.Is(err, analytics.ErrNotStarted) {
		t.Fatalf("GetConsent = %v, want ErrNotStarted", err)
	}
	if _, err := analytics.PostConsent(ctx, app, shared.ConsentRequest{AnonymousID: "anon"}, nil, "", ""); !errors.Is(err, analytics.ErrNotStarted) {
		t.Fatalf("PostConsent = %v, want ErrNotStarted", err)
	}
	analytics.Post(app, "session", []shared.AnalyticsEventInput{{}})
}
