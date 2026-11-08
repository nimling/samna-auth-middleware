package notification

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
)

func Stream(ctx echo.Context, userID uuid.UUID) error {
	r := current()
	if r == nil {
		return ErrNotStarted
	}
	res := ctx.Response()
	res.Header().Set(echo.HeaderContentType, "text/event-stream")
	res.Header().Set("Cache-Control", "no-cache")
	res.Header().Set("Connection", "keep-alive")
	res.Header().Set("X-Accel-Buffering", "no")
	res.WriteHeader(http.StatusOK)
	flusher, ok := res.Writer.(http.Flusher)
	if !ok {
		return errors.New("streaming unsupported")
	}
	flusher.Flush()

	client := r.hub.register(userID)
	defer r.hub.unregister(userID, client)

	ticker := time.NewTicker(heartbeat)
	defer ticker.Stop()

	done := ctx.Request().Context().Done()
	for {
		select {
		case <-done:
			return nil
		case <-r.stop:
			return nil
		case <-serveCtx.Done():
			return nil
		case msg, ok := <-client.Send:
			if !ok {
				return nil
			}
			if _, err := fmt.Fprintf(res.Writer, "data: %s\n\n", msg); err != nil {
				return nil
			}
			flusher.Flush()
		case <-ticker.C:
			if _, err := fmt.Fprint(res.Writer, ": heartbeat\n\n"); err != nil {
				return nil
			}
			flusher.Flush()
		}
	}
}
