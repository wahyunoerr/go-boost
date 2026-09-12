package generator

import (
	"encoding/json"
	"regexp"
	"strings"

	"github.com/wahyunoerr/go-boost/pkg/astparser"
)

type OpenAPIDoc struct {
	OpenAPI string                          `json:"openapi"`
	Info    OpenAPIInfo                     `json:"info"`
	Paths   map[string]map[string]Operation `json:"paths"`
}

type OpenAPIInfo struct {
	Title       string `json:"title"`
	Version     string `json:"version"`
	Description string `json:"description"`
}

type Operation struct {
	Summary     string              `json:"summary"`
	OperationID string              `json:"operationId"`
	Parameters  []OpenAPIParameter  `json:"parameters,omitempty"`
	RequestBody map[string]any      `json:"requestBody,omitempty"`
	Responses   map[string]Response `json:"responses"`
}

type OpenAPIParameter struct {
	Name     string         `json:"name"`
	In       string         `json:"in"`
	Required bool           `json:"required"`
	Schema   map[string]any `json:"schema"`
}

type Response struct {
	Description string `json:"description"`
}

var pathParamRegex = regexp.MustCompile(`[:{]([a-zA-Z0-9_]+)}?`)

func GenerateOpenAPISpec(rootDir string, title string) (string, error) {
	routes, err := astparser.ScanRoutes(rootDir)
	if err != nil {
		return "", err
	}

	if title == "" {
		title = "Go API Specification"
	}

	doc := OpenAPIDoc{
		OpenAPI: "3.0.3",
		Info: OpenAPIInfo{
			Title:       title,
			Version:     "1.0.0",
			Description: "Statically generated OpenAPI specification by go-boost",
		},
		Paths: make(map[string]map[string]Operation),
	}

	for _, r := range routes {
		normPath := normalizeOpenAPIPath(r.Path)
		methodLower := strings.ToLower(r.Method)
		if methodLower == "any" {
			methodLower = "get"
		}

		if doc.Paths[normPath] == nil {
			doc.Paths[normPath] = make(map[string]Operation)
		}

		params := extractPathParameters(normPath)

		op := Operation{
			Summary:     "Handler: " + r.Handler,
			OperationID: cleanOperationID(r.Handler + "_" + r.Method + "_" + normPath),
			Parameters:  params,
			Responses: map[string]Response{
				"200": {Description: "Successful operation"},
			},
		}

		if methodLower == "post" || methodLower == "put" || methodLower == "patch" {
			op.RequestBody = map[string]any{
				"required": true,
				"content": map[string]any{
					"application/json": map[string]any{
						"schema": map[string]any{
							"type": "object",
						},
					},
				},
			}
		}

		doc.Paths[normPath][methodLower] = op
	}

	b, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return "", err
	}

	return string(b), nil
}

func normalizeOpenAPIPath(path string) string {
	parts := strings.Split(path, "/")
	for i, p := range parts {
		if strings.HasPrefix(p, ":") {
			parts[i] = "{" + strings.TrimPrefix(p, ":") + "}"
		} else if p == "*" {
			parts[i] = "{wildcard}"
		} else if strings.HasPrefix(p, "*") {
			parts[i] = "{" + strings.TrimPrefix(p, "*") + "}"
		} else if strings.HasPrefix(p, "{") && strings.HasSuffix(p, "...}") {
			parts[i] = strings.TrimSuffix(p, "...}") + "}"
		}
	}
	res := strings.Join(parts, "/")
	if !strings.HasPrefix(res, "/") {
		res = "/" + res
	}
	return res
}

func extractPathParameters(path string) []OpenAPIParameter {
	matches := pathParamRegex.FindAllStringSubmatch(path, -1)
	var params []OpenAPIParameter

	for _, m := range matches {
		if len(m) > 1 {
			paramName := m[1]
			params = append(params, OpenAPIParameter{
				Name:     paramName,
				In:       "path",
				Required: true,
				Schema: map[string]any{
					"type": "string",
				},
			})
		}
	}

	return params
}

func cleanOperationID(raw string) string {
	re := regexp.MustCompile(`[^a-zA-Z0-9_]`)
	return re.ReplaceAllString(raw, "_")
}
