package http

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	statusservice "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/service"
)

func TestRegisteredRoutesSnapshot(t *testing.T) {
	router := NewRouter(RouterConfig{SessionService: &statusservice.AgentSessionService{}})
	actual, err := json.MarshalIndent(router.RegisteredRoutes(), "", "  ")
	if err != nil {
		t.Fatalf("json.MarshalIndent() error = %v", err)
	}
	assertSnapshot(t, "routes.snapshot.json", append(actual, '\n'))
}

func TestRenderedIngressSnapshot(t *testing.T) {
	template := readRepoFile(t, "deploy/k8s/bot-service/ingress.yaml.tpl")
	replacements := map[string]string{
		"${MATTERCODEX_NAMESPACE}":              "mattermost",
		"${MATTERCODEX_CLUSTER_ISSUER}":         "letsencrypt",
		"${MATTERCODEX_INGRESS_CLASS}":          "traefik",
		"${MATTERCODEX_BOT_SERVICE_HOST}":       "bot.example.test",
		"${MATTERCODEX_BOT_SERVICE_TLS_SECRET}": "bot-service-tls",
	}
	for key, value := range replacements {
		template = strings.ReplaceAll(template, key, value)
	}
	if strings.Contains(template, "${") {
		t.Fatalf("в снимке Ingress остались неотрендеренные переменные")
	}
	assertSnapshot(t, "ingress.snapshot.yaml", []byte(template))
}

func assertSnapshot(t *testing.T, name string, actual []byte) {
	t.Helper()
	expected, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("os.ReadFile(%s) error = %v", name, err)
	}
	if string(actual) != string(expected) {
		t.Fatalf("снимок %s изменился:\n--- expected\n%s\n--- actual\n%s", name, expected, actual)
	}
}

func readRepoFile(t *testing.T, relative string) string {
	t.Helper()
	_, current, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller() не вернул путь")
	}
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(current), "../../../../../.."))
	content, err := os.ReadFile(filepath.Join(repoRoot, relative))
	if err != nil {
		t.Fatalf("os.ReadFile(%s) error = %v", relative, err)
	}
	return string(content)
}
