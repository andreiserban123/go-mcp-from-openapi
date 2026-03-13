package mcpopenapi

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
)

// route is a parsed OpenAPI operation ready to become an MCP tool.
type route struct {
	path        string
	method      string
	name        string
	summary     string
	description string
	paramMap    map[string]paramLocation
	properties  map[string]any
	required    []string
	baseURL     string
}

// paramLocation tells the request builder where to put an argument.
type paramLocation struct {
	location string // path | query | header | body
	apiName  string // original name in the OpenAPI spec
}

// parseRoutes loads an OpenAPI 3.x document via kin-openapi and converts
// every operation into a route. The second return value is the first server
// URL found in the spec (empty string if none).
func parseRoutes(spec map[string]any) ([]*route, string, error) {
	data, err := json.Marshal(spec)
	if err != nil {
		return nil, "", fmt.Errorf("marshal spec: %w", err)
	}

	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromData(data)
	if err != nil {
		return nil, "", fmt.Errorf("load spec: %w", err)
	}

	baseURL := ""
	if len(doc.Servers) > 0 {
		baseURL = strings.TrimRight(doc.Servers[0].URL, "/")
	}

	if doc.Paths == nil {
		return nil, baseURL, nil
	}

	var routes []*route
	for path, pathItem := range doc.Paths.Map() {
		// Stable method order.
		ops := []struct {
			method string
			op     *openapi3.Operation
		}{
			{"GET", pathItem.Get},
			{"POST", pathItem.Post},
			{"PUT", pathItem.Put},
			{"PATCH", pathItem.Patch},
			{"DELETE", pathItem.Delete},
			{"HEAD", pathItem.Head},
			{"OPTIONS", pathItem.Options},
		}
		for _, entry := range ops {
			if entry.op == nil {
				continue
			}
			r := buildRoute(path, entry.method, entry.op, pathItem)
			routes = append(routes, r)
		}
	}

	return routes, baseURL, nil
}

func buildRoute(path, method string, op *openapi3.Operation, pathItem *openapi3.PathItem) *route {
	r := &route{
		path:   path,
		method: method,
	}

	r.summary = op.Summary
	r.description = op.Description
	if r.description == "" {
		r.description = r.summary
	}
	r.name = toolName(op.OperationID, method, path)

	// Merge path-level parameters with operation-level ones.
	// Operation-level takes precedence on name+in collision.
	params := mergeOAPIParams(pathItem.Parameters, op.Parameters)

	properties := map[string]any{}
	var required []string
	paramMap := map[string]paramLocation{}

	for _, pRef := range params {
		p := pRef.Value
		if p == nil || p.In == "cookie" {
			continue
		}
		flat := toToolName(p.Name)
		paramMap[flat] = paramLocation{location: p.In, apiName: p.Name}

		prop := schemaRefToMap(p.Schema)
		if p.Description != "" {
			prop["description"] = p.Description
		}
		properties[flat] = prop
		if p.Required {
			required = append(required, flat)
		}
	}

	// Request body.
	if op.RequestBody != nil && op.RequestBody.Value != nil {
		rb := op.RequestBody.Value

		// Prefer application/json, fallback to first available content type.
		var mediaType *openapi3.MediaType
		for _, ct := range []string{"application/json", "application/x-www-form-urlencoded"} {
			if mt, ok := rb.Content[ct]; ok {
				mediaType = mt
				break
			}
		}
		if mediaType == nil {
			for _, mt := range rb.Content {
				mediaType = mt
				break
			}
		}

		if mediaType != nil && mediaType.Schema != nil && mediaType.Schema.Value != nil {
			schema := mediaType.Schema.Value

			// Flatten allOf/anyOf/oneOf into a single property map.
			bodyProps, bodyRequired := flattenSchemaProps(schema)

			for name, propRef := range bodyProps {
				flat := toToolName(name)
				if _, exists := paramMap[flat]; exists {
					flat = "body_" + flat
				}
				paramMap[flat] = paramLocation{location: "body", apiName: name}
				properties[flat] = schemaRefToMap(propRef)
			}
			for _, name := range bodyRequired {
				flat := toToolName(name)
				if _, exists := paramMap["body_"+flat]; exists {
					flat = "body_" + flat
				}
				required = append(required, flat)
			}
		}
	}

	r.paramMap = paramMap
	r.properties = properties
	r.required = required
	return r
}

// flattenSchemaProps collects all properties and required fields from a schema,
// merging allOf / anyOf / oneOf sub-schemas recursively.
func flattenSchemaProps(s *openapi3.Schema) (map[string]*openapi3.SchemaRef, []string) {
	props := map[string]*openapi3.SchemaRef{}
	var required []string

	for name, ref := range s.Properties {
		props[name] = ref
	}
	required = append(required, s.Required...)

	for _, group := range [][]*openapi3.SchemaRef{s.AllOf, s.AnyOf, s.OneOf} {
		for _, ref := range group {
			if ref.Value == nil {
				continue
			}
			subProps, subReq := flattenSchemaProps(ref.Value)
			for k, v := range subProps {
				props[k] = v
			}
			required = append(required, subReq...)
		}
	}

	return props, required
}

// mergeOAPIParams merges path-level parameters with operation-level ones.
// Operation-level params take precedence on name+in collision.
func mergeOAPIParams(pathLevel, opLevel openapi3.Parameters) openapi3.Parameters {
	result := make(openapi3.Parameters, len(pathLevel))
	copy(result, pathLevel)
	for _, op := range opLevel {
		found := false
		for i, pl := range result {
			if pl.Value != nil && op.Value != nil &&
				pl.Value.Name == op.Value.Name && pl.Value.In == op.Value.In {
				result[i] = op
				found = true
				break
			}
		}
		if !found {
			result = append(result, op)
		}
	}
	return result
}

// schemaRefToMap converts a resolved *openapi3.SchemaRef to map[string]any
// for use in MCP tool InputSchema. Marshals the resolved Value (not the $ref
// string) so that all schema details survive.
func schemaRefToMap(ref *openapi3.SchemaRef) map[string]any {
	if ref == nil || ref.Value == nil {
		return map[string]any{"type": "string"}
	}
	data, err := json.Marshal(ref.Value)
	if err != nil {
		return map[string]any{"type": "string"}
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return map[string]any{"type": "string"}
	}
	return m
}

// toolName generates a valid MCP tool name from operationId or method+path.
// MCP tool names may only contain: a-z A-Z 0-9 _ - .
func toolName(operationID, method, path string) string {
	if operationID != "" {
		name := toToolName(operationID)
		if len(name) > 128 {
			name = name[:128]
		}
		return name
	}
	parts := []string{strings.ToLower(method)}
	for _, seg := range strings.Split(path, "/") {
		seg = strings.Trim(seg, "{}")
		seg = toToolName(seg)
		if seg != "" {
			parts = append(parts, seg)
		}
	}
	name := strings.Join(parts, "_")
	if len(name) > 128 {
		name = name[:128]
	}
	return name
}

var invalidCharsRe = regexp.MustCompile(`[^a-zA-Z0-9_\-.]`)

// toToolName converts an arbitrary string to a valid MCP tool/property name.
func toToolName(s string) string {
	s = strings.ReplaceAll(s, " ", "_")
	s = invalidCharsRe.ReplaceAllString(s, "_")
	s = strings.Trim(s, "_")
	return s
}
