package samnamw

import (
	"github.com/labstack/echo/v4"
)

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestLoggerMiddleware(t *testing.T) {
	// Setup
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	// Capture stdout
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	// Define test handler
	handler := func(c echo.Context) error {
		return c.String(http.StatusOK, "test")
	}

	// Wrap the test handler with our middleware
	h := AuthMiddleware(nil)(handler)

	// Perform the request
	err := h(c)

	// Restore stdout
	w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	io.Copy(&buf, r)

	// Assertions
	if err != nil {
		t.Errorf("Middleware returned an error: %v", err)
	}

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status code %d, got %d", http.StatusOK, rec.Code)
	}

	if rec.Body.String() != "test" {
		t.Errorf("Expected body 'test', got '%s'", rec.Body.String())
	}

	expectedLog := "Request: GET /test\n"
	if !strings.Contains(buf.String(), expectedLog) {
		t.Errorf("Expected log output to contain '%s', got '%s'", expectedLog, buf.String())
	}
}
