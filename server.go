// Package mcpopenapi converts an OpenAPI specification into a running MCP server.
//
// Each OpenAPI operation becomes an MCP tool.  When an LLM calls a tool the
// server translates the arguments back into an HTTP request, sends it to the
// upstream API, and returns the response as tool content.
//
// Minimal usage (mirrors the FastMCP Python example):
//
//	spec, _ := mcpopenapi.FetchSpec("https://api.example.com/openapi.json")
//	client := &http.Client{}
//	srv, _ := mcpopenapi.FromOpenAPI(spec, client, "My API Server",
//	    mcpopenapi.WithBaseURL("https://api.example.com"),
//	)
//	srv.Run(context.Background()) // stdio transport
package mcpopenapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// basicAuthTransport injects HTTP Basic Auth into every request.
type basicAuthTransport struct {
	username, password string
	base               http.RoundTripper
}

func (t *basicAuthTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	r := req.Clone(req.Context())
	r.SetBasicAuth(t.username, t.password)
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}
	return base.RoundTrip(r)
}

// bearerTransport injects a Bearer token into every request.
type bearerTransport struct {
	token string
	base  http.RoundTripper
}

func (t *bearerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	r := req.Clone(req.Context())
	r.Header.Set("Authorization", "Bearer "+t.token)
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}
	return base.RoundTrip(r)
}

// Server is an MCP server built from an OpenAPI specification.
type Server struct {
	s       *mcp.Server
	baseURL string
}

// Option configures the server.
type Option func(*config)

type config struct {
	baseURL   string
	version   string
	transport http.RoundTripper
}

// WithBaseURL overrides the base URL used for upstream API requests.
// When not set, the URL is taken from the first entry in the spec's
// "servers" array.
func WithBaseURL(baseURL string) Option {
	return func(c *config) { c.baseURL = baseURL }
}

// WithBasicAuth wraps the HTTP client with Basic Auth credentials.
// Equivalent to setting an Authorization header on every request.
func WithBasicAuth(username, password string) Option {
	return func(c *config) {
		c.transport = &basicAuthTransport{username: username, password: password, base: c.transport}
	}
}

// WithBearerToken wraps the HTTP client with a Bearer token.
func WithBearerToken(token string) Option {
	return func(c *config) {
		c.transport = &bearerTransport{token: token, base: c.transport}
	}
}

// WithVersion sets the version string advertised in the MCP implementation
// block (default: "1.0.0").
func WithVersion(v string) Option {
	return func(c *config) { c.version = v }
}

// FromOpenAPI creates an MCP server from a decoded OpenAPI 3.x specification.
// Every operation in the spec is registered as an MCP tool.
//
// If client is nil, http.DefaultClient is used.
func FromOpenAPI(spec map[string]any, client *http.Client, name string, opts ...Option) (*Server, error) {
	cfg := &config{version: "1.0.0"}
	for _, o := range opts {
		o(cfg)
	}

	if client == nil {
		client = &http.Client{}
	}
	if cfg.transport != nil {
		// Wrap the existing transport (or DefaultTransport if none set).
		if t, ok := cfg.transport.(*basicAuthTransport); ok {
			t.base = client.Transport
		} else if t, ok := cfg.transport.(*bearerTransport); ok {
			t.base = client.Transport
		}
		client = &http.Client{
			Transport:     cfg.transport,
			CheckRedirect: client.CheckRedirect,
			Jar:           client.Jar,
			Timeout:       client.Timeout,
		}
	}

	routes, specBaseURL, err := parseRoutes(spec)
	if err != nil {
		return nil, fmt.Errorf("parse openapi: %w", err)
	}

	baseURL := cfg.baseURL
	if baseURL == "" {
		baseURL = specBaseURL
	}

	s := mcp.NewServer(&mcp.Implementation{
		Name:    name,
		Version: cfg.version,
	}, nil)

	for _, r := range routes {
		r.baseURL = baseURL
		registerTool(s, r, client)
	}

	return &Server{s: s, baseURL: baseURL}, nil
}

// registerTool adds a single route as an MCP tool on the server.
func registerTool(s *mcp.Server, r *route, client *http.Client) {
	inputSchema := map[string]any{
		"type":       "object",
		"properties": r.properties,
	}
	if len(r.required) > 0 {
		inputSchema["required"] = r.required
	}

	tool := &mcp.Tool{
		Name:        r.name,
		Description: r.description,
		InputSchema: inputSchema,
	}

	s.AddTool(tool, makeHandler(r, client))
}

// Run starts the server using the stdio transport (stdin/stdout JSON-RPC).
// This is the standard transport for use with MCP clients such as Claude Desktop.
func (s *Server) Run(ctx context.Context) error {
	return s.s.Run(ctx, &mcp.StdioTransport{})
}

// RunHTTP starts the server using the Streamable HTTP transport on addr
// (e.g. ":8080").
func (s *Server) RunHTTP(ctx context.Context, addr string) error {
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server {
		return s.s
	}, nil)
	srv := &http.Server{Addr: addr, Handler: handler}

	go func() {
		<-ctx.Done()
		_ = srv.Shutdown(context.Background())
	}()

	return srv.ListenAndServe()
}

// FetchSpec is a convenience helper that downloads and decodes an OpenAPI JSON
// specification from the given URL.
func FetchSpec(specURL string) (map[string]any, error) {
	resp, err := http.Get(specURL) //nolint:gosec // URL is caller-supplied
	if err != nil {
		return nil, fmt.Errorf("fetch spec: %w", err)
	}
	defer resp.Body.Close()

	var spec map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&spec); err != nil {
		return nil, fmt.Errorf("decode spec: %w", err)
	}
	return spec, nil
}
