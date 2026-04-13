// Vikunja MCP - Go port
// github.com/acidvegas/vikunja-mcp

package main

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

//go:embed instructions.txt
var instructions string

// ----------------------------------------------------------------------------
// Configuration
// ----------------------------------------------------------------------------

var (
	vikunjaURL string
	token      string
	baseURL    string
	specURL    string
	headers    map[string]string

	httpClient = &http.Client{Timeout: 30 * time.Second}
	specClient = &http.Client{Timeout: 15 * time.Second}

	logger = log.New(os.Stderr, "vikunja-mcp: ", log.LstdFlags)
)

// allowKey is a (METHOD, path) pair used for the allowlist set.
type allowKey struct {
	method string
	path   string
}

// allowlist mirrors ALLOWLIST in server.py. Paths use the upstream
// Vikunja spec parameter names verbatim.
var allowlist = map[allowKey]struct{}{
	// server / user
	{"GET", "/info"}:    {},
	{"GET", "/user"}:    {},
	{"GET", "/users"}:   {},
	// projects
	{"GET", "/projects"}:                         {},
	{"PUT", "/projects"}:                         {},
	{"GET", "/projects/{id}"}:                    {},
	{"POST", "/projects/{id}"}:                   {},
	{"DELETE", "/projects/{id}"}:                 {},
	{"PUT", "/projects/{projectID}/duplicate"}:   {},
	{"GET", "/projects/{id}/projectusers"}:       {},
	// views
	{"GET", "/projects/{project}/views"}:          {},
	{"PUT", "/projects/{project}/views"}:          {},
	{"GET", "/projects/{project}/views/{id}"}:     {},
	{"POST", "/projects/{project}/views/{id}"}:    {},
	{"DELETE", "/projects/{project}/views/{id}"}: {},
	// buckets
	{"GET", "/projects/{id}/views/{view}/buckets"}:                      {},
	{"PUT", "/projects/{id}/views/{view}/buckets"}:                      {},
	{"POST", "/projects/{projectID}/views/{view}/buckets/{bucketID}"}:   {},
	{"DELETE", "/projects/{projectID}/views/{view}/buckets/{bucketID}"}: {},
	// tasks
	{"GET", "/tasks"}:                                                          {},
	{"GET", "/tasks/{id}"}:                                                     {},
	{"POST", "/tasks/{id}"}:                                                    {},
	{"DELETE", "/tasks/{id}"}:                                                  {},
	{"PUT", "/projects/{id}/tasks"}:                                            {},
	{"POST", "/tasks/bulk"}:                                                    {},
	{"POST", "/tasks/{id}/position"}:                                           {},
	{"POST", "/tasks/{projecttask}/read"}:                                      {},
	{"GET", "/projects/{id}/views/{view}/tasks"}:                               {},
	{"POST", "/projects/{project}/views/{view}/buckets/{bucket}/tasks"}:        {},
	// task relations
	{"PUT", "/tasks/{taskID}/relations"}:                                    {},
	{"DELETE", "/tasks/{taskID}/relations/{relationKind}/{otherTaskID}"}:    {},
	// assignees
	{"GET", "/tasks/{taskID}/assignees"}:              {},
	{"PUT", "/tasks/{taskID}/assignees"}:              {},
	{"POST", "/tasks/{taskID}/assignees/bulk"}:        {},
	{"DELETE", "/tasks/{taskID}/assignees/{userID}"}:  {},
	// labels
	{"GET", "/labels"}:                             {},
	{"PUT", "/labels"}:                             {},
	{"GET", "/labels/{id}"}:                        {},
	{"POST", "/labels/{id}"}:                       {},
	{"DELETE", "/labels/{id}"}:                     {},
	{"GET", "/tasks/{task}/labels"}:                {},
	{"PUT", "/tasks/{task}/labels"}:                {},
	{"DELETE", "/tasks/{task}/labels/{label}"}:     {},
	{"POST", "/tasks/{taskID}/labels/bulk"}:        {},
	// comments
	{"GET", "/tasks/{taskID}/comments"}:                 {},
	{"GET", "/tasks/{taskID}/comments/{commentID}"}:     {},
	{"PUT", "/tasks/{taskID}/comments"}:                 {},
	{"POST", "/tasks/{taskID}/comments/{commentID}"}:    {},
	{"DELETE", "/tasks/{taskID}/comments/{commentID}"}: {},
	// attachments
	{"GET", "/tasks/{id}/attachments"}:                    {},
	{"GET", "/tasks/{id}/attachments/{attachmentID}"}:     {},
	{"PUT", "/tasks/{id}/attachments"}:                    {},
	{"DELETE", "/tasks/{id}/attachments/{attachmentID}"}: {},
	// reactions
	{"GET", "/{kind}/{id}/reactions"}:          {},
	{"PUT", "/{kind}/{id}/reactions"}:          {},
	{"POST", "/{kind}/{id}/reactions/delete"}:  {},
	// filters
	{"PUT", "/filters"}:           {},
	{"GET", "/filters/{id}"}:      {},
	{"POST", "/filters/{id}"}:     {},
	{"DELETE", "/filters/{id}"}:   {},
	// teams
	{"GET", "/teams"}:                                    {},
	{"PUT", "/teams"}:                                    {},
	{"GET", "/teams/{id}"}:                               {},
	{"POST", "/teams/{id}"}:                              {},
	{"DELETE", "/teams/{id}"}:                            {},
	{"PUT", "/teams/{id}/members"}:                       {},
	{"POST", "/teams/{id}/members/{userID}/admin"}:       {},
	{"DELETE", "/teams/{id}/members/{username}"}:         {},
	// sharing
	{"GET", "/projects/{id}/users"}:                       {},
	{"PUT", "/projects/{id}/users"}:                       {},
	{"POST", "/projects/{projectID}/users/{userID}"}:      {},
	{"DELETE", "/projects/{projectID}/users/{userID}"}:    {},
	{"GET", "/projects/{id}/teams"}:                       {},
	{"PUT", "/projects/{id}/teams"}:                       {},
	{"POST", "/projects/{projectID}/teams/{teamID}"}:      {},
	{"DELETE", "/projects/{projectID}/teams/{teamID}"}:    {},
	// link shares
	{"GET", "/projects/{project}/shares"}:            {},
	{"GET", "/projects/{project}/shares/{share}"}:    {},
	{"PUT", "/projects/{project}/shares"}:            {},
	{"DELETE", "/projects/{project}/shares/{share}"}: {},
	// subscriptions / notifications
	{"PUT", "/subscriptions/{entity}/{entityID}"}:    {},
	{"DELETE", "/subscriptions/{entity}/{entityID}"}: {},
	{"GET", "/notifications"}:                        {},
	{"POST", "/notifications"}:                       {},
	{"POST", "/notifications/{id}"}:                  {},
	// webhooks
	{"GET", "/projects/{id}/webhooks"}:                  {},
	{"PUT", "/projects/{id}/webhooks"}:                  {},
	{"POST", "/projects/{id}/webhooks/{webhookID}"}:     {},
	{"DELETE", "/projects/{id}/webhooks/{webhookID}"}:   {},
	{"GET", "/webhooks/events"}:                         {},
}

// specPatch describes a known upstream spec fix.
type specPatch struct {
	path    string
	wrong   string
	correct string
}

var specPatches = []specPatch{
	{"/labels/{id}", "put", "post"},
}

// ----------------------------------------------------------------------------
// OpenAPI helpers
// ----------------------------------------------------------------------------

var (
	sanitizeRe  = regexp.MustCompile(`[^a-zA-Z0-9_-]+`)
	pathParamRe = regexp.MustCompile(`\{(\w+)\}`)
	httpMethods = map[string]struct{}{
		"get": {}, "post": {}, "put": {}, "delete": {}, "patch": {},
	}
)

func openapiToJSON(t string) string {
	switch t {
	case "integer":
		return "integer"
	case "number":
		return "number"
	case "boolean":
		return "boolean"
	case "array":
		return "array"
	case "file":
		return "string"
	default:
		return "string"
	}
}

func resolveRef(spec map[string]interface{}, ref string) map[string]interface{} {
	node := interface{}(spec)
	trimmed := strings.TrimPrefix(ref, "#/")
	for _, part := range strings.Split(trimmed, "/") {
		m, ok := node.(map[string]interface{})
		if !ok {
			return map[string]interface{}{}
		}
		node = m[part]
	}
	if m, ok := node.(map[string]interface{}); ok {
		return m
	}
	return map[string]interface{}{}
}

func resolveBodySchema(param, spec map[string]interface{}) map[string]interface{} {
	schema, _ := param["schema"].(map[string]interface{})
	if schema == nil {
		schema = map[string]interface{}{}
	}
	if ref, ok := schema["$ref"].(string); ok {
		schema = resolveRef(spec, ref)
	}

	if t, _ := schema["type"].(string); t != "object" {
		return map[string]interface{}{"type": "object"}
	}
	props, ok := schema["properties"].(map[string]interface{})
	if !ok {
		return map[string]interface{}{"type": "object"}
	}

	out := map[string]interface{}{}
	for name, raw := range props {
		prop, _ := raw.(map[string]interface{})
		if prop == nil {
			continue
		}
		ptype, _ := prop["type"].(string)
		entry := map[string]interface{}{"type": openapiToJSON(ptype)}
		if desc, ok := prop["description"].(string); ok {
			entry["description"] = strings.TrimSpace(desc)
		}
		if ptype == "array" {
			if items, ok := prop["items"].(map[string]interface{}); ok {
				if ref, ok := items["$ref"].(string); ok {
					items = resolveRef(spec, ref)
				}
				itype, _ := items["type"].(string)
				entry["items"] = map[string]interface{}{"type": openapiToJSON(itype)}
			}
		}
		out[name] = entry
	}
	return map[string]interface{}{"type": "object", "properties": out}
}

func patchSpec(spec map[string]interface{}) {
	paths, _ := spec["paths"].(map[string]interface{})
	if paths == nil {
		return
	}
	for _, p := range specPatches {
		node, _ := paths[p.path].(map[string]interface{})
		if node == nil {
			continue
		}
		if _, hasWrong := node[p.wrong]; !hasWrong {
			continue
		}
		if _, hasCorrect := node[p.correct]; hasCorrect {
			continue
		}
		node[p.correct] = node[p.wrong]
		delete(node, p.wrong)
		logger.Printf("spec patch: %s %s -> %s", p.path, strings.ToUpper(p.wrong), strings.ToUpper(p.correct))
	}
}

func sanitizeName(raw string) string {
	name := sanitizeRe.ReplaceAllString(raw, "_")
	name = strings.Trim(name, "_")
	if len(name) > 64 {
		name = name[:64]
	}
	if name == "" {
		return "op"
	}
	return name
}

// ----------------------------------------------------------------------------
// Spec loading
// ----------------------------------------------------------------------------

func loadSpec(ctx context.Context) (map[string]interface{}, error) {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		req, err := http.NewRequestWithContext(ctx, "GET", specURL, nil)
		if err != nil {
			return nil, err
		}
		resp, err := specClient.Do(req)
		if err == nil && resp.StatusCode >= 400 {
			resp.Body.Close()
			err = fmt.Errorf("HTTP %d fetching spec", resp.StatusCode)
		}
		if err == nil {
			defer resp.Body.Close()
			body, rerr := io.ReadAll(resp.Body)
			if rerr == nil {
				var spec map[string]interface{}
				if jerr := json.Unmarshal(body, &spec); jerr == nil {
					return spec, nil
				} else {
					err = jerr
				}
			} else {
				err = rerr
			}
		}
		lastErr = err
		if attempt == 2 {
			break
		}
		delay := time.Duration(1<<uint(attempt+1)) * time.Second // 2s, 4s
		logger.Printf("spec load attempt %d failed (%v), retrying in %v", attempt+1, err, delay)
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return nil, fmt.Errorf("spec load failed after 3 attempts: %w", lastErr)
}

// ----------------------------------------------------------------------------
// Tool building
// ----------------------------------------------------------------------------

type operation struct {
	method string
	path   string
	params []map[string]interface{}
}

type builtTool struct {
	tool mcp.Tool
	op   operation
}

func buildTools(spec map[string]interface{}) []builtTool {
	patchSpec(spec)

	var tools []builtTool
	paths, _ := spec["paths"].(map[string]interface{})
	if paths == nil {
		return tools
	}

	for path, rawMethods := range paths {
		methods, _ := rawMethods.(map[string]interface{})
		if methods == nil {
			continue
		}
		for method, rawOp := range methods {
			if _, ok := httpMethods[method]; !ok {
				continue
			}
			if _, ok := allowlist[allowKey{strings.ToUpper(method), path}]; !ok {
				continue
			}
			op, _ := rawOp.(map[string]interface{})
			if op == nil {
				continue
			}

			opID, _ := op["operationId"].(string)
			rawName := opID
			if rawName == "" {
				rawName = fmt.Sprintf("%s_%s", method, path)
			}
			name := sanitizeName(rawName)

			desc, _ := op["summary"].(string)
			if desc == "" {
				desc, _ = op["description"].(string)
			}
			if desc == "" {
				desc = fmt.Sprintf("%s %s", strings.ToUpper(method), path)
			}
			desc = strings.TrimSpace(desc)
			if len(desc) > 1024 {
				desc = desc[:1024]
			}

			properties := map[string]interface{}{}
			var required []string
			var paramList []map[string]interface{}

			if raw, ok := op["parameters"].([]interface{}); ok {
				for _, rp := range raw {
					param, _ := rp.(map[string]interface{})
					if param == nil {
						continue
					}
					paramList = append(paramList, param)
					pname, _ := param["name"].(string)
					if pname == "" {
						continue
					}
					loc, _ := param["in"].(string)

					if loc == "body" {
						bs := resolveBodySchema(param, spec)
						if _, ok := bs["description"]; !ok {
							bs["description"] = fmt.Sprintf("Request body for %s %s", strings.ToUpper(method), path)
						}
						properties[pname] = bs
					} else {
						ptype, _ := param["type"].(string)
						pdesc, _ := param["description"].(string)
						pdesc = strings.TrimSpace(pdesc)
						if pdesc == "" {
							pdesc = fmt.Sprintf("%s parameter", loc)
						}
						properties[pname] = map[string]interface{}{
							"type":        openapiToJSON(ptype),
							"description": pdesc,
						}
					}

					if req, _ := param["required"].(bool); req {
						required = append(required, pname)
					}
				}
			}

			inputSchema := mcp.ToolInputSchema{
				Type:       "object",
				Properties: properties,
				Required:   required,
			}
			tool := mcp.Tool{
				Name:        name,
				Description: desc,
				InputSchema: inputSchema,
			}
			tools = append(tools, builtTool{
				tool: tool,
				op: operation{
					method: strings.ToUpper(method),
					path:   path,
					params: paramList,
				},
			})
		}
	}
	return tools
}

// ----------------------------------------------------------------------------
// Endpoint execution
// ----------------------------------------------------------------------------

func callEndpoint(ctx context.Context, op operation, args map[string]interface{}) (string, error) {
	path := op.path
	query := url.Values{}
	var body interface{}
	hasBody := false

	for _, param := range op.params {
		pname, _ := param["name"].(string)
		if pname == "" {
			continue
		}
		val, present := args[pname]
		if !present || val == nil {
			continue
		}
		loc, _ := param["in"].(string)
		switch loc {
		case "path":
			path = strings.ReplaceAll(path, "{"+pname+"}", fmt.Sprintf("%v", val))
		case "query":
			query.Set(pname, fmt.Sprintf("%v", val))
		case "body":
			body = val
			hasBody = true
		}
	}

	if matches := pathParamRe.FindAllStringSubmatch(path, -1); len(matches) > 0 {
		names := make([]string, 0, len(matches))
		for _, m := range matches {
			names = append(names, m[1])
		}
		msg := fmt.Sprintf("error: missing required path parameters: %s", strings.Join(names, ", "))
		logger.Printf("%s", msg)
		return msg, nil
	}

	full := baseURL + path
	if len(query) > 0 {
		full += "?" + query.Encode()
	}

	var reqBody io.Reader
	if hasBody {
		buf, err := json.Marshal(body)
		if err != nil {
			return "", err
		}
		reqBody = bytes.NewReader(buf)
	}

	req, err := http.NewRequestWithContext(ctx, op.method, full, reqBody)
	if err != nil {
		return "", err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	if hasBody {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode >= 400 {
		logger.Printf("%s %s -> HTTP %d", op.method, path, resp.StatusCode)
	}
	return fmt.Sprintf("HTTP %d\n%s", resp.StatusCode, string(respBody)), nil
}

// ----------------------------------------------------------------------------
// MCP server wiring
// ----------------------------------------------------------------------------

func newMCPServer(ctx context.Context) (*server.MCPServer, error) {
	spec, err := loadSpec(ctx)
	if err != nil {
		return nil, fmt.Errorf("load spec: %w", err)
	}
	built := buildTools(spec)
	logger.Printf("loaded %d tools from vikunja spec", len(built))

	s := server.NewMCPServer(
		"vikunja",
		"0.1.0",
		server.WithInstructions(instructions),
		server.WithToolCapabilities(false),
	)

	// Build name→op index for dispatch inside closures.
	index := make(map[string]operation, len(built))
	for _, bt := range built {
		index[bt.tool.Name] = bt.op
	}

	for _, bt := range built {
		name := bt.tool.Name
		s.AddTool(bt.tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			op, ok := index[name]
			if !ok {
				logger.Printf("unknown tool requested: %s", name)
				return mcp.NewToolResultText(fmt.Sprintf("unknown tool: %s", name)), nil
			}
			args := req.GetArguments()
			if args == nil {
				args = map[string]interface{}{}
			}
			logger.Printf("tool call: %s -> %s %s", name, op.method, op.path)
			result, err := callEndpoint(ctx, op, args)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return mcp.NewToolResultText(result), nil
		})
	}

	return s, nil
}

// ----------------------------------------------------------------------------
// Entry point
// ----------------------------------------------------------------------------

func main() {
	vikunjaURL = strings.TrimRight(os.Getenv("VIKUNJA_URL"), "/")
	if vikunjaURL == "" {
		fmt.Fprintln(os.Stderr, "VIKUNJA_URL is not set. Export it before launching the server.")
		os.Exit(1)
	}
	token = os.Getenv("VIKUNJA_TOKEN")
	if token == "" {
		fmt.Fprintln(os.Stderr, "VIKUNJA_TOKEN is not set. Export it before launching the server.")
		os.Exit(1)
	}

	baseURL = vikunjaURL + "/api/v1"
	specURL = baseURL + "/docs.json"
	headers = map[string]string{
		"Authorization": "Bearer " + token,
		"Accept":        "application/json",
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	mcpServer, err := newMCPServer(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "startup error: %v\n", err)
		os.Exit(1)
	}

	if err := server.ServeStdio(mcpServer); err != nil {
		if ctx.Err() != nil {
			return
		}
		fmt.Fprintf(os.Stderr, "server error: %v\n", err)
		os.Exit(1)
	}
}
