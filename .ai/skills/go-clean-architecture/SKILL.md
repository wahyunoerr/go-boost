# Skill: Go Clean Architecture Pattern

Use this skill when developing or refactoring features in projects following Clean / Hexagonal Architecture in Go.

## Architectural Layers (Dependency Rule points inwards)
1. **Domain / Entity Layer** (\x60internal/domain\x60):
   - Pure business models and entities without external framework dependencies.
   - Domain errors and repository interface contracts.
2. **Repository Layer** (\x60internal/repository\x60):
   - Implements domain repository interfaces.
   - Manages database interactions (GORM, SQLX, Bun, standard sql).
   - Translates database records into domain entities.
3. **Usecase / Service Layer** (\x60internal/usecase\x60 or \x60internal/service\x60):
   - Business workflow orchestration and transaction management.
   - Independent of transport protocols (HTTP/gRPC/CLI).
4. **Delivery / Handler Layer** (\x60internal/handler\x60 or \x60internal/delivery\x60):
   - HTTP request parsing, DTO binding, and response serialization.
   - Calls usecases with \x60ctx\x60.
