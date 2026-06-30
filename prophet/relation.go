package prophet

import (
	"database/sql"
	"errors"
	"net/http"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/nimling/samna-auth-middleware/pkg/errorsx"
	"github.com/nimling/samna-auth-middleware/pkg/respond"
	prophetdb "github.com/nimling/samna-auth-middleware/prophet/db"
	"github.com/nimling/samna-auth-middleware/utils"
)

func GetRelations(ctx echo.Context) error {
	appID, _ := ctx.Get("app_id").(uuid.UUID)
	if _, err := utils.ValidateQueryParams(ctx, true, []string{}); err != nil {
		return respond.Error(ctx, http.StatusBadRequest, err.Error())
	}
	relations := make([]UserRelation, 0)
	if err := prophetdb.Instance().NewSelect().
		Model(&relations).
		ExcludeColumn("sa_deleted_at").
		Where("?TableAlias.app_id = ?", appID).
		Where("?TableAlias.sa_deleted_at IS NULL").
		OrderExpr("?TableAlias.priority DESC, ?TableAlias.used_at DESC NULLS LAST").
		Scan(ctx.Request().Context()); err != nil {
		ctx.Logger().Errorf("GetRelations: %v", err)
		return respond.Error(ctx, http.StatusInternalServerError, "Internal server error")
	}
	return respond.JSON(ctx, http.StatusOK, relations, len(relations))
}

func GetRelation(ctx echo.Context) error {
	appID, _ := ctx.Get("app_id").(uuid.UUID)
	relationID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return respond.Error(ctx, http.StatusBadRequest, "id must be a uuid")
	}
	relation, err := getRelation(ctx, relationID, appID)
	if err != nil {
		return respond.Error(ctx, errorsx.ErrToHttpCode(err), "")
	}
	return respond.JSON(ctx, http.StatusOK, relation, 1)
}

func PostRelation(ctx echo.Context) error {
	appID, _ := ctx.Get("app_id").(uuid.UUID)
	relation := &UserRelation{}
	if err := relation.Bind(ctx); err != nil {
		return respond.Error(ctx, http.StatusBadRequest, "invalid json body")
	}
	if err := relation.ValidatePost(); err != nil {
		return respond.Error(ctx, http.StatusBadRequest, err.Error())
	}
	relation.AppID = &appID
	created, err := postRelation(ctx, relation)
	if err != nil {
		return respond.Error(ctx, errorsx.ErrToHttpCode(err), err.Error())
	}
	return respond.JSON(ctx, http.StatusCreated, created, 1)
}

func PutRelation(ctx echo.Context) error {
	appID, _ := ctx.Get("app_id").(uuid.UUID)
	relationID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return respond.Error(ctx, http.StatusBadRequest, "id must be a uuid")
	}
	updated := &UserRelation{}
	if err := updated.Bind(ctx); err != nil {
		return respond.Error(ctx, http.StatusBadRequest, "invalid json body")
	}
	if err := updated.ValidatePatch(); err != nil {
		return respond.Error(ctx, http.StatusBadRequest, err.Error())
	}
	updated.ID = relationID
	updated.AppID = &appID
	if err := putRelation(ctx, updated, appID); err != nil {
		return respond.Error(ctx, errorsx.ErrToHttpCode(err), err.Error())
	}
	final, err := getRelation(ctx, relationID, appID)
	if err != nil {
		return respond.Error(ctx, errorsx.ErrToHttpCode(err), err.Error())
	}
	return respond.JSON(ctx, http.StatusOK, final, 1)
}

func DeleteRelation(ctx echo.Context) error {
	appID, _ := ctx.Get("app_id").(uuid.UUID)
	relationID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return respond.Error(ctx, http.StatusBadRequest, "id must be a uuid")
	}
	relation, err := deleteRelation(ctx, relationID, appID)
	if err != nil {
		return respond.Error(ctx, errorsx.ErrToHttpCode(err), "")
	}
	return respond.JSON(ctx, http.StatusOK, relation, 1)
}

func getRelation(ctx echo.Context, relationID, appID uuid.UUID) (*UserRelation, error) {
	var relation UserRelation
	err := prophetdb.Instance().NewSelect().
		Model(&relation).
		ExcludeColumn("sa_deleted_at").
		Where("?TableAlias.id = ?", relationID).
		Where("?TableAlias.app_id = ?", appID).
		Where("?TableAlias.sa_deleted_at IS NULL").
		Limit(1).
		Scan(ctx.Request().Context())
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errorsx.ErrNotFound
		}
		ctx.Logger().Errorf("getRelation: %v", err)
		return nil, errorsx.ErrDbUnexpected
	}
	return &relation, nil
}

func postRelation(ctx echo.Context, relation *UserRelation) (*UserRelation, error) {
	var created UserRelation
	if _, err := prophetdb.Instance().NewInsert().
		Model(relation).
		Returning("*").
		Exec(ctx.Request().Context(), &created); err != nil {
		ctx.Logger().Errorf("postRelation: %v", err)
		return nil, errorsx.ErrDbUnexpected
	}
	return &created, nil
}

func putRelation(ctx echo.Context, relation *UserRelation, appID uuid.UUID) error {
	res, err := prophetdb.Instance().NewUpdate().
		Model(relation).
		Where("sa_deleted_at IS NULL").
		Where("id = ?", relation.ID).
		Where("app_id = ?", appID).
		OmitZero().
		Returning("*").
		Exec(ctx.Request().Context())
	if err != nil {
		ctx.Logger().Errorf("putRelation: %v", err)
		return errorsx.ErrDbUnexpected
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return errorsx.ErrDbUnexpected
	}
	if affected == 0 {
		return errorsx.ErrNotFound
	}
	return nil
}

func deleteRelation(ctx echo.Context, relationID, appID uuid.UUID) (*UserRelation, error) {
	var relation UserRelation
	res, err := prophetdb.Instance().NewUpdate().
		Model(&relation).
		Set("sa_deleted_at = NOW()").
		Where("id = ?", relationID).
		Where("app_id = ?", appID).
		Where("sa_deleted_at IS NULL").
		Returning("*").
		Exec(ctx.Request().Context())
	if err != nil {
		ctx.Logger().Errorf("deleteRelation: %v", err)
		return nil, errorsx.ErrDbUnexpected
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return nil, errorsx.ErrDbUnexpected
	}
	if affected == 0 {
		return nil, errorsx.ErrNotFound
	}
	return &relation, nil
}
