package controlplaneclient

import (
	"context"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

func TestTrustedClusterDialHasNoCertificateSocketOrProofDependency(t *testing.T) {
	config := Config{RPCProfile: "trusted-cluster", CallerWorkload: "control-api-gateway",
		Target: "dns:///control-plane.kodex-system.svc:8443", DialTimeout: time.Second,
		Operations: map[string]string{"example.read": "/example.v1.Service/Read"}}
	client, err := Dial(t.Context(), config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	if client.issuer != nil || client.raw != nil || client.resolver != nil || !client.trustedCluster {
		t.Fatal("trusted client retains authority dependency")
	}
	if _, _, err := client.AuthorityProof(t.Context(), "example.read", "/example.v1.Service/Read"); err == nil {
		t.Fatal("trusted client manufactured a signed proof")
	}
	for _, mutate := range []func(*Config){
		func(c *Config) { c.RPCProfile = "insecure" },
		func(c *Config) { c.Target = "example.com:8443" },
		func(c *Config) { c.Target = "127.0.0.1:8443" },
		func(c *Config) { c.Target = "control-plane.foreign.svc:8443" },
		func(c *Config) { c.CallerWorkload = "" },
		func(c *Config) { c.CallerWorkload = "../owner" },
	} {
		invalid := config
		mutate(&invalid)
		if unexpected, err := Dial(t.Context(), invalid); err == nil {
			_ = unexpected.Close()
			t.Fatal("invalid trusted configuration accepted")
		}
	}
}

func TestTrustedClusterMetadataUsesConfiguredCallerAndExplicitUserCredential(t *testing.T) {
	const method = "/example.v1.Service/Read"
	const caller = "spiffe://kodex.local/ns/kodex-system/sa/control-api-gateway"
	ctx := metadata.NewOutgoingContext(t.Context(), metadata.Pairs(
		"authorization", "stale", "x-kodex-authorization", "legacy",
		"x-kodex-trusted-caller", "foreign", "x-kodex-project-ref", "foreign"))
	ctx, err := WithApplicationGrant(ctx, "synthetic-user-credential")
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	err = serviceUnary(operationSet{method: "example.read"}, nil, "trusted-cluster", caller)(ctx, method, nil, nil, nil,
		func(ctx context.Context, _ string, _, _ any, _ *grpc.ClientConn, _ ...grpc.CallOption) error {
			calls++
			md, _ := metadata.FromOutgoingContext(ctx)
			if len(md.Get("x-kodex-authorization")) != 0 || len(md.Get("x-kodex-project-ref")) != 0 ||
				len(md.Get("x-kodex-trusted-caller")) != 1 || md.Get("x-kodex-trusted-caller")[0] != caller ||
				md.Get("x-kodex-rpc-profile")[0] != "trusted-cluster" || md.Get("authorization")[0] != "Bearer synthetic-user-credential" {
				t.Fatal("trusted request metadata was not replaced")
			}
			return nil
		})
	if err != nil || calls != 1 {
		t.Fatal("trusted request failed")
	}
}
