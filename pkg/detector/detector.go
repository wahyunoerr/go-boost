package detector

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

type Detector struct {
	rootDir string
}

func NewDetector(rootDir string) *Detector {
	return &Detector{rootDir: rootDir}
}

func (d *Detector) Detect() (*ProjectStack, error) {
	stack := &ProjectStack{
		RootPath:       d.rootDir,
		Framework:      "net/http",
		ORM:            "database/sql",
		DatabaseEngine: "unknown",
		CacheEngine:    "none",
		QueueEngine:    "none",
		ConfigManager:  "none",
		RPC:            "none",
		Logger:         "slog",
		Validator:      "none",
		TestFramework:  "testing",
		Architecture:   "standard_flat",
		Packages:       make([]PackageInfo, 0),
		DetectedDirs:   make(map[string]bool),
	}

	goModPath := filepath.Join(d.rootDir, "go.mod")
	if _, err := os.Stat(goModPath); err == nil {
		if err := d.parseGoMod(goModPath, stack); err != nil {
			return nil, err
		}
	}

	if stack.ORM == "database/sql" {
		if _, err := os.Stat(filepath.Join(d.rootDir, "sqlc.yaml")); err == nil {
			stack.ORM = "sqlc"
		} else if _, err := os.Stat(filepath.Join(d.rootDir, "sqlc.json")); err == nil {
			stack.ORM = "sqlc"
		}
	}

	d.scanDirectoryStructure(stack)

	d.inferArchitecture(stack)

	return stack, nil
}

func (d *Detector) parseGoMod(path string, stack *ProjectStack) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	inRequireBlock := false

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "//") {
			continue
		}

		if strings.HasPrefix(line, "module ") {
			stack.ModuleName = strings.TrimSpace(strings.TrimPrefix(line, "module "))
			continue
		}

		if strings.HasPrefix(line, "go ") {
			stack.GoVersion = strings.TrimSpace(strings.TrimPrefix(line, "go "))
			continue
		}

		if line == "require (" {
			inRequireBlock = true
			continue
		}

		if inRequireBlock && line == ")" {
			inRequireBlock = false
			continue
		}

		if inRequireBlock {
			d.processRequireLine(line, stack)
			continue
		}

		if strings.HasPrefix(line, "require ") {
			reqLine := strings.TrimPrefix(line, "require ")
			d.processRequireLine(reqLine, stack)
			continue
		}
	}

	return scanner.Err()
}

func (d *Detector) processRequireLine(line string, stack *ProjectStack) {
	parts := strings.Fields(line)
	if len(parts) < 2 {
		return
	}

	pkgName := parts[0]
	version := parts[1]
	indirect := strings.Contains(line, "// indirect")

	stack.Packages = append(stack.Packages, PackageInfo{
		Name:     pkgName,
		Version:  version,
		Indirect: indirect,
	})

	if indirect {
		return
	}

	if framework, ok := matchModule(pkgName, frameworkModules); ok {
		stack.Framework = framework
		stack.FrameworkVersion = version
	}

	if orm, ok := matchModule(pkgName, ormModules); ok {
		if orm != "pgx" || stack.ORM == "database/sql" {
			stack.ORM = orm
			stack.ORMVersion = version
		}
	}

	if engine, ok := matchModule(pkgName, databaseModules); ok {
		stack.DatabaseEngine = engine
	}

	if hasModulePrefix(pkgName, "github.com/redis/go-redis") || hasModulePrefix(pkgName, "github.com/go-redis/redis") {
		stack.CacheEngine = "redis"
	} else if hasModulePrefix(pkgName, "github.com/bradfitz/gomemcache") {
		stack.CacheEngine = "memcached"
	}

	if hasModulePrefix(pkgName, "github.com/segmentio/kafka-go") || hasModulePrefix(pkgName, "github.com/confluentinc/confluent-kafka-go") || hasModulePrefix(pkgName, "github.com/IBM/sarama") {
		stack.QueueEngine = "kafka"
	} else if hasModulePrefix(pkgName, "github.com/rabbitmq/amqp091-go") || hasModulePrefix(pkgName, "github.com/streadway/amqp") {
		stack.QueueEngine = "rabbitmq"
	} else if hasModulePrefix(pkgName, "github.com/nats-io/nats.go") {
		stack.QueueEngine = "nats"
	}

	if hasModulePrefix(pkgName, "github.com/spf13/viper") {
		stack.ConfigManager = "viper"
	} else if hasModulePrefix(pkgName, "github.com/kelseyhightower/envconfig") || hasModulePrefix(pkgName, "github.com/caarlos0/env") {
		stack.ConfigManager = "envconfig"
	} else if hasModulePrefix(pkgName, "github.com/joho/godotenv") && stack.ConfigManager == "none" {
		stack.ConfigManager = "godotenv"
	}

	if hasModulePrefix(pkgName, "google.golang.org/grpc") {
		stack.RPC = "grpc"
	}

	if hasModulePrefix(pkgName, "go.uber.org/zap") {
		stack.Logger = "zap"
	} else if hasModulePrefix(pkgName, "github.com/rs/zerolog") {
		stack.Logger = "zerolog"
	} else if hasModulePrefix(pkgName, "github.com/sirupsen/logrus") {
		stack.Logger = "logrus"
	}

	if hasModulePrefix(pkgName, "github.com/go-playground/validator") {
		stack.Validator = "validator.v10"
	}

	if hasModulePrefix(pkgName, "github.com/stretchr/testify") {
		stack.TestFramework = "testify"
	}
}

func (d *Detector) scanDirectoryStructure(stack *ProjectStack) {
	commonDirs := []string{
		"cmd", "internal", "pkg", "api", "configs", "migrations",
		"internal/domain", "internal/entity", "internal/repository",
		"internal/usecase", "internal/service", "internal/handler",
		"internal/delivery", "internal/adapter", "internal/ports",
	}

	for _, rel := range commonDirs {
		target := filepath.Join(d.rootDir, rel)
		if info, err := os.Stat(target); err == nil && info.IsDir() {
			stack.DetectedDirs[rel] = true
		}
	}
}

func (d *Detector) inferArchitecture(stack *ProjectStack) {
	hasDomain := stack.DetectedDirs["internal/domain"] || stack.DetectedDirs["internal/entity"]
	hasRepo := stack.DetectedDirs["internal/repository"]
	hasUsecase := stack.DetectedDirs["internal/usecase"] || stack.DetectedDirs["internal/service"]
	hasHandler := stack.DetectedDirs["internal/handler"] || stack.DetectedDirs["internal/delivery"]

	if (hasDomain || hasRepo) && (hasUsecase || hasHandler) {
		stack.Architecture = "clean_architecture"
		return
	}

	if stack.DetectedDirs["internal/adapter"] || stack.DetectedDirs["internal/ports"] {
		stack.Architecture = "hexagonal"
		return
	}

	stack.Architecture = "standard_flat"
}

var frameworkModules = []moduleMatch{
	{"github.com/gin-gonic/gin", "gin"},
	{"github.com/gofiber/fiber", "fiber"},
	{"github.com/labstack/echo", "echo"},
	{"github.com/go-chi/chi", "chi"},
	{"github.com/gorilla/mux", "gorilla/mux"},
	{"github.com/zeromicro/go-zero", "go-zero"},
}

var ormModules = []moduleMatch{
	{"gorm.io/gorm", "gorm"},
	{"github.com/uptrace/bun", "bun"},
	{"github.com/jmoiron/sqlx", "sqlx"},
	{"entgo.io/ent", "ent"},
	{"github.com/sqlc-dev/sqlc", "sqlc"},
	{"github.com/jackc/pgx", "pgx"},
}

var databaseModules = []moduleMatch{
	{"github.com/lib/pq", "postgres"},
	{"github.com/jackc/pgx", "postgres"},
	{"github.com/jackc/pgconn", "postgres"},
	{"gorm.io/driver/postgres", "postgres"},
	{"github.com/go-sql-driver/mysql", "mysql"},
	{"gorm.io/driver/mysql", "mysql"},
	{"github.com/mattn/go-sqlite3", "sqlite"},
	{"modernc.org/sqlite", "sqlite"},
	{"gorm.io/driver/sqlite", "sqlite"},
	{"go.mongodb.org/mongo-driver", "mongodb"},
}

type moduleMatch struct {
	prefix string
	value  string
}

func matchModule(pkgName string, candidates []moduleMatch) (string, bool) {
	for _, c := range candidates {
		if hasModulePrefix(pkgName, c.prefix) {
			return c.value, true
		}
	}
	return "", false
}

func hasModulePrefix(pkgName, prefix string) bool {
	if pkgName == prefix {
		return true
	}
	return strings.HasPrefix(pkgName, prefix+"/")
}
