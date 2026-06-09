.PHONY: test-go tidy-go

test-go:
	go test ./...

tidy-go:
	go mod tidy
