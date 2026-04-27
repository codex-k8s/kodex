.PHONY: help lint lint-go dupl-go test-go test-go-migrations fmt-go gen-openapi-go gen-openapi-ts gen-openapi gen-proto-go validate-asyncapi

help:
	@echo "Targets:"
	@echo "  make lint-go   - golangci-lint for active Go packages, excluding deprecated/**"
	@echo "  make dupl-go   - fail on duplicated Go code (dupl -t 50)"
	@echo "  make test-go   - go test for active Go packages, excluding deprecated/**"
	@echo "  make test-go-migrations - run migration guard tests for active service goose files"
	@echo "  make fmt-go    - gofmt -w on tracked .go files"
	@echo "  make gen-openapi-go [SVC=services/external/api-gateway] - generate Go transport code from OpenAPI"
	@echo "  make gen-openapi-ts [APP=services/staff/web-console] - generate TS API client from OpenAPI"
	@echo "  make gen-openapi - run Go+TS OpenAPI generators for default services"
	@echo "  make gen-proto-go - generate Go gRPC contracts from active proto/**/*.proto"
	@echo "  make validate-asyncapi [SVC=access-manager|SPEC=specs/asyncapi/access-manager.v1.yaml] - validate AsyncAPI contract"
	@echo "  make lint      - run all linters"

lint: lint-go dupl-go

lint-go:
	@packages="$$(for root in services libs cmd; do \
		if [ -d "$$root" ]; then printf './%s/... ' "$$root"; fi; \
	done)"; \
	if [ -z "$$packages" ]; then \
		echo "lint-go: no active Go packages"; \
		exit 0; \
	fi; \
	golangci-lint run $$packages

dupl-go:
	@tmp="$$(mktemp)"; \
	filtered="$$(mktemp)"; \
	candidates="$$(mktemp)"; \
	roots="$$(for root in services libs; do if [ -d "$$root" ]; then printf '%s\n' "$$root"; fi; done)"; \
	if [ -n "$$roots" ]; then \
		printf '%s\n' "$$roots" | xargs rg --files -g '*.go' -g '!**/*_test.go' -g '!**/generated/**' > "$$candidates"; \
	fi; \
	if [ ! -s "$$candidates" ]; then \
		rm -f "$$tmp" "$$filtered" "$$candidates"; \
		exit 0; \
	fi; \
	dupl -t 50 -plumbing -files < "$$candidates" > "$$tmp"; \
	if [ -f tools/lint/dupl-baseline.txt ]; then \
		grep -F -x -v -f tools/lint/dupl-baseline.txt "$$tmp" > "$$filtered" || true; \
	else \
		cp "$$tmp" "$$filtered"; \
	fi; \
	if [ -s "$$filtered" ]; then \
		cat "$$filtered"; \
		echo "dupl-go: duplicates found (threshold=50)"; \
		rm -f "$$tmp" "$$filtered" "$$candidates"; \
		exit 1; \
	fi; \
	rm -f "$$tmp" "$$filtered" "$$candidates"

test-go:
	@packages="$$(for root in services libs cmd proto/gen/go; do \
		if [ -d "$$root" ]; then go list ./$$root/...; fi; \
	done)"; \
	if [ -z "$$packages" ]; then \
		echo "test-go: no active Go packages"; \
		exit 0; \
	fi; \
	go test $$packages

test-go-migrations:
	@packages="$$(go list ./services/... 2>/dev/null | grep '/cmd/cli/migrations' || true)"; \
	if [ -z "$$packages" ]; then \
		echo "test-go-migrations: no active migration packages"; \
		exit 0; \
	fi; \
	go test $$packages

fmt-go:
	@files="$$(git ls-files '*.go'; git ls-files --others --exclude-standard '*.go')"; \
	files="$$(printf '%s\n' "$$files" | grep -v '^deprecated/' || true)"; \
	if [ -z "$$files" ]; then \
		echo "fmt-go: no active Go files"; \
		exit 0; \
	fi; \
	printf '%s\n' "$$files" | xargs -r gofmt -w

gen-openapi-go:
	@svc="$${SVC:-services/external/api-gateway}"; \
	spec="$$svc/api/server/api.yaml"; \
	cfg="tools/codegen/openapi/$$(basename "$$svc").oapi-codegen.yaml"; \
	out="$$svc/internal/transport/http/generated/openapi.gen.go"; \
	case "$$svc" in \
		services/external/api-gateway|services/external/telegram-interaction-adapter) ;; \
		*) echo "gen-openapi-go: unsupported SVC=$$svc"; exit 1 ;; \
	esac; \
	test -f "$$spec"; \
	test -f "$$cfg"; \
	mkdir -p "$$(dirname "$$out")"; \
	go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@v2.5.0 -config "$$cfg" "$$spec" > "$$out"

gen-openapi-ts:
	@app="$${APP:-services/staff/web-console}"; \
	if [ "$$app" != "services/staff/web-console" ]; then \
		echo "gen-openapi-ts: unsupported APP=$$app (currently only services/staff/web-console)"; \
		exit 1; \
	fi; \
	npm --prefix "$$app" run gen:openapi

gen-openapi:
	@$(MAKE) gen-openapi-go SVC=services/external/api-gateway
	@$(MAKE) gen-openapi-go SVC=services/external/telegram-interaction-adapter
	@$(MAKE) gen-openapi-ts

gen-proto-go:
	@mkdir -p proto/gen/go; \
	protos="$$(find proto -name '*.proto' -print 2>/dev/null | sort)"; \
	if [ -z "$$protos" ]; then \
		echo "gen-proto-go: no proto files"; \
		exit 0; \
	fi; \
	protoc -I proto \
		--go_out=proto/gen/go --go_opt=paths=source_relative \
		--go-grpc_out=proto/gen/go --go-grpc_opt=paths=source_relative \
		$$protos

validate-asyncapi:
	@spec="$${SPEC:-}"; \
	if [ -z "$$spec" ]; then \
		svc="$${SVC:-access-manager}"; \
		spec="specs/asyncapi/$$svc.v1.yaml"; \
	fi; \
	test -f "$$spec"; \
	npx --yes @asyncapi/cli validate "$$spec"
