package mcp

import (
	"context"
	"encoding/json"

	"github.com/wahyunoerr/go-boost/v2/pkg/astparser"
	"github.com/wahyunoerr/go-boost/v2/pkg/database"
	"github.com/wahyunoerr/go-boost/v2/pkg/detector"
	"github.com/wahyunoerr/go-boost/v2/pkg/runner"
)

func RegisterAllResources(s *Server, rootDir string) {
	s.RegisterResource(Resource{
		URI:         "project://metadata",
		Name:        "Project Metadata",
		Description: "Complete detected Go stack, module name, dependencies, and architecture",
		MimeType:    "application/json",
	}, func(ctx context.Context, uri string) (*ReadResourceResult, error) {
		d := detector.NewDetector(rootDir)
		stack, err := d.Detect()
		if err != nil {
			return nil, err
		}
		data, _ := json.MarshalIndent(stack, "", "  ")
		return &ReadResourceResult{
			Contents: []ResourceContent{
				{URI: uri, MimeType: "application/json", Text: string(data)},
			},
		}, nil
	})

	s.RegisterResource(Resource{
		URI:         "project://routes",
		Name:        "HTTP Routes",
		Description: "All HTTP endpoints and handlers mapped from project AST",
		MimeType:    "application/json",
	}, func(ctx context.Context, uri string) (*ReadResourceResult, error) {
		routes, err := astparser.ScanRoutes(rootDir)
		if err != nil {
			return nil, err
		}
		data, _ := json.MarshalIndent(routes, "", "  ")
		return &ReadResourceResult{
			Contents: []ResourceContent{
				{URI: uri, MimeType: "application/json", Text: string(data)},
			},
		}, nil
	})

	s.RegisterResource(Resource{
		URI:         "project://schema",
		Name:        "Database Schema",
		Description: "Database schema tables and column metadata",
		MimeType:    "application/json",
	}, func(ctx context.Context, uri string) (*ReadResourceResult, error) {
		conns, err := database.DiscoverConnections(rootDir)
		if err != nil || len(conns.Connections) == 0 {
			return &ReadResourceResult{
				Contents: []ResourceContent{
					{URI: uri, MimeType: "application/json", Text: `{"message": "No database connection configured"}`},
				},
			}, nil
		}
		schema, err := database.Introspect(ctx, &conns.Connections[0], database.SchemaOptions{Summary: true})
		if err != nil {
			return nil, err
		}
		data, _ := json.MarshalIndent(schema, "", "  ")
		return &ReadResourceResult{
			Contents: []ResourceContent{
				{URI: uri, MimeType: "application/json", Text: string(data)},
			},
		}, nil
	})

	s.RegisterResource(Resource{
		URI:         "project://diagnostics",
		Name:        "Project Diagnostics",
		Description: "Live compiler vet and static check results",
		MimeType:    "text/plain",
	}, func(ctx context.Context, uri string) (*ReadResourceResult, error) {
		diag, err := runner.CodeCheck(ctx, rootDir, "./...")
		if err != nil {
			return nil, err
		}
		return &ReadResourceResult{
			Contents: []ResourceContent{
				{URI: uri, MimeType: "text/plain", Text: diag},
			},
		}, nil
	})
}
