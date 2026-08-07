package runtime

import (
	"context"
	"net"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/matter-codex/services/external/egress-gateway/internal/dnsresolver"
	"github.com/codex-k8s/matter-codex/services/external/egress-gateway/internal/observability"
	"github.com/codex-k8s/matter-codex/services/external/egress-gateway/internal/policy"
	"github.com/miekg/dns"
)

func TestReadinessAndReadbackReflectEffectivePolicyAndResolver(t *testing.T) {
	root := filepath.Join("..", "..", "..", "..", "..")
	value, err := os.ReadFile(filepath.Join(root, "deploy", "k8s", "base", "egress-gateway", "policy.json"))
	if err != nil {
		t.Fatal(err)
	}
	active, err := policy.Load(value, "2026-08-07.1", "5c71fefd60e624d6891e857442302c2b119f21b76b474d3c34f1c6df330f62ae")
	if err != nil {
		t.Fatal(err)
	}
	resolver, err := dnsresolver.New(active.DNS(), []netip.AddrPort{netip.MustParseAddrPort("127.0.0.53:53")}, readbackExchanger{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	metrics := observability.NewMetrics()
	state := newState(active, metrics)
	if state.ready() {
		t.Fatal("BOOTING state must not be ready")
	}
	if _, err := resolver.Resolve(context.Background(), "api.openai.com"); err != nil {
		t.Fatal(err)
	}
	state.setResolverReady(true)
	state.setProcess(processReady)
	if !state.ready() {
		t.Fatal("ACTIVE policy and validated resolver must be ready")
	}
	server := newTechnicalServer("unused", state, metrics)
	request := httptest.NewRequest("GET", "/policy", nil)
	response := httptest.NewRecorder()
	server.server.Handler.ServeHTTP(response, request)
	if response.Code != 200 || !strings.Contains(response.Body.String(), `"policyState":"ACTIVE"`) ||
		!strings.Contains(response.Body.String(), `"resolverState":"VALIDATED"`) ||
		!strings.Contains(response.Body.String(), active.Digest()) {
		t.Fatalf("unexpected safe readback: %d %s", response.Code, response.Body.String())
	}
	state.setProcess(processDraining)
	if state.ready() {
		t.Fatal("DRAINING state must become not-ready before shutdown")
	}
}

func TestInvalidPolicyPublishesNotReadyReadbackWithoutLoadedIdentity(t *testing.T) {
	metrics := observability.NewMetrics()
	state := newInvalidPolicyState(metrics)
	server := newTechnicalServer("unused", state, metrics)
	readyRequest := httptest.NewRequest("GET", "/readyz", nil)
	readyResponse := httptest.NewRecorder()
	server.server.Handler.ServeHTTP(readyResponse, readyRequest)
	if readyResponse.Code != 503 {
		t.Fatalf("invalid policy readiness must fail: %d", readyResponse.Code)
	}
	readbackRequest := httptest.NewRequest("GET", "/policy", nil)
	readbackResponse := httptest.NewRecorder()
	server.server.Handler.ServeHTTP(readbackResponse, readbackRequest)
	body := readbackResponse.Body.String()
	if readbackResponse.Code != 200 || !strings.Contains(body, `"policyState":"INVALID"`) ||
		!strings.Contains(body, `"revision":""`) || !strings.Contains(body, `"digest":""`) {
		t.Fatalf("unexpected invalid-policy readback: %d %s", readbackResponse.Code, body)
	}
	queryRequest := httptest.NewRequest("GET", "/policy?hostname=api.openai.com", nil)
	queryResponse := httptest.NewRecorder()
	server.server.Handler.ServeHTTP(queryResponse, queryRequest)
	if queryResponse.Code != 400 {
		t.Fatalf("policy readback must reject caller query parameters: %d", queryResponse.Code)
	}
}

func TestInvalidPolicyRuntimeCancelsAndJoinsWithoutConnectListener(t *testing.T) {
	lifecycle, cancel := context.WithCancel(context.Background())
	cancel()
	done := make(chan error, 1)
	go func() {
		done <- RunInvalidPolicy(lifecycle, context.Background(), Config{TechnicalAddress: "127.0.0.1:0"})
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("invalid-policy technical runtime did not cancel and join")
	}
}

type readbackExchanger struct{}

func (readbackExchanger) Exchange(_ context.Context, request *dns.Msg, _ netip.AddrPort, _ string) (*dns.Msg, error) {
	response := new(dns.Msg)
	response.SetReply(request)
	if request.Question[0].Qtype == dns.TypeA {
		response.Answer = []dns.RR{&dns.A{Hdr: dns.RR_Header{Name: request.Question[0].Name, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 60}, A: net.ParseIP("93.184.216.34").To4()}}
	}
	return response, nil
}
