GOVULNCHECK_VERSION := v1.6.0
GO_MIN_VERSION := 1.26.5
GO_TOOLCHAIN := go1.26.5

.PHONY: check-go-toolchain test-go-toolchain-contract test-go test-go-postgres test-go-all test-render-evidence tidy-go govulncheck

check-go-toolchain:
	@./scripts/check-go-toolchain.sh

test-go-toolchain-contract: check-go-toolchain
	@./scripts/tests/go-toolchain-contract-test.sh

test-go: test-go-toolchain-contract
	env -u GOFLAGS GOENV=off GOWORK=off go test -tags= ./...

test-go-postgres: check-go-toolchain
	@env -u GOFLAGS GOENV=off GOWORK=off go run ./services/external/bot-service/cmd/postgres-test-target --majors 15,16 -- go test -tags=postgres ./services/external/bot-service/internal/repository/postgres/... ./services/external/bot-service/internal/domain/service ./services/external/bot-service/internal/transport/http ./services/external/bot-service/internal/app -count=1

test-go-all:
	@$(MAKE) test-go
	@$(MAKE) test-go-postgres

test-render-evidence: check-go-toolchain
	go test ./services/external/bot-service/internal/app -run TestBotServiceRenderCountsNonEmptyObjects -count=1

tidy-go: check-go-toolchain
	go mod tidy

govulncheck: check-go-toolchain
	$(if $(filter file,$(origin GOVULNCHECK_VERSION)),,$(error GOVULNCHECK_VERSION нельзя переопределять))
	@printf 'Проверенный Go toolchain: %s\n' "$$(env -u GOFLAGS GOENV=off GOWORK=off go env GOVERSION)"
	env -u GOFLAGS GOENV=off GOWORK=off go run 'golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION)' -mode=source -scan=symbol -show=traces,version ./...
