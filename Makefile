.PHONY: test-go test-go-postgres test-go-all test-render-evidence tidy-go

test-go:
	go test ./...

test-go-postgres:
	@go run ./services/external/bot-service/cmd/postgres-test-target -- go test ./services/external/bot-service/internal/repository/postgres/... ./services/external/bot-service/internal/domain/service -count=1

test-go-all: test-go test-go-postgres

test-render-evidence:
	go test ./services/external/bot-service/internal/app -run TestBotServiceRenderCountsNonEmptyObjects -count=1

tidy-go:
	go mod tidy
