package httpserver

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// readyStub provides a constant successful snapshot without dependency I/O.
// readyStub предоставляет неизменную готовность без I/O к зависимостям.
type readyStub struct{}

// TestStandardProcessConfigPinsBudgets checks the shared preset field by field and verifies
// that New accepts it without opening a listener or contacting a dependency.
// RU: TestStandardProcessConfigPinsBudgets по полям проверяет общий профиль и подтверждает,
// что New принимает его без открытия listener и обращения к зависимости.
func TestStandardProcessConfigPinsBudgets(t *testing.T) {
	config := StandardProcessConfig("127.0.0.1:0")
	if config.Address != "127.0.0.1:0" || config.ReadHeaderTimeout != 2*time.Second ||
		config.ReadTimeout != 5*time.Second || config.WriteTimeout != 5*time.Second ||
		config.IdleTimeout != 30*time.Second || config.MaximumHeaderBytes != 32<<10 ||
		config.MaximumConnections != 128 {
		t.Fatalf("standard process budgets changed: %+v", config)
	}
	if _, err := New(config, readyStub{}, http.NotFoundHandler()); err != nil {
		t.Fatalf("standard process config rejected: %v", err)
	}
}

// Ready returns deterministic synthetic readiness for handler registration tests.
// Ready возвращает устойчивую синтетическую готовность для тестов регистрации.
func (readyStub) Ready() (bool, string) { return true, "ready" }

// TestExactGETRouteRejectsQueryBodyAndUnsafeRegistration covers the allowed GET,
// rejects query/body/non-GET inputs and prevents replacement of a standard route.
// TestExactGETRouteRejectsQueryBodyAndUnsafeRegistration проверяет допустимый GET,
// запрещает query/body/другой метод и подмену стандартного маршрута.
func TestExactGETRouteRejectsQueryBodyAndUnsafeRegistration(t *testing.T) {
	server, err := New(testConfig(), readyStub{}, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}), ExactGETRoute{
		Path: "/policy", ContentType: "application/json", Handler: http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
			writer.WriteHeader(http.StatusNoContent)
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		method string
		target string
		body   bool
		status int
	}{
		{http.MethodGet, "/policy", false, http.StatusNoContent},
		{http.MethodGet, "/policy?host=example.com", false, http.StatusBadRequest},
		{http.MethodGet, "/policy", true, http.StatusBadRequest},
		{http.MethodPost, "/policy", false, http.StatusMethodNotAllowed},
	} {
		request := httptest.NewRequest(test.method, test.target, nil)
		if test.body {
			request.ContentLength = 1
		}
		response := httptest.NewRecorder()
		server.server.Handler.ServeHTTP(response, request)
		if response.Code != test.status {
			t.Fatalf("%s %s: got %d, want %d", test.method, test.target, response.Code, test.status)
		}
	}
	if _, err := New(testConfig(), readyStub{}, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}), ExactGETRoute{
		Path: "/readyz", ContentType: "text/plain", Handler: http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}),
	}); err == nil {
		t.Fatal("expected standard route collision to fail")
	}
}

// testConfig uses a loopback ephemeral address and minimum accepted budgets so
// handler tests neither reserve a fixed port nor contact an external service.
// testConfig задаёт loopback со случайным портом и минимальные бюджеты, чтобы
// тесты handler не резервировали постоянный порт и не обращались наружу.
func testConfig() Config {
	return Config{
		Address: "127.0.0.1:0", ReadHeaderTimeout: time.Second, ReadTimeout: time.Second,
		WriteTimeout: time.Second, IdleTimeout: time.Second, MaximumHeaderBytes: 16 << 10,
		MaximumConnections: 16,
	}
}
