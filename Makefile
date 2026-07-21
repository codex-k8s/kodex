GOVULNCHECK_VERSION := v1.6.0

.PHONY: test-go test-go-postgres test-go-all test-render-evidence tidy-go govulncheck

test-go:
	env -u GOFLAGS GOENV=off GOWORK=off go test -tags= ./...

test-go-postgres:
	@env -u GOFLAGS GOENV=off GOWORK=off go run ./services/external/bot-service/cmd/postgres-test-target --majors 15,16 -- go test -tags=postgres ./services/external/bot-service/internal/repository/postgres/... ./services/external/bot-service/internal/domain/service -count=1

test-go-all:
	@$(MAKE) test-go
	@$(MAKE) test-go-postgres

test-render-evidence:
	go test ./services/external/bot-service/internal/app -run TestBotServiceRenderCountsNonEmptyObjects -count=1

tidy-go:
	go mod tidy

govulncheck:
	env -u GOFLAGS GOENV=off GOWORK=off go run golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION) ./...
