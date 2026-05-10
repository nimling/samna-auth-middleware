package echo

import (
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/nimling/samna-auth-middleware/client"
	"github.com/nimling/samna-auth-middleware/db"
	"github.com/uptrace/bun"
)

func GetSecrets(ctx echo.Context) error {
	params := ctx.QueryParams()

	orgIDs := make([]uuid.UUID, 0)
	for _, raw := range params["org_id"] {
		for _, s := range strings.Split(raw, ",") {
			s = strings.TrimSpace(s)
			if s == "" {
				continue
			}
			id, err := uuid.Parse(s)
			if err != nil {
				return respondError(ctx, http.StatusBadRequest, "Bad request")
			}
			orgIDs = append(orgIDs, id)
		}
	}

	locIDs := make([]uuid.UUID, 0)
	for _, raw := range params["loc_id"] {
		for _, s := range strings.Split(raw, ",") {
			s = strings.TrimSpace(s)
			if s == "" {
				continue
			}
			id, err := uuid.Parse(s)
			if err != nil {
				return respondError(ctx, http.StatusBadRequest, "Bad request")
			}
			locIDs = append(locIDs, id)
		}
	}

	usrIDs := make([]uuid.UUID, 0)
	for _, raw := range params["usr_id"] {
		for _, s := range strings.Split(raw, ",") {
			s = strings.TrimSpace(s)
			if s == "" {
				continue
			}
			id, err := uuid.Parse(s)
			if err != nil {
				return respondError(ctx, http.StatusBadRequest, "Bad request")
			}
			usrIDs = append(usrIDs, id)
		}
	}

	if debugUserID := getDebugUserID(); debugUserID != nil {
		secrets := make([]db.SamnaSecret, 0)
		q := db.Instance().NewSelect().
			Model(&secrets).
		ExcludeColumn("sa_deleted_at").
			ExcludeColumn("app_id").
			Where("samna_secret.sa_deleted_at IS NULL").
			Where("samna_secret.id IN (SELECT id FROM claimius.get_secrets(?, claimius.get_disciple_app_id(), ?))", *debugUserID, db.ACCESS_OWNER)

		if len(orgIDs) > 0 {
			q.Where("samna_secret.sa_owner_id IN (?)", bun.In(orgIDs))
		}
		if len(locIDs) > 0 {
			q.Where("samna_secret.sa_owner_id IN (SELECT sa_owner_id FROM claimius.location WHERE sa_deleted_at IS NULL AND id IN (?))", bun.In(locIDs))
		}
		if len(usrIDs) > 0 {
			q.WhereGroup(" AND ", func(qq *bun.SelectQuery) *bun.SelectQuery {
				for _, uid := range usrIDs {
					qq = qq.WhereOr("samna_secret.id IN (SELECT id FROM claimius.get_secrets(?, claimius.get_disciple_app_id(), ?))", uid, db.ACCESS_OWNER)
				}
				return qq
			})
		}

		if err := q.Scan(ctx.Request().Context()); err != nil {
			ctx.Logger().Errorf("GetSecrets: %v", err)
			return respondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		return respondJSON(ctx, http.StatusOK, secrets, len(secrets))
	}

	body, err := client.GetSecrets(ctx.Request().Context(), ctx.QueryString())
	if err != nil {
		ctx.Logger().Errorf("GetSecrets: %v", err)
		return respondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	defer body.Close()
	return ctx.Stream(http.StatusOK, "application/json", body)
}

func GetSecret(ctx echo.Context) error {
	secretID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}

	if debugUserID := getDebugUserID(); debugUserID != nil {
		ac, err := db.Instance().GetAccess(ctx.Request().Context(), *debugUserID, secretID, db.OBJECT_TYPE_SAMNA_SECRET, db.ACCESS_OWNER)
		if err != nil {
			return respondError(ctx, http.StatusNotFound, "Not found")
		}
		if ac == nil {
			ac, err = db.Instance().GetDirectAccess(ctx.Request().Context(), *debugUserID, secretID, db.OBJECT_TYPE_SAMNA_SECRET)
			if err != nil || ac == nil {
				return respondError(ctx, http.StatusNotFound, "Not found")
			}
		}
		secret, err := db.Instance().GetSecret(ctx.Request().Context(), secretID)
		if err != nil {
			return respondError(ctx, http.StatusNotFound, "Not found")
		}
		return respondJSON(ctx, http.StatusOK, secret, 1)
	}

	body, err := client.GetSecret(ctx.Request().Context(), secretID)
	if err != nil {
		ctx.Logger().Errorf("GetSecret: %v", err)
		return respondError(ctx, http.StatusNotFound, "Not found")
	}
	defer body.Close()
	return ctx.Stream(http.StatusOK, "application/json", body)
}

func PostSecret(ctx echo.Context) error {
	var reqBody db.BodySecret
	if err := ctx.Bind(&reqBody); err != nil {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}
	if reqBody.OwnerID == nil || *reqBody.OwnerID == (uuid.UUID{}) {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}

	if debugUserID := getDebugUserID(); debugUserID != nil {
		ac, err := db.Instance().GetAccess(ctx.Request().Context(), *debugUserID, *reqBody.OwnerID, db.OBJECT_TYPE_ORGANIZATION, db.ACCESS_OWNER)
		if err != nil {
			ctx.Logger().Errorf("PostSecret GetAccess: %v", err)
			return respondError(ctx, http.StatusForbidden, "Forbidden")
		}
		if ac == nil {
			ac, err = db.Instance().GetDirectAccess(ctx.Request().Context(), *debugUserID, *reqBody.OwnerID, db.OBJECT_TYPE_ORGANIZATION)
			if err != nil || ac == nil {
				return respondError(ctx, http.StatusForbidden, "Forbidden")
			}
		}
		if ac.UserClaimID == nil || ac.ClaimID == nil {
			return respondError(ctx, http.StatusForbidden, "Forbidden")
		}
		secret, err := db.Instance().PostSecret(ctx.Request().Context(), reqBody, *reqBody.OwnerID, *ac.UserClaimID, ac.RootID, *ac.ClaimID)
		if err != nil {
			ctx.Logger().Errorf("PostSecret: %v", err)
			return respondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		return respondJSON(ctx, http.StatusCreated, secret, 1)
	}

	body, err := client.PostSecret(ctx.Request().Context(), reqBody)
	if err != nil {
		ctx.Logger().Errorf("PostSecret: %v", err)
		return respondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	defer body.Close()
	return ctx.Stream(http.StatusCreated, "application/json", body)
}

func PutSecret(ctx echo.Context) error {
	secretID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}

	var reqBody db.BodySecret
	if err := ctx.Bind(&reqBody); err != nil {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}

	if debugUserID := getDebugUserID(); debugUserID != nil {
		ac, err := db.Instance().GetAccess(ctx.Request().Context(), *debugUserID, secretID, db.OBJECT_TYPE_SAMNA_SECRET, db.ACCESS_OWNER)
		if err != nil {
			return respondError(ctx, http.StatusForbidden, "Forbidden")
		}
		if ac == nil {
			ac, err = db.Instance().GetDirectAccess(ctx.Request().Context(), *debugUserID, secretID, db.OBJECT_TYPE_SAMNA_SECRET)
			if err != nil || ac == nil {
				return respondError(ctx, http.StatusForbidden, "Forbidden")
			}
		}
		secret, err := db.Instance().PutSecret(ctx.Request().Context(), secretID, reqBody)
		if err != nil {
			ctx.Logger().Errorf("PutSecret: %v", err)
			return respondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		return respondJSON(ctx, http.StatusOK, secret, 1)
	}

	body, err := client.PutSecret(ctx.Request().Context(), secretID, reqBody)
	if err != nil {
		ctx.Logger().Errorf("PutSecret: %v", err)
		return respondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	defer body.Close()
	return ctx.Stream(http.StatusOK, "application/json", body)
}

func DeleteSecret(ctx echo.Context) error {
	secretID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return respondError(ctx, http.StatusBadRequest, "Bad request")
	}

	if debugUserID := getDebugUserID(); debugUserID != nil {
		ac, err := db.Instance().GetAccess(ctx.Request().Context(), *debugUserID, secretID, db.OBJECT_TYPE_SAMNA_SECRET, db.ACCESS_OWNER)
		if err != nil {
			return respondError(ctx, http.StatusForbidden, "Forbidden")
		}
		if ac == nil {
			ac, err = db.Instance().GetDirectAccess(ctx.Request().Context(), *debugUserID, secretID, db.OBJECT_TYPE_SAMNA_SECRET)
			if err != nil || ac == nil {
				return respondError(ctx, http.StatusForbidden, "Forbidden")
			}
		}
		if err := db.Instance().DeleteSecret(ctx.Request().Context(), secretID); err != nil {
			ctx.Logger().Errorf("DeleteSecret: %v", err)
			return respondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		return ctx.NoContent(http.StatusOK)
	}

	body, err := client.DeleteSecret(ctx.Request().Context(), secretID)
	if err != nil {
		ctx.Logger().Errorf("DeleteSecret: %v", err)
		return respondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	defer body.Close()
	return ctx.Stream(http.StatusOK, "application/json", body)
}
