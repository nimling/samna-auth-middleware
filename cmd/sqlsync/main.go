package main

import (
	"flag"
	"fmt"
	"os"

	claimius "github.com/nimling/samna-auth-middleware"
)

func main() {
	role := flag.String("role", "", "prophet or disciple")
	out := flag.String("out", "", "output directory; shared SQL lands in <out>/shared, role SQL in <out>/main")
	flag.Parse()

	if *role == "" || *out == "" {
		fmt.Fprintln(os.Stderr, "usage: sqlsync --role=prophet|disciple --out=<dir>")
		os.Exit(2)
	}

	if err := claimius.WriteRole(*role, *out); err != nil {
		fmt.Fprintf(os.Stderr, "sqlsync: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("wrote %s claimius SQL to %s\n", *role, *out)
}
