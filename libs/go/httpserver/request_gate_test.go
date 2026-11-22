package httpserver

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// TestRequestGateRequiresExplicitClosureAndRealJoin distinguishes idle/open, closed/active and actually joined states without an auxiliary wait goroutine.
// TestRequestGateRequiresExplicitClosureAndRealJoin различает idle/open, closed/active и actually joined states без auxiliary wait goroutine.
func TestRequestGateRequiresExplicitClosureAndRealJoin(t *testing.T) {
	started, release, returned := make(chan struct{}), make(chan struct{}), make(chan struct{})
	gate, err := NewRequestGate(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { close(started); <-release }), 1)
	if err != nil {
		t.Fatal(err)
	}
	if gate.Joined() || gate.Closed() || !errors.Is(gate.Wait(t.Context()), ErrRequestGateLifecycle) {
		t.Fatal("idle gate manufactured join")
	}
	go func() {
		defer close(returned)
		gate.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/private", nil))
	}()
	<-started
	_ = gate.Close()
	_ = gate.Close()
	if !gate.Closed() || gate.Joined() {
		t.Fatal("closing active handler manufactured join")
	}
	expired, cancel := context.WithCancel(t.Context())
	cancel()
	if !errors.Is(gate.Wait(expired), context.Canceled) {
		t.Fatal("timeout was credited as join")
	}
	rejected := httptest.NewRecorder()
	gate.ServeHTTP(rejected, httptest.NewRequest(http.MethodPost, "/secret-path", nil))
	if rejected.Code != http.StatusServiceUnavailable {
		t.Fatal("closed gate admitted another request")
	}
	close(release)
	<-returned
	if !gate.Joined() || gate.Wait(expired) != nil {
		t.Fatal("actual completed handler did not publish join")
	}
}

// TestRequestGateBoundsOnlyLocalConcurrency rejects the extra simultaneous request before domain work while allowing a later request after the first returns; it claims no iteration or remote-effect budget.
// TestRequestGateBoundsOnlyLocalConcurrency отклоняет extra simultaneous request до domain work, разрешая later request после возврата первого; iteration либо remote-effect budget не заявляется.
func TestRequestGateBoundsOnlyLocalConcurrency(t *testing.T) {
	started, release, returned := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	gate, _ := NewRequestGate(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		if calls.Add(1) == 1 {
			close(started)
			<-release
		}
	}), 1)
	go func() {
		defer close(returned)
		gate.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/one", nil))
	}()
	<-started
	denied := httptest.NewRecorder()
	gate.ServeHTTP(denied, httptest.NewRequest(http.MethodGet, "/two", nil))
	if denied.Code != http.StatusTooManyRequests || calls.Load() != 1 {
		t.Fatal("concurrency bound reached domain work")
	}
	close(release)
	<-returned
	gate.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/three", nil))
	if calls.Load() != 2 {
		t.Fatal("completed local slot was not released")
	}
	_ = gate.Close()
	if gate.Wait(t.Context()) != nil {
		t.Fatal("idle closed gate failed join")
	}
}

// TestRequestGateReleasesAfterPanic proves panic unwinding cannot strand a local admission slot or falsely close an active request graph.
// TestRequestGateReleasesAfterPanic доказывает, что panic unwinding не удерживает local admission slot и не закрывает active request graph ошибочно.
func TestRequestGateReleasesAfterPanic(t *testing.T) {
	gate, _ := NewRequestGate(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("synthetic handler failure") }), 1)
	func() {
		defer func() { _ = recover() }()
		gate.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	}()
	_ = gate.Close()
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if gate.Wait(ctx) != nil || !gate.Joined() {
		t.Fatal("panic stranded an owned handler")
	}
}

// TestRequestGateRejectsInvalidConstruction avoids a zero/unbounded concurrency policy and an unavailable wait context.
// TestRequestGateRejectsInvalidConstruction запрещает zero/unbounded concurrency policy и недоступный wait context.
func TestRequestGateRejectsInvalidConstruction(t *testing.T) {
	for _, limit := range []int{-1, 0, 129} {
		if _, err := NewRequestGate(http.NotFoundHandler(), limit); !errors.Is(err, ErrRequestGateLifecycle) {
			t.Fatal("invalid request bound accepted")
		}
	}
	if _, err := NewRequestGate(nil, 8); !errors.Is(err, ErrRequestGateLifecycle) {
		t.Fatal("nil handler accepted")
	}
	gate, _ := NewRequestGate(http.NotFoundHandler(), 8)
	_ = gate.Close()
	if !errors.Is(gate.Wait(nil), ErrRequestGateLifecycle) {
		t.Fatal("nil context accepted")
	}
}
