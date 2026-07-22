GOVULNCHECK_VERSION := v1.6.0

.PHONY: test-go test-go-postgres test-go-all test-render-evidence tidy-go govulncheck gen-openapi gen-openapi-go gen-openapi-ts

test-go:
	env -u GOFLAGS GOENV=off GOWORK=off go test -tags= ./...

test-go-postgres:
	@env -u GOFLAGS GOENV=off GOWORK=off go run ./services/external/bot-service/cmd/postgres-test-target --majors 15,16 -- go test -tags=postgres ./services/external/bot-service/internal/repository/postgres/... ./services/external/bot-service/internal/domain/service ./services/external/bot-service/internal/transport/http ./services/external/bot-service/internal/app -count=1

test-go-all:
	@$(MAKE) test-go
	@$(MAKE) test-go-postgres

test-render-evidence:
	go test ./services/external/bot-service/internal/app -run TestBotServiceRenderCountsNonEmptyObjects -count=1

tidy-go:
	go mod tidy

govulncheck:
	env -u GOFLAGS GOENV=off GOWORK=off go run golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION) ./...

gen-openapi: gen-openapi-go gen-openapi-ts

gen-openapi-go:
	oapi-codegen -config tools/codegen/openapi/control-center-go.yaml specs/openapi/control-center.v1.yaml

gen-openapi-ts:
	openapi-ts -f tools/codegen/openapi/control-center-ts.config.mjs
