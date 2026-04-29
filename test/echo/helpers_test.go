package echo_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
)

type response struct {
	Data      json.RawMessage `json:"data"`
	Count     int             `json:"count"`
	RequestID uuid.UUID       `json:"request_id"`
	Error     *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func newCtx(t *testing.T, method, target string, body any) (echo.Context, *httptest.ResponseRecorder) {
	t.Helper()

	var reader io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		reader = bytes.NewReader(buf)
	}

	req := httptest.NewRequest(method, target, reader)
	if body != nil {
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	}
	rec := httptest.NewRecorder()
	return testEcho.NewContext(req, rec), rec
}

func setParam(ctx echo.Context, key, value string) {
	names := append([]string{}, ctx.ParamNames()...)
	values := append([]string{}, ctx.ParamValues()...)
	names = append(names, key)
	values = append(values, value)
	ctx.SetParamNames(names...)
	ctx.SetParamValues(values...)
}

func decodeResponse(t *testing.T, rec *httptest.ResponseRecorder) response {
	t.Helper()
	var r response
	if rec.Body.Len() == 0 {
		return r
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &r); err != nil {
		t.Fatalf("decode response: %v\nbody: %s", err, rec.Body.String())
	}
	return r
}

func decodeData[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	r := decodeResponse(t, rec)
	var v T
	if len(r.Data) == 0 {
		return v
	}
	if err := json.Unmarshal(r.Data, &v); err != nil {
		t.Fatalf("decode data: %v\ndata: %s", err, string(r.Data))
	}
	return v
}

func assertStatus(t *testing.T, rec *httptest.ResponseRecorder, want int) {
	t.Helper()
	if rec.Code != want {
		t.Fatalf("status: got %d want %d\nbody: %s", rec.Code, want, rec.Body.String())
	}
}

func mustParseUUID(t *testing.T, s string) uuid.UUID {
	t.Helper()
	id, err := uuid.Parse(s)
	if err != nil {
		t.Fatalf("parse uuid %q: %v", s, err)
	}
	return id
}

var _ = http.StatusOK
