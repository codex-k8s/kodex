package codexappserver

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestMaskAccountReturnsOnlyBoundedMetadata(t *testing.T) {
	t.Parallel()

	if got := maskAccount("owner@example.test"); got != "o***@example.test" {
		t.Fatalf("unexpected masked account: %q", got)
	}
	for _, invalid := range []string{"", "owner", "@example.test"} {
		if got := maskAccount(invalid); got != "configured" {
			t.Fatalf("invalid account was reflected: %q", got)
		}
	}
}

func TestBoundedWriterDoesNotRetainProviderDiagnostic(t *testing.T) {
	t.Parallel()

	var target bytes.Buffer
	writer := &boundedWriter{target: &target, remaining: 8}
	raw := []byte("token=super-secret-provider-value")
	count, err := writer.Write(raw)
	if err != nil || count != len(raw) || target.Len() != 8 {
		t.Fatalf("bounded writer result is invalid: %d %d %v", count, target.Len(), err)
	}
	if strings.Contains(target.String(), "super-secret-provider-value") {
		t.Fatal("provider diagnostic exceeded the bounded buffer")
	}
}

func TestParseCapacityObservationUsesMostRestrictiveRealWindow(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)
	raw := []byte(`{"rateLimits":{"primary":{"usedPercent":25,"windowDurationMins":300,"resetsAt":1786201200},"secondary":{"usedPercent":80,"windowDurationMins":10080,"resetsAt":1786806000},"rateLimitReachedType":null}}`)
	observation, err := parseCapacityObservation(raw, now)
	if err != nil {
		t.Fatal(err)
	}
	if observation.Usage != 80 || observation.Limit != 100 || observation.WindowSeconds != 10080*60 ||
		observation.Revision != uint64(now.UnixMicro()) || !observation.ObservedAt.Equal(now) ||
		!observation.ExpiresAt.Equal(now.Add(5*time.Minute)) {
		t.Fatalf("unexpected bounded provider observation: %#v", observation)
	}
}

func TestParseCapacityObservationFailsClosedWithoutFreshProviderWindow(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)
	for name, raw := range map[string]string{
		"missing":   `{"rateLimits":{"primary":null}}`,
		"invalid":   `{"rateLimits":{"primary":{"usedPercent":101,"windowDurationMins":300,"resetsAt":1786201200}}}`,
		"expired":   `{"rateLimits":{"primary":{"usedPercent":25,"windowDurationMins":300,"resetsAt":1786186800}}}`,
		"malformed": `{`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := parseCapacityObservation([]byte(raw), now); err == nil {
				t.Fatal("invalid provider capacity became eligible")
			}
		})
	}
}

func TestCodexEnvironmentKeepsExternalHTTPSBehindPlatformGateway(t *testing.T) {
	t.Parallel()

	client := &Client{config: Config{
		Executable: "/usr/local/bin/codex", SSLCertificateFile: "/etc/ssl/certs/ca-certificates.crt",
		HTTPSProxy: "http://egress-gateway.mattercodex-system.svc.cluster.local:8080",
		NoProxy:    "localhost,127.0.0.1,::1,.svc,.svc.cluster.local",
	}}
	environment := strings.Join(client.environment("/tmp/codex"), "\n")
	for _, required := range []string{
		"HTTPS_PROXY=http://egress-gateway.mattercodex-system.svc.cluster.local:8080",
		"HTTP_PROXY=http://egress-gateway.mattercodex-system.svc.cluster.local:8080",
		"NO_PROXY=localhost,127.0.0.1,::1,.svc,.svc.cluster.local",
	} {
		if !strings.Contains(environment, required) {
			t.Fatalf("Codex environment missed platform egress binding %q", required)
		}
	}
}
