package mcpopenapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/url"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// makeHandler returns an MCP ToolHandler that translates a tool call into
// an HTTP request against the API described by the given route.
func makeHandler(r *route, client *http.Client) mcp.ToolHandler {
	return func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		// Arguments is json.RawMessage; unmarshal into a flat map.
		var args map[string]any
		if len(req.Params.Arguments) > 0 {
			if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
				return toolError(fmt.Sprintf("decode arguments: %v", err)), nil
			}
		}
		if args == nil {
			args = map[string]any{}
		}

		httpReq, err := buildHTTPRequest(ctx, r, args)
		if err != nil {
			return toolError(fmt.Sprintf("build request: %v", err)), nil
		}

		resp, err := client.Do(httpReq)
		if err != nil {
			return toolError(fmt.Sprintf("http: %v", err)), nil
		}
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return toolError(fmt.Sprintf("read body: %v", err)), nil
		}

		if resp.StatusCode >= 400 {
			return toolError(fmt.Sprintf("API error %d: %s", resp.StatusCode, body)), nil
		}

		// Pretty-print if the response is JSON, otherwise return as-is.
		var buf bytes.Buffer
		if json.Indent(&buf, body, "", "  ") == nil && buf.Len() > 0 {
			return toolText(buf.String()), nil
		}
		return toolText(string(body)), nil
	}
}

func buildHTTPRequest(ctx context.Context, r *route, args map[string]any) (*http.Request, error) {
	pathStr := r.path
	queryParams := url.Values{}
	reqHeaders := http.Header{}
	bodyProps := map[string]any{}

	for argName, value := range args {
		if value == nil {
			continue
		}
		loc, ok := r.paramMap[argName]
		if !ok {
			continue
		}
		strVal := fmt.Sprintf("%v", value)

		switch loc.location {
		case "path":
			pathStr = strings.ReplaceAll(pathStr, "{"+loc.apiName+"}", url.PathEscape(strVal))
		case "query":
			queryParams.Set(loc.apiName, strVal)
		case "header":
			reqHeaders.Set(loc.apiName, strVal)
		case "body":
			bodyProps[loc.apiName] = value
		}
	}

	rawURL := strings.TrimRight(r.baseURL, "/") + pathStr
	if len(queryParams) > 0 {
		rawURL += "?" + queryParams.Encode()
	}

	var bodyReader io.Reader
	if len(bodyProps) > 0 {
		data, err := json.Marshal(bodyProps)
		if err != nil {
			return nil, fmt.Errorf("marshal body: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	httpReq, err := http.NewRequestWithContext(ctx, r.method, rawURL, bodyReader)
	if err != nil {
		return nil, err
	}

	httpReq.Header.Set("Accept", "application/json")
	if bodyReader != nil {
		httpReq.Header.Set("Content-Type", "application/json")
	}
	maps.Copy(httpReq.Header, reqHeaders)

	return httpReq, nil
}

func toolText(text string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: text}},
	}
}

func toolError(text string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: text}},
		IsError: true,
	}
}
