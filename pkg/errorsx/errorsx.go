package errorsx

import (
	"errors"
	"net/http"

	"github.com/uptrace/bun/driver/pgdriver"
)

var (
	ErrDbUnexpected = errors.New("unexpected database error")
	ErrUnexpected   = errors.New("unexpected error")

	ErrNotFound      = errors.New("not found")
	ErrBadRequest    = errors.New("bad request")
	ErrAlreadyExists = errors.New("already exists")
	ErrForbidden     = errors.New("forbidden")
)

func ErrToHttpCode(err error) int {
	var pgErr pgdriver.Error
	code := ""
	if errors.As(err, &pgErr) {
		code = pgErr.Field('C')
	}
	switch {
	case errors.Is(err, ErrNotFound):
		return http.StatusNotFound
	case errors.Is(err, ErrDbUnexpected), errors.Is(err, ErrUnexpected):
		return http.StatusInternalServerError
	case errors.Is(err, ErrBadRequest):
		return http.StatusBadRequest
	case errors.Is(err, ErrAlreadyExists):
		return http.StatusConflict
	case errors.Is(err, ErrForbidden):
		return http.StatusForbidden
	case code == "P0403":
		return http.StatusForbidden
	case code == "P0404":
		return http.StatusNotFound
	default:
		return http.StatusInternalServerError
	}
}

func IsUnexpected(err error) bool {
	return errors.Is(err, ErrUnexpected) || errors.Is(err, ErrDbUnexpected)
}
