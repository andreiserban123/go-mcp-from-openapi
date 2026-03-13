package mcpopenapi

// Integration test against the public typicode demo server.
// Run with: go test -v -run TestTypicode ./...

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

// typicodeSpec is a hand-written OpenAPI 3 spec for
// https://my-json-server.typicode.com/typicode/demo
var typicodeSpec = map[string]any{
	"openapi": "3.0.0",
	"info":    map[string]any{"title": "Typicode Demo", "version": "1.0.0"},
	"servers": []any{map[string]any{"url": "https://my-json-server.typicode.com/typicode/demo"}},
	"paths": map[string]any{
		"/comments": map[string]any{
			"get": map[string]any{
				"operationId": "listComments",
				"summary":     "List all comments",
				"parameters": []any{
					map[string]any{
						"name": "postId", "in": "query", "required": false,
						"description": "Filter comments by post ID",
						"schema":      map[string]any{"type": "integer"},
					},
				},
				"responses": map[string]any{
					"200": map[string]any{"description": "A list of comments"},
				},
			},
			"post": map[string]any{
				"operationId": "createComment",
				"summary":     "Create a comment",
				"requestBody": map[string]any{
					"required": true,
					"content": map[string]any{
						"application/json": map[string]any{
							"schema": map[string]any{
								"type": "object",
								"properties": map[string]any{
									"body":   map[string]any{"type": "string", "description": "Comment text"},
									"postId": map[string]any{"type": "integer", "description": "ID of the post"},
								},
								"required": []any{"body", "postId"},
							},
						},
					},
				},
				"responses": map[string]any{
					"201": map[string]any{"description": "Created"},
				},
			},
		},
		"/comments/{id}": map[string]any{
			"get": map[string]any{
				"operationId": "getComment",
				"summary":     "Get a comment by ID",
				"parameters": []any{
					map[string]any{
						"name": "id", "in": "path", "required": true,
						"description": "Comment ID",
						"schema":      map[string]any{"type": "integer"},
					},
				},
				"responses": map[string]any{
					"200": map[string]any{"description": "A comment"},
					"404": map[string]any{"description": "Not found"},
				},
			},
		},
		"/posts": map[string]any{
			"get": map[string]any{
				"operationId": "listPosts",
				"summary":     "List all posts",
				"responses":   map[string]any{"200": map[string]any{"description": "A list of posts"}},
			},
		},
	},
}

func TestTypicode_ParsesSpec(t *testing.T) {
	routes, baseURL, err := parseRoutes(typicodeSpec)
	if err != nil {
		t.Fatalf("parseRoutes: %v", err)
	}
	t.Logf("base URL: %s", baseURL)
	t.Logf("registered %d tools:", len(routes))
	for _, r := range routes {
		t.Logf("  %-20s  %s %s", r.name, r.method, r.path)
		for param, loc := range r.paramMap {
			t.Logf("    arg %-15s -> %s (%s)", param, loc.location, loc.apiName)
		}
	}

	wantTools := []string{"listComments", "createComment", "getComment", "listPosts"}
	names := map[string]bool{}
	for _, r := range routes {
		names[r.name] = true
	}
	for _, w := range wantTools {
		if !names[w] {
			t.Errorf("expected tool %q to be registered", w)
		}
	}
}

func TestTypicode_ListComments(t *testing.T) {
	routes, _, err := parseRoutes(typicodeSpec)
	if err != nil {
		t.Fatalf("parseRoutes: %v", err)
	}

	var r *route
	for _, rt := range routes {
		if rt.name == "listComments" {
			r = rt
			r.baseURL = "https://my-json-server.typicode.com/typicode/demo"
			break
		}
	}
	if r == nil {
		t.Fatal("listComments route not found")
	}

	req, err := buildHTTPRequest(context.Background(), r, map[string]any{})
	if err != nil {
		t.Fatalf("buildHTTPRequest: %v", err)
	}

	t.Logf("→ %s %s", req.Method, req.URL)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("http: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	t.Logf("← %d  %s", resp.StatusCode, string(body))

	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	if !strings.Contains(string(body), `"id"`) {
		t.Errorf("expected JSON array in response, got: %s", body)
	}
}

func TestTypicode_GetCommentByID(t *testing.T) {
	routes, _, err := parseRoutes(typicodeSpec)
	if err != nil {
		t.Fatalf("parseRoutes: %v", err)
	}

	var r *route
	for _, rt := range routes {
		if rt.name == "getComment" {
			r = rt
			r.baseURL = "https://my-json-server.typicode.com/typicode/demo"
			break
		}
	}
	if r == nil {
		t.Fatal("getComment route not found")
	}

	req, err := buildHTTPRequest(context.Background(), r, map[string]any{"id": "1"})
	if err != nil {
		t.Fatalf("buildHTTPRequest: %v", err)
	}

	t.Logf("→ %s %s", req.Method, req.URL)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("http: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	t.Logf("← %d  %s", resp.StatusCode, string(body))

	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	if !strings.Contains(string(body), `"id": 1`) && !strings.Contains(string(body), `"id":1`) {
		t.Errorf("expected comment id=1 in response, got: %s", body)
	}
}

func TestTypicode_FilterByPostID(t *testing.T) {
	routes, _, err := parseRoutes(typicodeSpec)
	if err != nil {
		t.Fatalf("parseRoutes: %v", err)
	}

	var r *route
	for _, rt := range routes {
		if rt.name == "listComments" {
			r = rt
			r.baseURL = "https://my-json-server.typicode.com/typicode/demo"
			break
		}
	}
	if r == nil {
		t.Fatal("listComments route not found")
	}

	req, err := buildHTTPRequest(context.Background(), r, map[string]any{"postId": "1"})
	if err != nil {
		t.Fatalf("buildHTTPRequest: %v", err)
	}

	t.Logf("→ %s %s", req.Method, req.URL)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("http: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	t.Logf("← %d  %s", resp.StatusCode, string(body))

	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

func TestTypicode_FromOpenAPIEndToEnd(t *testing.T) {
	srv, err := FromOpenAPI(typicodeSpec, nil, "Typicode Demo")
	if err != nil {
		t.Fatalf("FromOpenAPI: %v", err)
	}
	if srv == nil {
		t.Fatal("expected non-nil server")
	}
	t.Logf("MCP server created: baseURL=%s", srv.baseURL)
}
