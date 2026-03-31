package echo

import (
	"net/http"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/nimling/samna-auth-middleware/client"
	"github.com/nimling/samna-auth-middleware/db"
)

func GetRelations(ctx echo.Context) error {
	userID := ctx.Get("user_id").(uuid.UUID)

	relations, err := db.Instance().GetUserRelations(ctx.Request().Context(), userID)
	if err != nil {
		ctx.Logger().Errorf("GetRelations: %v", err)
		return RespondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	return RespondJSON(ctx, http.StatusOK, relations, len(relations))
}

func GetRelation(ctx echo.Context) error {
	relationID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return RespondError(ctx, http.StatusBadRequest, "Bad request")
	}

	userID := ctx.Get("user_id").(uuid.UUID)

	relations, err := db.Instance().GetRelationByUserAndObject(ctx.Request().Context(), userID, relationID)
	if err != nil {
		ctx.Logger().Errorf("GetRelation: %v", err)
		return RespondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	return RespondJSON(ctx, http.StatusOK, relations, nil)
}

func PostRelation(ctx echo.Context) error {
	userID := ctx.Get("user_id").(uuid.UUID)

	var relation db.UserRelation
	if err := ctx.Bind(&relation); err != nil {
		return RespondError(ctx, http.StatusBadRequest, "Bad request")
	}
	relation.UserID = userID

	result, err := client.PostRelation(ctx.Request().Context(), relation)
	if err != nil {
		ctx.Logger().Errorf("PostRelation: %v", err)
		return RespondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	return RespondJSON(ctx, http.StatusOK, result, 1)
}

func PutRelation(ctx echo.Context) error {
	relationID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return RespondError(ctx, http.StatusBadRequest, "Bad request")
	}

	var relation db.UserRelation
	if err := ctx.Bind(&relation); err != nil {
		return RespondError(ctx, http.StatusBadRequest, "Bad request")
	}

	result, err := client.PutRelation(ctx.Request().Context(), relationID, relation)
	if err != nil {
		ctx.Logger().Errorf("PutRelation: %v", err)
		return RespondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	return RespondJSON(ctx, http.StatusOK, result, 1)
}

func DeleteRelation(ctx echo.Context) error {
	relationID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return RespondError(ctx, http.StatusBadRequest, "Bad request")
	}

	result, err := client.DeleteRelation(ctx.Request().Context(), relationID)
	if err != nil {
		ctx.Logger().Errorf("DeleteRelation: %v", err)
		return RespondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	return RespondJSON(ctx, http.StatusOK, result, 1)
}
