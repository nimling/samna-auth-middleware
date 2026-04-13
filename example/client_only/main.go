//go:build example

package main

import (
	"context"
	"fmt"
	"io"
	"log"

	"github.com/joho/godotenv"
	"github.com/nimling/samna-auth-middleware/client"
)

func main() {
	godotenv.Load()

	meta := client.RequestMetadata{
		AccessToken: "bearer-token-from-caller",
		RequestID:   "req-001",
		UserIP:      "127.0.0.1",
	}

	ctx := context.Background()

	body, err := client.GetUsers(ctx, meta)
	if err != nil {
		log.Fatal(err)
	}
	defer body.Close()

	data, _ := io.ReadAll(body)
	fmt.Println(string(data))
}
