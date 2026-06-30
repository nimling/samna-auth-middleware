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

func GetObjectFields(ctx echo.Context) error {
	appID, _ := ctx.Get("app_id").(uuid.UUID)
	params, err := utils.ValidateQueryParams(ctx, true, []string{"object_type"})
	if err != nil {
		return respond.Error(ctx, http.StatusBadRequest, err.Error())
	}
	fields, err := fieldObjectFields(ctx, appID, params.Get("object_type"))
	if err != nil {
		ctx.Logger().Error(err)
		return respond.Error(ctx, http.StatusInternalServerError, "object_field list query failed")
	}
	return respond.JSON(ctx, http.StatusOK, fields, len(fields))
}

func GetObjectField(ctx echo.Context) error {
	appID, _ := ctx.Get("app_id").(uuid.UUID)
	fieldID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return respond.Error(ctx, http.StatusBadRequest, "id must be a uuid")
	}
	field, err := fieldObjectField(ctx, fieldID, appID)
	if err != nil {
		return respond.Error(ctx, errorsx.ErrToHttpCode(err), "")
	}
	return respond.JSON(ctx, http.StatusOK, field, 1)
}

func PostObjectField(ctx echo.Context) error {
	appID, _ := ctx.Get("app_id").(uuid.UUID)

	field := &ObjectField{}
	if err := field.Bind(ctx); err != nil {
		return respond.Error(ctx, http.StatusBadRequest, "invalid json body")
	}
	if err := field.ValidatePost(); err != nil {
		return respond.Error(ctx, http.StatusBadRequest, err.Error())
	}

	field.AppID = appID

	if err := fieldPostObjectField(ctx, field); err != nil {
		ctx.Logger().Error(err)
		return respond.Error(ctx, http.StatusInternalServerError, "object_field insert failed")
	}
	return respond.JSON(ctx, http.StatusCreated, field, 1)
}

func PutObjectField(ctx echo.Context) error {
	appID, _ := ctx.Get("app_id").(uuid.UUID)
	fieldID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return respond.Error(ctx, http.StatusBadRequest, "id must be a uuid")
	}

	existing, err := fieldObjectField(ctx, fieldID, appID)
	if err != nil {
		return respond.Error(ctx, errorsx.ErrToHttpCode(err), "")
	}

	patch := &ObjectField{}
	if err := patch.Bind(ctx); err != nil {
		return respond.Error(ctx, http.StatusBadRequest, "invalid json body")
	}
	if err := patch.ValidatePatch(); err != nil {
		return respond.Error(ctx, http.StatusBadRequest, err.Error())
	}

	if patch.HasKey("name") {
		existing.Name = patch.Name
	}
	if patch.HasKey("description") {
		existing.Description = patch.Description
	}
	if patch.HasKey("value") {
		existing.Value = patch.Value
	}
	if patch.HasKey("required") {
		existing.Required = patch.Required
	}
	if patch.HasKey("json_schema") {
		existing.JsonSchema = patch.JsonSchema
	}

	if err := fieldPutObjectField(ctx, fieldID, appID, existing); err != nil {
		ctx.Logger().Error(err)
		return respond.Error(ctx, http.StatusInternalServerError, "object_field update failed")
	}
	return respond.JSON(ctx, http.StatusOK, existing, 1)
}

func DeleteObjectField(ctx echo.Context) error {
	appID, _ := ctx.Get("app_id").(uuid.UUID)
	fieldID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return respond.Error(ctx, http.StatusBadRequest, "id must be a uuid")
	}
	deleted, err := fieldDeleteObjectField(ctx, fieldID, appID)
	if err != nil {
		return respond.Error(ctx, errorsx.ErrToHttpCode(err), "")
	}
	return respond.JSON(ctx, http.StatusOK, deleted, 1)
}

func GetUserFields(ctx echo.Context) error {
	appID, _ := ctx.Get("app_id").(uuid.UUID)
	fieldID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return respond.Error(ctx, http.StatusBadRequest, "id must be a uuid")
	}
	if _, err := utils.ValidateQueryParams(ctx, true, []string{}); err != nil {
		return respond.Error(ctx, http.StatusBadRequest, err.Error())
	}
	values, err := fieldUserFields(ctx, fieldID, appID)
	if err != nil {
		ctx.Logger().Error(err)
		return respond.Error(ctx, http.StatusInternalServerError, "user_field list query failed")
	}
	return respond.JSON(ctx, http.StatusOK, values, len(values))
}

func PostUserField(ctx echo.Context) error {
	appID, _ := ctx.Get("app_id").(uuid.UUID)
	fieldID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return respond.Error(ctx, http.StatusBadRequest, "id must be a uuid")
	}

	value := &UserField{}
	if err := value.Bind(ctx); err != nil {
		return respond.Error(ctx, http.StatusBadRequest, "invalid json body")
	}

	value.AppID = appID
	value.FieldID = fieldID

	if err := value.ValidatePost(); err != nil {
		return respond.Error(ctx, http.StatusBadRequest, err.Error())
	}

	if _, err := prophetdb.Instance().NewInsert().
		Model(value).
		Returning("*").
		Exec(ctx.Request().Context()); err != nil {
		ctx.Logger().Error(err)
		return respond.Error(ctx, http.StatusInternalServerError, "user_field insert failed")
	}
	return respond.JSON(ctx, http.StatusCreated, value, 1)
}

func PutUserField(ctx echo.Context) error {
	appID, _ := ctx.Get("app_id").(uuid.UUID)
	fieldID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return respond.Error(ctx, http.StatusBadRequest, "id must be a uuid")
	}
	valueID, err := uuid.Parse(ctx.Param("value_id"))
	if err != nil {
		return respond.Error(ctx, http.StatusBadRequest, "value_id must be a uuid")
	}

	patch := &UserField{}
	if err := patch.Bind(ctx); err != nil {
		return respond.Error(ctx, http.StatusBadRequest, "invalid json body")
	}
	if err := patch.ValidatePatch(); err != nil {
		return respond.Error(ctx, http.StatusBadRequest, err.Error())
	}

	res, err := prophetdb.Instance().NewUpdate().
		Model(patch).
		Where("sa_deleted_at IS NULL").
		Where("id = ?", valueID).
		Where("field_id = ?", fieldID).
		Where("app_id = ?", appID).
		OmitZero().
		Returning("*").
		Exec(ctx.Request().Context())
	if err != nil {
		ctx.Logger().Error(err)
		return respond.Error(ctx, http.StatusInternalServerError, "user_field update failed")
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return respond.Error(ctx, http.StatusInternalServerError, "user_field update failed")
	}
	if affected == 0 {
		return respond.Error(ctx, errorsx.ErrToHttpCode(errorsx.ErrNotFound), "")
	}
	return respond.JSON(ctx, http.StatusOK, patch, 1)
}

func DeleteUserField(ctx echo.Context) error {
	appID, _ := ctx.Get("app_id").(uuid.UUID)
	fieldID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return respond.Error(ctx, http.StatusBadRequest, "id must be a uuid")
	}
	valueID, err := uuid.Parse(ctx.Param("value_id"))
	if err != nil {
		return respond.Error(ctx, http.StatusBadRequest, "value_id must be a uuid")
	}

	var value UserField
	res, err := prophetdb.Instance().NewUpdate().
		Model(&value).
		Set("sa_deleted_at = NOW()").
		Where("id = ?", valueID).
		Where("field_id = ?", fieldID).
		Where("app_id = ?", appID).
		Where("sa_deleted_at IS NULL").
		Returning("*").
		Exec(ctx.Request().Context())
	if err != nil {
		ctx.Logger().Error(err)
		return respond.Error(ctx, http.StatusInternalServerError, "user_field delete failed")
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return respond.Error(ctx, http.StatusInternalServerError, "user_field delete failed")
	}
	if affected == 0 {
		return respond.Error(ctx, errorsx.ErrToHttpCode(errorsx.ErrNotFound), "")
	}
	return respond.JSON(ctx, http.StatusOK, &value, 1)
}

func fieldObjectFields(ctx echo.Context, appID uuid.UUID, objectType string) ([]ObjectField, error) {
	fields := make([]ObjectField, 0)
	query := prophetdb.Instance().NewSelect().
		Model(&fields).
		ExcludeColumn("sa_deleted_at").
		Where("app_id = ?", appID).
		Where("sa_deleted_at IS NULL")

	if objectType != "" {
		query = query.Where("object_type = ?", objectType)
	}

	if err := query.Scan(ctx.Request().Context()); err != nil {
		return nil, err
	}
	return fields, nil
}

func fieldObjectField(ctx echo.Context, fieldID, appID uuid.UUID) (*ObjectField, error) {
	var field ObjectField
	err := prophetdb.Instance().NewSelect().
		Model(&field).
		ExcludeColumn("sa_deleted_at").
		Where("id = ?", fieldID).
		Where("app_id = ?", appID).
		Where("sa_deleted_at IS NULL").
		Limit(1).
		Scan(ctx.Request().Context())
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errorsx.ErrNotFound
		}
		return nil, errorsx.ErrDbUnexpected
	}
	return &field, nil
}

func fieldPostObjectField(ctx echo.Context, field *ObjectField) error {
	_, err := prophetdb.Instance().NewInsert().
		Model(field).
		Returning("*").
		Exec(ctx.Request().Context())
	return err
}

func fieldPutObjectField(ctx echo.Context, fieldID, appID uuid.UUID, field *ObjectField) error {
	res, err := prophetdb.Instance().NewUpdate().
		Model(field).
		Where("sa_deleted_at IS NULL").
		Where("id = ?", fieldID).
		Where("app_id = ?", appID).
		Returning("*").
		Exec(ctx.Request().Context())
	if err != nil {
		return err
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

func fieldDeleteObjectField(ctx echo.Context, fieldID, appID uuid.UUID) (*ObjectField, error) {
	var field ObjectField
	res, err := prophetdb.Instance().NewUpdate().
		Model(&field).
		Set("sa_deleted_at = NOW()").
		Where("id = ?", fieldID).
		Where("app_id = ?", appID).
		Where("sa_deleted_at IS NULL").
		Returning("*").
		Exec(ctx.Request().Context())
	if err != nil {
		return nil, errorsx.ErrDbUnexpected
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return nil, errorsx.ErrDbUnexpected
	}
	if affected == 0 {
		return nil, errorsx.ErrNotFound
	}
	return &field, nil
}

func fieldUserFields(ctx echo.Context, fieldID, appID uuid.UUID) ([]UserField, error) {
	values := make([]UserField, 0)
	err := prophetdb.Instance().NewSelect().
		Model(&values).
		ExcludeColumn("sa_deleted_at").
		Where("field_id = ?", fieldID).
		Where("app_id = ?", appID).
		Where("sa_deleted_at IS NULL").
		Scan(ctx.Request().Context())
	if err != nil {
		return nil, err
	}
	return values, nil
}
