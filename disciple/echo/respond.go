package echo

import (
	"encoding/json"
	"reflect"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
)

type returnError struct {
	Message string `json:"message"`
}

type returnObject struct {
	Data      any          `json:"data"`
	Error     *returnError `json:"error,omitempty"`
	RequestID uuid.UUID    `json:"request_id"`
	Count     int          `json:"count"`
}

func getRequestID(ctx echo.Context) uuid.UUID {
	if id, ok := ctx.Get("request_id").(uuid.UUID); ok {
		return id
	}
	return uuid.New()
}

func Redirect(ctx echo.Context, statusCode int, url string) error {
	return ctx.Redirect(statusCode, url)
}

func respondError(ctx echo.Context, statusCode int, message string) error {
	returnData := returnObject{
		Count:     0,
		Data:      []any{},
		Error:     &returnError{Message: message},
		RequestID: getRequestID(ctx),
	}

	ctx.Response().Header().Set(echo.HeaderContentType, echo.MIMEApplicationJSONCharsetUTF8)
	ctx.Response().WriteHeader(statusCode)
	enc := json.NewEncoder(ctx.Response())
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	return enc.Encode(returnData)
}

func respondJSON(ctx echo.Context, statusCode int, data any, count int) error {
	rt := reflect.TypeOf(data)
	switch {
	case rt != nil && (rt.Kind() == reflect.Slice || rt.Kind() == reflect.Array):
		count = reflect.ValueOf(data).Len()
	default:
		count = 1
	}

	returnData := returnObject{
		Data:      data,
		Count:     count,
		RequestID: getRequestID(ctx),
	}

	ctx.Response().Header().Set(echo.HeaderContentType, echo.MIMEApplicationJSONCharsetUTF8)
	ctx.Response().WriteHeader(statusCode)
	enc := json.NewEncoder(ctx.Response())
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	return enc.Encode(returnData)
}
