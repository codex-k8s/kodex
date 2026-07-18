.PHONY: test-go test-go-postgres test-go-all test-render-evidence tidy-go

test-go:
	go test ./...

test-go-postgres:
	@test -n "$$MATTERCODEX_BOT_SERVICE_TEST_DATABASE_DSN" || (echo "MATTERCODEX_BOT_SERVICE_TEST_DATABASE_DSN обязателен для PostgreSQL-проверок" >&2; exit 1)
	MATTERCODEX_POSTGRES_TEST_REQUIRED=1 go test ./services/external/bot-service/internal/repository/postgres/... ./services/external/bot-service/internal/domain/service -count=1

test-go-all: test-go test-go-postgres

test-render-evidence:
	go test ./services/external/bot-service/internal/app -run TestBotServiceRenderCountsNonEmptyObjects -count=1

tidy-go:
	go mod tidy
