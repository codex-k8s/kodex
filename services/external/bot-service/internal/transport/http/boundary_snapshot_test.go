package http

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	statusservice "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/service"
	networkingv1 "k8s.io/api/networking/v1"
	"sigs.k8s.io/yaml"
)

func TestRegisteredRoutesSnapshot(t *testing.T) {
	router := NewRouter(RouterConfig{SessionService: &statusservice.AgentSessionService{}})
	actual, err := json.MarshalIndent(router.RegisteredRoutes(), "", "  ")
	if err != nil {
		t.Fatalf("json.MarshalIndent() error = %v", err)
	}
	assertSnapshot(t, "routes.snapshot.json", append(actual, '\n'))
}

func TestProductionIngressExactlyMatchesPublicRouteRegistry(t *testing.T) {
	values := map[string]string{
		"MATTERCODEX_NAMESPACE":              "synthetic-mattermost",
		"MATTERCODEX_CLUSTER_ISSUER":         "synthetic-issuer",
		"MATTERCODEX_INGRESS_CLASS":          "synthetic-ingress",
		"MATTERCODEX_BOT_SERVICE_HOST":       "bot.example.test",
		"MATTERCODEX_BOT_SERVICE_TLS_SECRET": "synthetic-tls",
	}
	template := readRepoFile(t, "deploy/k8s/bot-service/ingress.yaml.tpl")
	missing := map[string]bool{}
	rendered := os.Expand(template, func(key string) string {
		value, ok := values[key]
		if !ok {
			missing[key] = true
		}
		return value
	})
	if len(missing) > 0 || strings.Contains(rendered, "${") {
		t.Fatalf("production Ingress содержит неотрендеренные переменные: %#v", missing)
	}
	var ingress networkingv1.Ingress
	if err := yaml.UnmarshalStrict([]byte(rendered), &ingress); err != nil {
		t.Fatalf("разбор отрендерованного production Ingress: %v", err)
	}
	actual, err := productionIngressPaths(ingress)
	if err != nil {
		t.Fatal(err)
	}
	router := NewRouter(RouterConfig{SessionService: &statusservice.AgentSessionService{}})
	expected := make([]string, 0)
	for _, route := range router.RegisteredRoutes() {
		if route.Boundary == RouteBoundaryPublic {
			expected = append(expected, route.Path)
		}
	}
	sort.Strings(actual)
	sort.Strings(expected)
	if strings.Join(actual, "\n") != strings.Join(expected, "\n") {
		t.Fatalf("production Ingress paths не равны public registry: ingress=%#v registry=%#v", actual, expected)
	}
}

func TestProductionIngressValidatorRejectsEveryBoundaryEscape(t *testing.T) {
	exact := networkingv1.PathTypeExact
	prefix := networkingv1.PathTypePrefix
	valid := networkingv1.Ingress{
		Spec: networkingv1.IngressSpec{Rules: []networkingv1.IngressRule{{IngressRuleValue: networkingv1.IngressRuleValue{HTTP: &networkingv1.HTTPIngressRuleValue{Paths: []networkingv1.HTTPIngressPath{{
			Path: "/mattermost/slash/agents", PathType: &exact,
			Backend: networkingv1.IngressBackend{Service: &networkingv1.IngressServiceBackend{Name: "matter-codex-bot-service"}},
		}}}}}}},
	}
	tests := []struct {
		name   string
		mutate func(*networkingv1.Ingress)
	}{
		{name: "defaultBackend", mutate: func(ingress *networkingv1.Ingress) { ingress.Spec.DefaultBackend = &networkingv1.IngressBackend{} }},
		{name: "root", mutate: func(ingress *networkingv1.Ingress) { ingress.Spec.Rules[0].HTTP.Paths[0].Path = "/" }},
		{name: "non-Exact", mutate: func(ingress *networkingv1.Ingress) { ingress.Spec.Rules[0].HTTP.Paths[0].PathType = &prefix }},
		{name: "wrong Service", mutate: func(ingress *networkingv1.Ingress) {
			ingress.Spec.Rules[0].HTTP.Paths[0].Backend.Service.Name = "other-service"
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ingress := valid.DeepCopy()
			test.mutate(ingress)
			if _, err := productionIngressPaths(*ingress); err == nil {
				t.Fatal("productionIngressPaths() error = nil")
			}
		})
	}
}

func productionIngressPaths(ingress networkingv1.Ingress) ([]string, error) {
	if ingress.Spec.DefaultBackend != nil {
		return nil, fmt.Errorf("production Ingress не должен иметь defaultBackend")
	}
	actual := make([]string, 0)
	for _, rule := range ingress.Spec.Rules {
		if rule.HTTP == nil {
			return nil, fmt.Errorf("production Ingress содержит правило без HTTP paths")
		}
		for _, path := range rule.HTTP.Paths {
			if path.Path == "/" {
				return nil, fmt.Errorf("production Ingress публикует корневой маршрут")
			}
			if path.PathType == nil || (*path.PathType != networkingv1.PathTypeExact && !(path.Path == pathControlCenter && *path.PathType == networkingv1.PathTypePrefix)) {
				return nil, fmt.Errorf("маршрут %q имеет недопустимый pathType", path.Path)
			}
			if path.Backend.Service == nil || path.Backend.Service.Name != "matter-codex-bot-service" {
				return nil, fmt.Errorf("маршрут %q направлен не в matter-codex-bot-service", path.Path)
			}
			actual = append(actual, path.Path)
		}
	}
	return actual, nil
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
