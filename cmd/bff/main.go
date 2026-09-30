package main

import (
	"context"

	"github.com/nimling/samna-auth-middleware/v2/bff"
)

func main() {
	if err := bff.Serve(context.Background(), bff.ConfigFromEnv()); err != nil {
		panic(err)
	}
}
