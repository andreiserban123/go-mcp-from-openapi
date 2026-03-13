package mcpopenapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// petstore is a minimal OpenAPI 3 spec used across multiple tests.
var petstore = map[string]any{
	"openapi": "3.0.0",
	"info":    map[string]any{"title": "Petstore", "version": "0.1.0"},
	"servers": []any{map[string]any{"url": "https://petstore.example.com"}},
	"paths": map[string]any{
		"/pets": map[string]any{
			"get": map[string]any{
				"operationId": "listPets",
				"summary":     "List all pets",
				"parameters": []any{
					map[string]any{
						"name": "limit", "in": "query", "required": false,
						"schema": map[string]any{"type": "integer"},
					},
				},
				"responses": map[string]any{"200": map[string]any{"description": "ok"}},
			},
			"post": map[string]any{
				"operationId": "createPet",
				"summary":     "Create a pet",
				"requestBody": map[string]any{
					"required": true,
					"content": map[string]any{
						"application/json": map[string]any{
							"schema": map[string]any{
								"type": "object",
								"properties": map[string]any{
									"name": map[string]any{"type": "string"},
									"age":  map[string]any{"type": "integer"},
								},
								"required": []any{"name"},
							},
						},
					},
				},
				"responses": map[string]any{"201": map[string]any{"description": "created"}},
			},
		},
		"/pets/{petId}": map[string]any{
			"get": map[string]any{
				"operationId": "getPet",
				"summary":     "Get a pet",
				"parameters": []any{
					map[string]any{
						"name": "petId", "in": "path", "required": true,
						"schema": map[string]any{"type": "string"},
					},
					map[string]any{
						"name": "X-Trace-Id", "in": "header", "required": false,
						"schema": map[string]any{"type": "string"},
					},
				},
				"responses": map[string]any{"200": map[string]any{"description": "ok"}},
			},
		},
	},
}

// ---------------------------------------------------------------------------
// parseRoutes
// ---------------------------------------------------------------------------

func TestParseRoutes_Count(t *testing.T) {
	routes, _, err := parseRoutes(petstore)
	if err != nil {
		t.Fatalf("parseRoutes: %v", err)
	}
	if len(routes) != 3 {
		t.Fatalf("expected 3 routes, got %d", len(routes))
	}
}

func TestParseRoutes_BaseURL(t *testing.T) {
	_, baseURL, err := parseRoutes(petstore)
	if err != nil {
		t.Fatalf("parseRoutes: %v", err)
	}
	if baseURL != "https://petstore.example.com" {
		t.Errorf("expected base URL %q, got %q", "https://petstore.example.com", baseURL)
	}
}

func TestParseRoutes_MissingPaths(t *testing.T) {
	spec := map[string]any{
		"openapi": "3.0.0",
		"info":    map[string]any{"title": "Empty", "version": "1.0"},
	}
	routes, _, err := parseRoutes(spec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(routes) != 0 {
		t.Errorf("expected 0 routes, got %d", len(routes))
	}
}

func TestParseRoutes_OperationID(t *testing.T) {
	routes, _, _ := parseRoutes(petstore)
	names := make(map[string]bool)
	for _, r := range routes {
		names[r.name] = true
	}
	for _, want := range []string{"listPets", "createPet", "getPet"} {
		if !names[want] {
			t.Errorf("expected tool %q, not found in %v", want, names)
		}
	}
}

func TestParseRoutes_NoOperationID(t *testing.T) {
	spec := map[string]any{
		"openapi": "3.0.0",
		"info":    map[string]any{"title": "T", "version": "1"},
		"paths": map[string]any{
			"/users/{id}": map[string]any{
				"delete": map[string]any{
					"summary":   "Delete user",
					"responses": map[string]any{"204": map[string]any{"description": "ok"}},
				},
			},
		},
	}
	routes, _, err := parseRoutes(spec)
	if err != nil {
		t.Fatalf("parseRoutes: %v", err)
	}
	if len(routes) != 1 {
		t.Fatalf("expected 1 route, got %d", len(routes))
	}
	if routes[0].name != "delete_users_id" {
		t.Errorf("unexpected name %q", routes[0].name)
	}
}

// ---------------------------------------------------------------------------
// Parameter mapping
// ---------------------------------------------------------------------------

func TestParseRoutes_QueryParam(t *testing.T) {
	routes, _, _ := parseRoutes(petstore)
	var listPets *route
	for _, r := range routes {
		if r.name == "listPets" {
			listPets = r
		}
	}
	if listPets == nil {
		t.Fatal("listPets route not found")
	}
	loc, ok := listPets.paramMap["limit"]
	if !ok {
		t.Fatal("expected 'limit' in paramMap")
	}
	if loc.location != "query" {
		t.Errorf("expected query, got %q", loc.location)
	}
}

func TestParseRoutes_PathParam(t *testing.T) {
	routes, _, _ := parseRoutes(petstore)
	var getPet *route
	for _, r := range routes {
		if r.name == "getPet" {
			getPet = r
		}
	}
	if getPet == nil {
		t.Fatal("getPet route not found")
	}
	loc, ok := getPet.paramMap["petId"]
	if !ok {
		t.Fatal("expected 'petId' in paramMap")
	}
	if loc.location != "path" {
		t.Errorf("expected path, got %q", loc.location)
	}
}

func TestParseRoutes_HeaderParam(t *testing.T) {
	routes, _, _ := parseRoutes(petstore)
	var getPet *route
	for _, r := range routes {
		if r.name == "getPet" {
			getPet = r
		}
	}
	if getPet == nil {
		t.Fatal("getPet route not found")
	}
	loc, ok := getPet.paramMap["X-Trace-Id"]
	if !ok {
		t.Fatal("expected 'X-Trace-Id' in paramMap")
	}
	if loc.location != "header" {
		t.Errorf("expected header, got %q", loc.location)
	}
	if loc.apiName != "X-Trace-Id" {
		t.Errorf("expected apiName 'X-Trace-Id', got %q", loc.apiName)
	}
}

func TestParseRoutes_BodyParams(t *testing.T) {
	routes, _, _ := parseRoutes(petstore)
	var createPet *route
	for _, r := range routes {
		if r.name == "createPet" {
			createPet = r
		}
	}
	if createPet == nil {
		t.Fatal("createPet route not found")
	}
	for _, field := range []string{"name", "age"} {
		loc, ok := createPet.paramMap[field]
		if !ok {
			t.Errorf("expected body field %q in paramMap", field)
			continue
		}
		if loc.location != "body" {
			t.Errorf("field %q: expected body, got %q", field, loc.location)
		}
	}
	// 'name' should be required
	found := false
	for _, req := range createPet.required {
		if req == "name" {
			found = true
		}
	}
	if !found {
		t.Error("expected 'name' in required list")
	}
}

// ---------------------------------------------------------------------------
// toolName / toToolName
// ---------------------------------------------------------------------------

func TestToolName_UsesOperationID(t *testing.T) {
	got := toolName("listUsers", "GET", "/users")
	if got != "listUsers" {
		t.Errorf("got %q", got)
	}
}

func TestToolName_FallsBackToMethodAndPath(t *testing.T) {
	got := toolName("", "POST", "/api/v1/users")
	if got != "post_api_v1_users" {
		t.Errorf("got %q", got)
	}
}

func TestToolName_StripsCurlyBraces(t *testing.T) {
	got := toolName("", "DELETE", "/users/{id}/posts/{postId}")
	if got != "delete_users_id_posts_postId" {
		t.Errorf("got %q", got)
	}
}

func TestToolName_TruncatesAt128(t *testing.T) {
	long := strings.Repeat("a", 200)
	got := toolName(long, "GET", "/x")
	if len(got) > 128 {
		t.Errorf("name too long: %d chars", len(got))
	}
}

func TestToToolName_ReplacesSpaces(t *testing.T) {
	if got := toToolName("hello world"); got != "hello_world" {
		t.Errorf("got %q", got)
	}
}

func TestToToolName_ReplacesInvalidChars(t *testing.T) {
	if got := toToolName("foo/bar:baz"); got != "foo_bar_baz" {
		t.Errorf("got %q", got)
	}
}

func TestToToolName_PreservesValidChars(t *testing.T) {
	if got := toToolName("foo-bar.baz_123"); got != "foo-bar.baz_123" {
		t.Errorf("got %q", got)
	}
}

// ---------------------------------------------------------------------------
// schemaRefToMap
// ---------------------------------------------------------------------------

func TestSchemaRefToMap_NilReturnsString(t *testing.T) {
	m := schemaRefToMap(nil)
	if m["type"] != "string" {
		t.Errorf("expected type=string, got %v", m)
	}
}

func TestSchemaRefToMap_IntegerType(t *testing.T) {
	routes, _, _ := parseRoutes(petstore)
	var listPets *route
	for _, r := range routes {
		if r.name == "listPets" {
			listPets = r
		}
	}
	prop, ok := listPets.properties["limit"].(map[string]any)
	if !ok {
		t.Fatal("limit property missing or wrong type")
	}
	if prop["type"] != "integer" {
		t.Errorf("expected integer, got %v", prop["type"])
	}
}

// ---------------------------------------------------------------------------
// allOf / anyOf / oneOf body flattening
// ---------------------------------------------------------------------------

func TestParseRoutes_AllOfBody(t *testing.T) {
	spec := map[string]any{
		"openapi": "3.0.0",
		"info":    map[string]any{"title": "T", "version": "1"},
		"paths": map[string]any{
			"/items": map[string]any{
				"post": map[string]any{
					"operationId": "createItem",
					"requestBody": map[string]any{
						"required": true,
						"content": map[string]any{
							"application/json": map[string]any{
								"schema": map[string]any{
									"allOf": []any{
										map[string]any{
											"type": "object",
											"properties": map[string]any{
												"foo": map[string]any{"type": "string"},
											},
										},
										map[string]any{
											"type": "object",
											"properties": map[string]any{
												"bar": map[string]any{"type": "integer"},
											},
										},
									},
								},
							},
						},
					},
					"responses": map[string]any{"201": map[string]any{"description": "ok"}},
				},
			},
		},
	}
	routes, _, err := parseRoutes(spec)
	if err != nil {
		t.Fatalf("parseRoutes: %v", err)
	}
	if len(routes) != 1 {
		t.Fatalf("expected 1 route, got %d", len(routes))
	}
	r := routes[0]
	if _, ok := r.paramMap["foo"]; !ok {
		t.Error("expected 'foo' from allOf")
	}
	if _, ok := r.paramMap["bar"]; !ok {
		t.Error("expected 'bar' from allOf")
	}
}

// ---------------------------------------------------------------------------
// Path-level parameter inheritance
// ---------------------------------------------------------------------------

func TestParseRoutes_PathLevelParams(t *testing.T) {
	spec := map[string]any{
		"openapi": "3.0.0",
		"info":    map[string]any{"title": "T", "version": "1"},
		"paths": map[string]any{
			"/resources/{id}": map[string]any{
				// path-level parameter
				"parameters": []any{
					map[string]any{
						"name": "id", "in": "path", "required": true,
						"schema": map[string]any{"type": "string"},
					},
				},
				"get": map[string]any{
					"operationId": "getResource",
					"responses":   map[string]any{"200": map[string]any{"description": "ok"}},
				},
			},
		},
	}
	routes, _, err := parseRoutes(spec)
	if err != nil {
		t.Fatalf("parseRoutes: %v", err)
	}
	if len(routes) != 1 {
		t.Fatalf("expected 1 route")
	}
	loc, ok := routes[0].paramMap["id"]
	if !ok {
		t.Fatal("path-level param 'id' not inherited")
	}
	if loc.location != "path" {
		t.Errorf("expected path, got %q", loc.location)
	}
}

// ---------------------------------------------------------------------------
// buildHTTPRequest
// ---------------------------------------------------------------------------

func TestBuildHTTPRequest_PathSubstitution(t *testing.T) {
	r := &route{
		path:    "/pets/{petId}",
		method:  "GET",
		baseURL: "https://example.com",
		paramMap: map[string]paramLocation{
			"petId": {location: "path", apiName: "petId"},
		},
	}
	req, err := buildHTTPRequest(context.Background(), r, map[string]any{"petId": "42"})
	if err != nil {
		t.Fatalf("buildHTTPRequest: %v", err)
	}
	if req.URL.Path != "/pets/42" {
		t.Errorf("expected /pets/42, got %q", req.URL.Path)
	}
}

func TestBuildHTTPRequest_QueryParam(t *testing.T) {
	r := &route{
		path:    "/pets",
		method:  "GET",
		baseURL: "https://example.com",
		paramMap: map[string]paramLocation{
			"limit": {location: "query", apiName: "limit"},
		},
	}
	req, err := buildHTTPRequest(context.Background(), r, map[string]any{"limit": "10"})
	if err != nil {
		t.Fatalf("buildHTTPRequest: %v", err)
	}
	if req.URL.Query().Get("limit") != "10" {
		t.Errorf("expected limit=10, got %q", req.URL.RawQuery)
	}
}

func TestBuildHTTPRequest_HeaderParam(t *testing.T) {
	r := &route{
		path:    "/pets",
		method:  "GET",
		baseURL: "https://example.com",
		paramMap: map[string]paramLocation{
			"X_Trace_Id": {location: "header", apiName: "X-Trace-Id"},
		},
	}
	req, err := buildHTTPRequest(context.Background(), r, map[string]any{"X_Trace_Id": "abc123"})
	if err != nil {
		t.Fatalf("buildHTTPRequest: %v", err)
	}
	if req.Header.Get("X-Trace-Id") != "abc123" {
		t.Errorf("expected X-Trace-Id: abc123, got %q", req.Header.Get("X-Trace-Id"))
	}
}

func TestBuildHTTPRequest_JSONBody(t *testing.T) {
	r := &route{
		path:    "/pets",
		method:  "POST",
		baseURL: "https://example.com",
		paramMap: map[string]paramLocation{
			"name": {location: "body", apiName: "name"},
			"age":  {location: "body", apiName: "age"},
		},
	}
	req, err := buildHTTPRequest(context.Background(), r, map[string]any{"name": "Fido", "age": 3})
	if err != nil {
		t.Fatalf("buildHTTPRequest: %v", err)
	}
	if req.Header.Get("Content-Type") != "application/json" {
		t.Errorf("expected Content-Type application/json, got %q", req.Header.Get("Content-Type"))
	}
	body, _ := io.ReadAll(req.Body)
	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	if got["name"] != "Fido" {
		t.Errorf("expected name=Fido, got %v", got["name"])
	}
}

func TestBuildHTTPRequest_NoBodyNoContentType(t *testing.T) {
	r := &route{
		path:     "/ping",
		method:   "GET",
		baseURL:  "https://example.com",
		paramMap: map[string]paramLocation{},
	}
	req, err := buildHTTPRequest(context.Background(), r, map[string]any{})
	if err != nil {
		t.Fatalf("buildHTTPRequest: %v", err)
	}
	if req.Header.Get("Content-Type") != "" {
		t.Errorf("expected no Content-Type, got %q", req.Header.Get("Content-Type"))
	}
}

// ---------------------------------------------------------------------------
// Auth transports
// ---------------------------------------------------------------------------

func TestBasicAuthTransport(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok || user != "alice" || pass != "secret" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := &http.Client{Transport: &basicAuthTransport{username: "alice", password: "secret"}}
	resp, err := client.Get(srv.URL)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

func TestBearerTransport(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer mytoken" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := &http.Client{Transport: &bearerTransport{token: "mytoken"}}
	resp, err := client.Get(srv.URL)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// FetchSpec
// ---------------------------------------------------------------------------

func TestFetchSpec(t *testing.T) {
	spec := map[string]any{"openapi": "3.0.0", "info": map[string]any{"title": "T", "version": "1"}}
	data, _ := json.Marshal(spec)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(data)
	}))
	defer srv.Close()

	got, err := FetchSpec(srv.URL + "/openapi.json")
	if err != nil {
		t.Fatalf("FetchSpec: %v", err)
	}
	if got["openapi"] != "3.0.0" {
		t.Errorf("expected openapi=3.0.0, got %v", got["openapi"])
	}
}

// ---------------------------------------------------------------------------
// FromOpenAPI integration — end-to-end tool registration
// ---------------------------------------------------------------------------

func TestFromOpenAPI_RegistersTools(t *testing.T) {
	srv, err := FromOpenAPI(petstore, nil, "Petstore")
	if err != nil {
		t.Fatalf("FromOpenAPI: %v", err)
	}
	if srv == nil {
		t.Fatal("expected non-nil server")
	}
}

func TestFromOpenAPI_WithBaseURL(t *testing.T) {
	srv, err := FromOpenAPI(petstore, nil, "Petstore",
		WithBaseURL("https://override.example.com"),
	)
	if err != nil {
		t.Fatalf("FromOpenAPI: %v", err)
	}
	if srv.baseURL != "https://override.example.com" {
		t.Errorf("expected override URL, got %q", srv.baseURL)
	}
}

func TestFromOpenAPI_UsesSpecBaseURLWhenNotOverridden(t *testing.T) {
	srv, err := FromOpenAPI(petstore, nil, "Petstore")
	if err != nil {
		t.Fatalf("FromOpenAPI: %v", err)
	}
	if srv.baseURL != "https://petstore.example.com" {
		t.Errorf("expected spec base URL, got %q", srv.baseURL)
	}
}

// ---------------------------------------------------------------------------
// ParseSpecJSON / FromOpenAPIJSON
// ---------------------------------------------------------------------------

func TestParseSpecJSON_ValidJSON(t *testing.T) {
	raw := `{"openapi":"3.0.0","info":{"title":"T","version":"1"},"paths":{}}`
	spec, err := ParseSpecJSON([]byte(raw))
	if err != nil {
		t.Fatalf("ParseSpecJSON: %v", err)
	}
	if spec["openapi"] != "3.0.0" {
		t.Errorf("expected openapi=3.0.0, got %v", spec["openapi"])
	}
}

func TestParseSpecJSON_InvalidJSON(t *testing.T) {
	_, err := ParseSpecJSON([]byte(`not json`))
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestParseSpecJSON_EmptyInput(t *testing.T) {
	_, err := ParseSpecJSON([]byte(``))
	if err == nil {
		t.Error("expected error for empty input")
	}
}

func TestFromOpenAPIJSON_ParsesAndCreatesServer(t *testing.T) {
	raw, _ := json.Marshal(petstore)
	srv, err := FromOpenAPIJSON(raw, nil, "Petstore")
	if err != nil {
		t.Fatalf("FromOpenAPIJSON: %v", err)
	}
	if srv == nil {
		t.Fatal("expected non-nil server")
	}
}

func TestFromOpenAPIJSON_StringCast(t *testing.T) {
	// Real-world pattern: you have a string, cast to []byte.
	jsonStr := `{
		"openapi": "3.0.0",
		"info": {"title": "My API", "version": "2.0.0"},
		"servers": [{"url": "https://api.example.com"}],
		"paths": {
			"/hello": {
				"get": {
					"operationId": "sayHello",
					"responses": {"200": {"description": "ok"}}
				}
			}
		}
	}`
	srv, err := FromOpenAPIJSON([]byte(jsonStr), nil, "My API",
		WithBaseURL("https://api.example.com"),
	)
	if err != nil {
		t.Fatalf("FromOpenAPIJSON: %v", err)
	}
	if srv.baseURL != "https://api.example.com" {
		t.Errorf("unexpected baseURL: %q", srv.baseURL)
	}
}

func TestFromOpenAPIJSON_InvalidJSON(t *testing.T) {
	_, err := FromOpenAPIJSON([]byte(`not json`), nil, "Bad")
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestFromOpenAPI_ToolCallForwardsToAPI(t *testing.T) {
	// Start a real HTTP server that acts as the upstream API.
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "GET" && r.URL.Path == "/pets":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode([]map[string]any{{"id": 1, "name": "Fido"}})
		case r.Method == "GET" && strings.HasPrefix(r.URL.Path, "/pets/"):
			id := strings.TrimPrefix(r.URL.Path, "/pets/")
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{"id": id, "name": "Fido"})
		case r.Method == "POST" && r.URL.Path == "/pets":
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(body)
		default:
			http.NotFound(w, r)
		}
	}))
	defer api.Close()

	spec := map[string]any{
		"openapi": "3.0.0",
		"info":    map[string]any{"title": "T", "version": "1"},
		"servers": []any{map[string]any{"url": api.URL}},
		"paths": map[string]any{
			"/pets": map[string]any{
				"get": map[string]any{
					"operationId": "listPets",
					"responses":   map[string]any{"200": map[string]any{"description": "ok"}},
				},
			},
			"/pets/{petId}": map[string]any{
				"get": map[string]any{
					"operationId": "getPet",
					"parameters": []any{
						map[string]any{
							"name": "petId", "in": "path", "required": true,
							"schema": map[string]any{"type": "string"},
						},
					},
					"responses": map[string]any{"200": map[string]any{"description": "ok"}},
				},
			},
		},
	}

	mcpSrv, err := FromOpenAPI(spec, nil, "Test API")
	if err != nil {
		t.Fatalf("FromOpenAPI: %v", err)
	}

	// Verify the routes are wired correctly by directly invoking buildHTTPRequest
	// and making the real call through a plain http.Client.
	var listRoute *route
	var getRoute *route
	routes, _, _ := parseRoutes(spec)
	for _, r := range routes {
		r.baseURL = api.URL
		switch r.name {
		case "listPets":
			listRoute = r
		case "getPet":
			getRoute = r
		}
	}
	_ = mcpSrv // server is built; we test the lower-level request building here

	// listPets
	req, err := buildHTTPRequest(context.Background(), listRoute, map[string]any{})
	if err != nil {
		t.Fatalf("buildHTTPRequest listPets: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do listPets: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("listPets: expected 200, got %d", resp.StatusCode)
	}

	// getPet with path param
	req, err = buildHTTPRequest(context.Background(), getRoute, map[string]any{"petId": "99"})
	if err != nil {
		t.Fatalf("buildHTTPRequest getPet: %v", err)
	}
	if !strings.HasSuffix(req.URL.Path, "/99") {
		t.Errorf("expected path ending in /99, got %q", req.URL.Path)
	}
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do getPet: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("getPet: expected 200, got %d", resp.StatusCode)
	}
}
