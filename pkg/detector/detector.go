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

	if strings.Contains(pkgName, "github.com/gin-gonic/gin") {
		stack.Framework = "gin"
		stack.FrameworkVersion = version
	} else if strings.Contains(pkgName, "github.com/gofiber/fiber") {
		stack.Framework = "fiber"
		stack.FrameworkVersion = version
	} else if strings.Contains(pkgName, "github.com/labstack/echo") {
		stack.Framework = "echo"
		stack.FrameworkVersion = version
	} else if strings.Contains(pkgName, "github.com/go-chi/chi") {
		stack.Framework = "chi"
		stack.FrameworkVersion = version
	} else if strings.Contains(pkgName, "github.com/gorilla/mux") {
		stack.Framework = "gorilla/mux"
		stack.FrameworkVersion = version
	} else if strings.Contains(pkgName, "github.com/zeromicro/go-zero") {
		stack.Framework = "go-zero"
		stack.FrameworkVersion = version
	}

	if strings.Contains(pkgName, "gorm.io/gorm") {
		stack.ORM = "gorm"
		stack.ORMVersion = version
	} else if strings.Contains(pkgName, "github.com/uptrace/bun") {
		stack.ORM = "bun"
		stack.ORMVersion = version
	} else if strings.Contains(pkgName, "github.com/jmoiron/sqlx") {
		stack.ORM = "sqlx"
		stack.ORMVersion = version
	} else if strings.Contains(pkgName, "entgo.io/ent") {
		stack.ORM = "ent"
		stack.ORMVersion = version
	} else if strings.Contains(pkgName, "github.com/sqlc-dev/sqlc") {
		stack.ORM = "sqlc"
		stack.ORMVersion = version
	} else if strings.Contains(pkgName, "github.com/jackc/pgx") {
		if stack.ORM == "database/sql" {
			stack.ORM = "pgx"
			stack.ORMVersion = version
		}
	}

	if strings.Contains(pkgName, "postgres") || strings.Contains(pkgName, "pq") || strings.Contains(pkgName, "pgx") {
		stack.DatabaseEngine = "postgres"
	} else if strings.Contains(pkgName, "mysql") {
		stack.DatabaseEngine = "mysql"
	} else if strings.Contains(pkgName, "sqlite") {
		stack.DatabaseEngine = "sqlite"
	}

	if strings.Contains(pkgName, "github.com/redis/go-redis") || strings.Contains(pkgName, "github.com/go-redis/redis") {
		stack.CacheEngine = "redis"
	} else if strings.Contains(pkgName, "github.com/bradfitz/gomemcache") {
		stack.CacheEngine = "memcached"
	}

	if strings.Contains(pkgName, "github.com/segmentio/kafka-go") || strings.Contains(pkgName, "github.com/confluentinc/confluent-kafka-go") || strings.Contains(pkgName, "github.com/IBM/sarama") {
		stack.QueueEngine = "kafka"
	} else if strings.Contains(pkgName, "github.com/rabbitmq/amqp091-go") || strings.Contains(pkgName, "github.com/streadway/amqp") {
		stack.QueueEngine = "rabbitmq"
	} else if strings.Contains(pkgName, "github.com/nats-io/nats.go") {
		stack.QueueEngine = "nats"
	}

	if strings.Contains(pkgName, "github.com/spf13/viper") {
		stack.ConfigManager = "viper"
	} else if strings.Contains(pkgName, "github.com/kelseyhightower/envconfig") || strings.Contains(pkgName, "github.com/caarlos0/env") {
		stack.ConfigManager = "envconfig"
	} else if strings.Contains(pkgName, "github.com/joho/godotenv") && stack.ConfigManager == "none" {
		stack.ConfigManager = "godotenv"
	}

	if strings.Contains(pkgName, "google.golang.org/grpc") {
		stack.RPC = "grpc"
	}

	if strings.Contains(pkgName, "go.uber.org/zap") {
		stack.Logger = "zap"
	} else if strings.Contains(pkgName, "github.com/rs/zerolog") {
		stack.Logger = "zerolog"
	} else if strings.Contains(pkgName, "github.com/sirupsen/logrus") {
		stack.Logger = "logrus"
	}

	if strings.Contains(pkgName, "go-playground/validator") {
		stack.Validator = "validator.v10"
	}

	if strings.Contains(pkgName, "github.com/stretchr/testify") {
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
