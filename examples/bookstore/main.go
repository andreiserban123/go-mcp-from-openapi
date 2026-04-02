// Bookstore MCP demo
//
// Reads the bundled bookstore.yaml OpenAPI spec and exposes every operation as
// an MCP tool over the stdio transport (suitable for Claude Desktop, etc.).
//
// Usage:
//
//	go run ./examples/bookstore
//	go run ./examples/bookstore --base-url https://your-real-api.example.com/v1
//	go run ./examples/bookstore --http :8080
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	_ "embed"

	mcpopenapi "github.com/user/go-mcp-from-openapi"
)

//go:embed bookstore.yaml
var specYAML []byte

func main() {
	baseURL := flag.String("base-url", "", "Override the API base URL from the spec")
	httpAddr := flag.String("http", "", "Serve over HTTP on this address instead of stdio (e.g. :8080)")
	user := flag.String("user", "", "Basic auth username for the upstream API")
	pass := flag.String("pass", "", "Basic auth password for the upstream API")
	flag.Parse()

	opts := []mcpopenapi.Option{}
	if *baseURL != "" {
		opts = append(opts, mcpopenapi.WithBaseURL(*baseURL))
	}
	if *user != "" {
		opts = append(opts, mcpopenapi.WithBasicAuth(*user, *pass))
	}

	srv, err := mcpopenapi.FromOpenAPIYAML(specYAML, nil, "Bookstore API", opts...)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if *httpAddr != "" {
		log.Printf("bookstore MCP server listening on %s", *httpAddr)
		if err := srv.RunHTTP(ctx, *httpAddr); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
	} else {
		if err := srv.Run(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
	}
}
