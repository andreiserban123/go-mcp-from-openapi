package mcpopenapi

import (
	"fmt"
	"regexp"
	"strings"
)

// route is a parsed OpenAPI operation ready to become an MCP tool.
type route struct {
	path        string
	method      string
	name        string // MCP tool name
	summary     string
	description string
	parameters  []*parameter
	requestBody *requestBody
	paramMap    map[string]paramLocation
	properties  map[string]any // JSON Schema properties (flat, for MCP InputSchema)
	required    []string       // required property names
	baseURL     string
}

type parameter struct {
	name        string
	in          string // path, query, header, cookie
	description string
	required    bool
	schema      map[string]any
}

type requestBody struct {
	description string
	required    bool
	properties  map[string]any
	requiredProps []string
}

// paramLocation tells the request builder where to put an argument.
type paramLocation struct {
	location string // path | query | header | body
	apiName  string // original name in the OpenAPI spec
}

func parseRoutes(spec map[string]any) ([]*route, error) {
	paths, ok := spec["paths"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("spec missing 'paths' field")
	}

	var routes []*route
	// Deterministic method order
	methodOrder := []string{"get", "post", "put", "patch", "delete", "head", "options"}

	for path, pathItemAny := range paths {
		pathItem, ok := pathItemAny.(map[string]any)
		if !ok {
			continue
		}

		// Path-level parameters inherited by all operations under this path
		pathLevelParams := parseParameters(spec, pathItem["parameters"])

		for _, method := range methodOrder {
			opAny, ok := pathItem[method]
			if !ok {
				continue
			}
			op, ok := opAny.(map[string]any)
			if !ok {
				continue
			}
			r := parseOperation(spec, path, strings.ToUpper(method), op, pathLevelParams)
			routes = append(routes, r)
		}
	}

	return routes, nil
}

func parseOperation(spec map[string]any, path, method string, op map[string]any, inherited []*parameter) *route {
	r := &route{
		path:   path,
		method: method,
	}

	r.summary, _ = op["summary"].(string)
	r.description, _ = op["description"].(string)
	if r.description == "" {
		r.description = r.summary
	}

	operationID, _ := op["operationId"].(string)
	r.name = toolName(operationID, method, path)

	opParams := parseParameters(spec, op["parameters"])
	r.parameters = mergeParameters(inherited, opParams)

	if rb, ok := op["requestBody"].(map[string]any); ok {
		rb = resolveRef(spec, rb)
		r.requestBody = parseRequestBody(spec, rb)
	}

	r.paramMap, r.properties, r.required = buildParamMap(r.parameters, r.requestBody)

	return r
}

func parseParameters(spec map[string]any, paramsAny any) []*parameter {
	paramsSlice, ok := paramsAny.([]any)
	if !ok {
		return nil
	}

	var params []*parameter
	for _, pAny := range paramsSlice {
		p, ok := pAny.(map[string]any)
		if !ok {
			continue
		}
		p = resolveRef(spec, p)

		param := &parameter{
			name: getString(p, "name"),
			in:   getString(p, "in"),
		}
		param.required, _ = p["required"].(bool)
		param.description, _ = p["description"].(string)

		if schema, ok := p["schema"].(map[string]any); ok {
			param.schema = resolveRef(spec, schema)
		}
		if param.schema == nil {
			param.schema = map[string]any{"type": "string"}
		}

		if param.name != "" && param.in != "" {
			params = append(params, param)
		}
	}
	return params
}

func parseRequestBody(spec map[string]any, rb map[string]any) *requestBody {
	result := &requestBody{}
	result.description, _ = rb["description"].(string)
	result.required, _ = rb["required"].(bool)

	content, ok := rb["content"].(map[string]any)
	if !ok {
		return result
	}

	// Prefer application/json, then fall back to first available content type
	var mediaType map[string]any
	for _, ct := range []string{"application/json", "application/x-www-form-urlencoded"} {
		if mt, ok := content[ct].(map[string]any); ok {
			mediaType = mt
			break
		}
	}
	if mediaType == nil {
		for _, mt := range content {
			if mtMap, ok := mt.(map[string]any); ok {
				mediaType = mtMap
				break
			}
		}
	}
	if mediaType == nil {
		return result
	}

	schema, ok := mediaType["schema"].(map[string]any)
	if !ok {
		return result
	}
	schema = resolveRef(spec, schema)
	schema = flattenSchema(spec, schema)

	result.properties, _ = schema["properties"].(map[string]any)
	if req, ok := schema["required"].([]any); ok {
		for _, r := range req {
			if s, ok := r.(string); ok {
				result.requiredProps = append(result.requiredProps, s)
			}
		}
	}
	return result
}

func buildParamMap(params []*parameter, rb *requestBody) (map[string]paramLocation, map[string]any, []string) {
	properties := map[string]any{}
	var required []string
	paramMap := map[string]paramLocation{}

	for _, p := range params {
		if p.in == "cookie" {
			continue
		}
		flat := toToolName(p.name)
		paramMap[flat] = paramLocation{location: p.in, apiName: p.name}

		prop := shallowCopy(p.schema)
		if p.description != "" {
			prop["description"] = p.description
		}
		properties[flat] = prop

		if p.required {
			required = append(required, flat)
		}
	}

	if rb != nil {
		for name, schemAny := range rb.properties {
			schema, ok := schemAny.(map[string]any)
			if !ok {
				schema = map[string]any{"type": "string"}
			}
			flat := toToolName(name)
			// Avoid collision with path/query/header params
			if _, exists := paramMap[flat]; exists {
				flat = "body_" + flat
			}
			paramMap[flat] = paramLocation{location: "body", apiName: name}
			properties[flat] = shallowCopy(schema)
		}
		for _, name := range rb.requiredProps {
			flat := toToolName(name)
			if _, exists := paramMap["body_"+flat]; exists {
				flat = "body_" + flat
			}
			required = append(required, flat)
		}
	}

	return paramMap, properties, required
}

// toolName generates a valid MCP tool name from operationId or method+path.
// MCP tool names may only contain: a-z A-Z 0-9 _ - .
func toolName(operationID, method, path string) string {
	if operationID != "" {
		return toToolName(operationID)
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

// extractBaseURL reads the first server URL from the spec.
func extractBaseURL(spec map[string]any) string {
	servers, ok := spec["servers"].([]any)
	if !ok || len(servers) == 0 {
		return ""
	}
	first, ok := servers[0].(map[string]any)
	if !ok {
		return ""
	}
	u, _ := first["url"].(string)
	return strings.TrimRight(u, "/")
}

// resolveRef follows a JSON Pointer $ref within the same document.
func resolveRef(spec, obj map[string]any) map[string]any {
	ref, ok := obj["$ref"].(string)
	if !ok {
		return obj
	}
	if !strings.HasPrefix(ref, "#/") {
		return obj // external $refs not supported
	}
	parts := strings.Split(strings.TrimPrefix(ref, "#/"), "/")
	current := spec
	for _, part := range parts {
		part = strings.ReplaceAll(part, "~1", "/")
		part = strings.ReplaceAll(part, "~0", "~")
		next, ok := current[part].(map[string]any)
		if !ok {
			return obj
		}
		current = next
	}
	return resolveRef(spec, current) // recurse in case target also has $ref
}

// flattenSchema merges allOf sub-schemas into a single object schema.
func flattenSchema(spec map[string]any, schema map[string]any) map[string]any {
	allOf, ok := schema["allOf"].([]any)
	if !ok {
		return schema
	}
	merged := map[string]any{"type": "object", "properties": map[string]any{}}
	var required []string
	for _, sub := range allOf {
		subMap, ok := sub.(map[string]any)
		if !ok {
			continue
		}
		subMap = resolveRef(spec, subMap)
		subMap = flattenSchema(spec, subMap)
		if props, ok := subMap["properties"].(map[string]any); ok {
			for k, v := range props {
				merged["properties"].(map[string]any)[k] = v
			}
		}
		if req, ok := subMap["required"].([]any); ok {
			for _, r := range req {
				if s, ok := r.(string); ok {
					required = append(required, s)
				}
			}
		}
	}
	if len(required) > 0 {
		merged["required"] = required
	}
	return merged
}

// mergeParameters merges path-level params with operation-level ones.
// Operation-level params take precedence on name+in collision.
func mergeParameters(pathLevel, opLevel []*parameter) []*parameter {
	result := make([]*parameter, len(pathLevel))
	copy(result, pathLevel)
	for _, op := range opLevel {
		found := false
		for i, pl := range result {
			if pl.name == op.name && pl.in == op.in {
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

func getString(m map[string]any, key string) string {
	v, _ := m[key].(string)
	return v
}

func shallowCopy(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
