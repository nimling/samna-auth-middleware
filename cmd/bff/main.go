package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/nimling/samna-auth-middleware/v2/bff"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := bff.Serve(ctx, bff.ConfigFromEnv()); err != nil {
		panic(err)
	}
}
