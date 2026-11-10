package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	sam "github.com/nimling/samna-auth-middleware/v2/disciple/sam"
	sdb "github.com/nimling/samna-auth-middleware/v2/disciple/sdb"
	"github.com/nimling/samna-auth-middleware/v2/notification"
	"github.com/nimling/samna-auth-middleware/v2/shared"
	smw "github.com/nimling/samna-auth-middleware/v2/smw"
	"github.com/nimling/samna-auth-middleware/v2/uptime"
)

type accessCheck struct {
	ObjectID   uuid.UUID          `json:"object_id"`
	ObjectType string             `json:"object_type"`
	Required   shared.ClaimAccess `json:"required"`
}

type accessVerdict struct {
	ObjectID  uuid.UUID `json:"object_id"`
	Granted   bool      `json:"granted"`
	ClaimName *string   `json:"claim_name"`
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	e := echo.New()

	if _, err := sdb.Init(e.Logger); err != nil {
		e.Logger.Fatal(err)
	}

	sam.RegisterStatus(e.Group(uptime.StatusBasePath))
	sam.RegisterCli(e.Group("/api/auth"))

	sam.RegisterEndpoints(e.Group("", smw.UserAuthMiddleware(nil)))

	api := e.Group("", smw.UserAuthMiddleware(nil))

	api.GET("/api/access/:type/:id", GetAccess)
	api.POST("/api/access", PostAccessBatch)
	api.GET("/api/user/:target", GetUserAccess)
	api.GET("/api/root", GetRootAccess)
	api.GET("/api/graph/owner", GetOwnerGraph)
	api.GET("/api/graph/claim", GetClaimGraph)
	api.GET("/api/organizations", GetOrganizations)
	api.GET("/api/locations", GetLocations)

	runCtx, cancelRun := context.WithCancel(context.Background())
	defer cancelRun()
	if err := sam.Init(runCtx); err != nil {
		e.Logger.Fatal(err)
	}
	e.Server.RegisterOnShutdown(notification.EndStreams)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	go func() {
		if err := e.Start(":" + port); err != nil && !errors.Is(err, http.ErrServerClosed) {
			e.Logger.Fatal(err)
		}
	}()
	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := e.Shutdown(shutdownCtx); err != nil {
		e.Logger.Errorf("shutdown: %v", err)
	}
	cancelRun()
	<-sam.Done()
	if err := sdb.Instance().Close(); err != nil {
		e.Logger.Errorf("close database: %v", err)
	}
}

func actor(ctx echo.Context) uuid.UUID {
	id, _ := ctx.Get(smw.UserIDKey).(uuid.UUID)
	return id
}

func accessError(ctx echo.Context, err error) error {
	switch {
	case errors.Is(err, shared.ErrNoAccess):
		return ctx.JSON(http.StatusForbidden, map[string]string{"error": "no access"})
	case errors.Is(err, shared.ErrNotFound):
		return ctx.JSON(http.StatusNotFound, map[string]string{"error": "not found"})
	default:
		return ctx.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
}

func intQuery(ctx echo.Context, key string, fallback int) int {
	if v, err := strconv.Atoi(ctx.QueryParam(key)); err == nil {
		return v
	}
	return fallback
}

func uuidQuery(ctx echo.Context, key string) *uuid.UUID {
	if v, err := uuid.Parse(ctx.QueryParam(key)); err == nil {
		return &v
	}
	return nil
}

func graphWindow(ctx echo.Context) shared.GraphWindow {
	return shared.GraphWindow{Width: intQuery(ctx, "width", 0), Sort: ctx.QueryParam("sort"), NextID: uuidQuery(ctx, "next_id")}
}

func GetAccess(ctx echo.Context) error {
	objectID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return ctx.JSON(http.StatusBadRequest, map[string]string{"error": "id must be a uuid"})
	}
	required := shared.ClaimAccess(intQuery(ctx, "required", int(shared.ACCESS_READ)))
	ac, err := sdb.Instance().MutationAccess(ctx.Request().Context(), actor(ctx), objectID, "claimius."+ctx.Param("type"), required)
	if err != nil {
		return accessError(ctx, err)
	}
	return ctx.JSON(http.StatusOK, ac)
}

func PostAccessBatch(ctx echo.Context) error {
	var checks []accessCheck
	if err := ctx.Bind(&checks); err != nil {
		return ctx.JSON(http.StatusBadRequest, map[string]string{"error": "body must be a list of checks"})
	}
	user := actor(ctx)
	requests := make([]shared.AccessRequest, len(checks))
	for i, c := range checks {
		requests[i] = shared.AccessRequest{UserID: user, ObjectID: c.ObjectID, ObjectType: c.ObjectType, RequiredAccess: c.Required}
	}
	set, err := sdb.Instance().MutationAccessMany(ctx.Request().Context(), requests)
	if err != nil {
		return accessError(ctx, err)
	}
	verdicts := make([]accessVerdict, len(checks))
	for i, c := range checks {
		claim := set.Get(c.ObjectType, c.ObjectID, user)
		verdicts[i] = accessVerdict{ObjectID: c.ObjectID, Granted: set.Has(c.ObjectType, c.ObjectID, user, c.Required)}
		if claim != nil {
			verdicts[i].ClaimName = claim.ClaimName
		}
	}
	return ctx.JSON(http.StatusOK, verdicts)
}

func GetUserAccess(ctx echo.Context) error {
	u, err := sdb.Instance().UserAccess(ctx.Request().Context(), actor(ctx), ctx.Param("target"), 0)
	if err != nil {
		return accessError(ctx, err)
	}
	return ctx.JSON(http.StatusOK, u)
}

func GetRootAccess(ctx echo.Context) error {
	ac, err := sdb.Instance().RootAccess(ctx.Request().Context(), actor(ctx), uuid.Nil, shared.ACCESS_READ)
	if err != nil {
		return accessError(ctx, err)
	}
	return ctx.JSON(http.StatusOK, ac)
}

func GetOwnerGraph(ctx echo.Context) error {
	nodes, err := sdb.Instance().GetOwnerGraph(ctx.Request().Context(), actor(ctx), uuidQuery(ctx, "start_id"),
		intQuery(ctx, "depth", 0), shared.ClaimAccess(intQuery(ctx, "access", int(shared.ACCESS_READ))), graphWindow(ctx))
	if err != nil {
		return accessError(ctx, err)
	}
	return ctx.JSON(http.StatusOK, nodes)
}

func GetClaimGraph(ctx echo.Context) error {
	nodes, err := sdb.Instance().GetClaimGraph(ctx.Request().Context(), actor(ctx), uuidQuery(ctx, "start_id"),
		intQuery(ctx, "depth", 0), shared.ClaimAccess(intQuery(ctx, "access", int(shared.ACCESS_READ))), graphWindow(ctx))
	if err != nil {
		return accessError(ctx, err)
	}
	return ctx.JSON(http.StatusOK, nodes)
}

func GetOrganizations(ctx echo.Context) error {
	after, limit, err := shared.QueryPage(ctx)
	if err != nil {
		return ctx.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}
	orgs := make([]shared.Organization, 0)
	if err := sdb.Instance().NewRaw("SELECT * FROM claimius.get_organizations(?, ?, ?, ?)", actor(ctx),
		int(shared.ClaimAccess(intQuery(ctx, "required", int(shared.ACCESS_READ)))), after, nullableLimit(limit)).Scan(ctx.Request().Context(), &orgs); err != nil {
		return accessError(ctx, err)
	}
	return ctx.JSON(http.StatusOK, orgs)
}

func GetLocations(ctx echo.Context) error {
	after, limit, err := shared.QueryPage(ctx)
	if err != nil {
		return ctx.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}
	locs := make([]shared.Location, 0)
	if err := sdb.Instance().NewRaw("SELECT * FROM claimius.get_locations(?, ?, ?, ?)", actor(ctx),
		int(shared.ClaimAccess(intQuery(ctx, "required", int(shared.ACCESS_READ)))), after, nullableLimit(limit)).Scan(ctx.Request().Context(), &locs); err != nil {
		return accessError(ctx, err)
	}
	return ctx.JSON(http.StatusOK, locs)
}

func nullableLimit(limit int) *int {
	if limit <= 0 {
		return nil
	}
	return &limit
}
