package main

import (
	"os"
	"strings"
	"testing"
)

func TestProductionImageIncludesCodexCLI(t *testing.T) {
	raw, err := os.ReadFile("../../Dockerfile")
	if err != nil {
		t.Fatalf("ReadFile(Dockerfile) err = %v", err)
	}
	dockerfile := string(raw)
	required := []string{
		"ARG CODEX_CLI_VERSION=0.130.0",
		`npm install -g --omit=dev "@openai/codex@${CODEX_CLI_VERSION}"`,
		"COPY --from=codex-cli /usr/local/lib/node_modules/@openai/codex /usr/local/lib/node_modules/@openai/codex",
		"ln -s ../lib/node_modules/@openai/codex/bin/codex.js /usr/local/bin/codex",
		"/usr/local/bin/codex --version",
		"/usr/local/bin/codex exec --help >/dev/null",
	}
	for _, marker := range required {
		if !strings.Contains(dockerfile, marker) {
			t.Fatalf("Dockerfile is missing production Codex CLI marker %q", marker)
		}
	}
	if strings.Contains(dockerfile, "FROM scratch AS prod") {
		t.Fatal("production image cannot be scratch because codex exec requires Node and Codex CLI")
	}
}
