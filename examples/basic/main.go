package main

import (
	"context"
	"log"
	"net/http"

	mcpopenapi "github.com/user/go-mcp-from-openapi"
)

func main() {
	// Fetch the OpenAPI spec from the API.
	spec, err := mcpopenapi.FetchSpec("https://api.example.com/openapi.json")
	if err != nil {
		log.Fatalf("fetch spec: %v", err)
	}

	// Create an HTTP client for your API.
	client := &http.Client{}

	// Create the MCP server from the OpenAPI spec.
	// Every operation in the spec becomes an MCP tool.
	srv, err := mcpopenapi.FromOpenAPI(
		spec,
		client,
		"My API Server",
		mcpopenapi.WithBaseURL("https://api.example.com"),
		mcpopenapi.WithBasicAuth("myuser", "mypassword"),
	)
	if err != nil {
		log.Fatalf("create server: %v", err)
	}

	// Run with stdio transport — compatible with Claude Desktop and any
	// MCP client that speaks JSON-RPC over stdin/stdout.
	if err := srv.Run(context.Background()); err != nil {
		log.Fatalf("run: %v", err)
	}
}
