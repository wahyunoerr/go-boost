package detector

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func buildProject(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	return dir
}

func hasPath(paths []string, want string) bool {
	for _, p := range paths {
		if p == want {
			return true
		}
	}
	return false
}

func TestDetectLayoutRecognisesLayeredProject(t *testing.T) {
	dir := buildProject(t, map[string]string{
		"go.mod":                      "module app\n\ngo 1.22\n",
		"cmd/api/main.go":             "package main\n\nfunc main() {}\n",
		"internal/handler/user.go":    "package handler\n",
		"internal/usecase/user.go":    "package usecase\n",
		"internal/repository/user.go": "package repository\n",
		"internal/domain/user.go":     "package domain\n",
		"migrations/001_init.sql":     "CREATE TABLE users (id int);",
	})

	layout := DetectLayout(dir)

	if layout.Style != "layered" {
		t.Errorf("style = %q, want layered", layout.Style)
	}
	if !hasPath(layout.MainPackages, "./cmd/api") {
		t.Errorf("main packages = %v, want ./cmd/api", layout.MainPackages)
	}
	for layer, want := range map[string]string{
		"handler":    "internal/handler",
		"usecase":    "internal/usecase",
		"repository": "internal/repository",
		"domain":     "internal/domain",
	} {
		if !hasPath(layout.Layers[layer], want) {
			t.Errorf("layer %q = %v, want %s", layer, layout.Layers[layer], want)
		}
	}
	if !hasPath(layout.Migrations, "migrations") {
		t.Errorf("migrations = %v", layout.Migrations)
	}
}

func TestDetectLayoutRecognisesFeatureModules(t *testing.T) {
	dir := buildProject(t, map[string]string{
		"go.mod":                           "module app\n\ngo 1.22\n",
		"cmd/server/main.go":               "package main\n\nfunc main() {}\n",
		"internal/user/handler/user.go":    "package handler\n",
		"internal/user/repository/user.go": "package repository\n",
		"internal/order/handler/order.go":  "package handler\n",
		"internal/order/service/order.go":  "package service\n",
	})

	layout := DetectLayout(dir)

	if layout.Style != "feature" {
		t.Errorf("style = %q, want feature", layout.Style)
	}
	if !hasPath(layout.Features, "user") || !hasPath(layout.Features, "order") {
		t.Errorf("features = %v, want user and order", layout.Features)
	}
}

func TestDetectLayoutRecognisesNonStandardNames(t *testing.T) {
	dir := buildProject(t, map[string]string{
		"go.mod":                  "module app\n\ngo 1.22\n",
		"main.go":                 "package main\n\nfunc main() {}\n",
		"app/controllers/user.go": "package controllers\n",
		"app/services/user.go":    "package services\n",
		"app/storage/user.go":     "package storage\n",
		"app/models/user.go":      "package models\n",
	})

	layout := DetectLayout(dir)

	if !hasPath(layout.Layers["handler"], "app/controllers") {
		t.Errorf("controllers not recognised as handlers: %v", layout.Layers["handler"])
	}
	if !hasPath(layout.Layers["repository"], "app/storage") {
		t.Errorf("storage not recognised as data access: %v", layout.Layers["repository"])
	}
	if !hasPath(layout.Layers["domain"], "app/models") {
		t.Errorf("models not recognised as domain: %v", layout.Layers["domain"])
	}
	if !hasPath(layout.MainPackages, ".") {
		t.Errorf("root main package not detected: %v", layout.MainPackages)
	}
}

func TestDetectLayoutHandlesFlatProject(t *testing.T) {
	dir := buildProject(t, map[string]string{
		"go.mod":  "module app\n\ngo 1.22\n",
		"main.go": "package main\n\nfunc main() {}\n",
		"user.go": "package main\n",
	})

	layout := DetectLayout(dir)

	if layout.Style != "flat" {
		t.Errorf("style = %q, want flat", layout.Style)
	}
}

func TestDetectLayoutFindsEveryEntryPoint(t *testing.T) {
	dir := buildProject(t, map[string]string{
		"go.mod":               "module app\n\ngo 1.22\n",
		"cmd/api/main.go":      "package main\n\nfunc main() {}\n",
		"cmd/worker/main.go":   "package main\n\nfunc main() {}\n",
		"cmd/migrate/main.go":  "package main\n\nfunc main() {}\n",
		"internal/shared/x.go": "package shared\n",
	})

	layout := DetectLayout(dir)

	for _, want := range []string{"./cmd/api", "./cmd/worker", "./cmd/migrate"} {
		if !hasPath(layout.MainPackages, want) {
			t.Errorf("entry point %s missing from %v", want, layout.MainPackages)
		}
	}
}

func TestDetectLayoutIgnoresVendorAndHiddenDirs(t *testing.T) {
	dir := buildProject(t, map[string]string{
		"go.mod":                    "module app\n\ngo 1.22\n",
		"internal/handler/user.go":  "package handler\n",
		"vendor/other/handler/x.go": "package handler\n",
		".cache/handler/x.go":       "package handler\n",
		"testdata/handler/x.go":     "package handler\n",
	})

	layout := DetectLayout(dir)

	for _, p := range layout.Layers["handler"] {
		if p != "internal/handler" {
			t.Errorf("unexpected directory scanned: %s", p)
		}
	}
}

func TestDetectLayoutReportsTestStyle(t *testing.T) {
	dir := buildProject(t, map[string]string{
		"go.mod":                 "module app\n\ngo 1.22\n",
		"internal/svc/a.go":      "package svc\n",
		"internal/svc/a_test.go": "package svc_test\n",
	})

	layout := DetectLayout(dir)
	if layout.TestStyle == "" {
		t.Error("expected a test package style to be reported")
	}
}

func TestDetectLayoutDoesNotTreatEntryPointsAsLayers(t *testing.T) {
	dir := buildProject(t, map[string]string{
		"go.mod":                   "module app\n\ngo 1.22\n",
		"cmd/api/main.go":          "package main\n\nfunc main() {}\n",
		"cmd/worker/main.go":       "package main\n\nfunc main() {}\n",
		"internal/handler/user.go": "package handler\n",
	})

	layout := DetectLayout(dir)

	for layer, paths := range layout.Layers {
		for _, p := range paths {
			if p == "cmd/api" || p == "cmd/worker" {
				t.Errorf("entry point %s was classified as layer %q", p, layer)
			}
		}
	}
	if !hasPath(layout.Layers["handler"], "internal/handler") {
		t.Errorf("real handler directory lost: %v", layout.Layers["handler"])
	}
}

func TestDetectLayoutAlwaysReportsForwardSlashPaths(t *testing.T) {
	dir := buildProject(t, map[string]string{
		"go.mod":                           "module app\n\ngo 1.22\n",
		"cmd/api/main.go":                  "package main\n\nfunc main() {}\n",
		"internal/user/handler/user.go":    "package handler\n",
		"internal/user/repository/user.go": "package repository\n",
		"db/migrations/001.sql":            "CREATE TABLE users (id int);",
		"configs/config.go":                "package configs\n",
	})

	layout := DetectLayout(dir)

	var all []string
	all = append(all, layout.MainPackages...)
	all = append(all, layout.SourceRoots...)
	all = append(all, layout.Migrations...)
	all = append(all, layout.ConfigDirs...)
	for _, paths := range layout.Layers {
		all = append(all, paths...)
	}

	if len(all) == 0 {
		t.Fatal("expected the layout to report some paths")
	}
	for _, p := range all {
		if strings.Contains(p, `\`) {
			t.Errorf("path %q uses a backslash; guidelines must read the same on every platform", p)
		}
	}
}
