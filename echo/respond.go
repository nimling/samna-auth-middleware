package echo

import (
	"reflect"

	"github.com/labstack/echo/v4"
)

type returnError struct {
	Message string `json:"message"`
}

type returnObject struct {
	Data  any          `json:"data"`
	Error *returnError `json:"error,omitempty"`
	Count int          `json:"count"`
}

func respondError(ctx echo.Context, statusCode int, message string) error {
	return ctx.JSON(statusCode, returnObject{
		Count: 0,
		Data:  []any{},
		Error: &returnError{Message: message},
	})
}

func respondJSON(ctx echo.Context, statusCode int, data any, count int) error {
	rt := reflect.TypeOf(data)
	if rt != nil && rt.Kind() != reflect.Slice && rt.Kind() != reflect.Array {
		data = []any{data}
		if count == 0 {
			count = 1
		}
	}

	return ctx.JSON(statusCode, returnObject{
		Data:  data,
		Count: count,
	})
}
