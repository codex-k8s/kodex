package mailpolicy

import (
	"bytes"
	"context"
	"encoding/json"
	api "github.com/codex-k8s/kodex/libs/go/emailbridgeapi"
	"net/netip"
	"os"
	"strings"
	"testing"
	"time"
)

type resolverFixture struct {
	snapshot Snapshot
	calls    int
}

func (resolver *resolverFixture) Resolve(context.Context, string) (Snapshot, error) {
	resolver.calls++
	return resolver.snapshot, nil
}

func TestTypedProducerKeepsSourceIdentityAndExactRender(t *testing.T) {
	raw, err := os.ReadFile("../../../contracts/email-bridge/v1/examples/mailboxes.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var configuration api.Configuration
	if err := api.Decode(raw, &configuration); err != nil {
		t.Fatal(err)
	}
	resolver := &resolverFixture{snapshot: Snapshot{Addresses: []netip.Addr{netip.MustParseAddr("8.8.8.8")}, ExpiresAt: time.Now().Add(time.Minute)}}
	digest := strings.Repeat("a", 64)
	document, err := Produce(t.Context(), configuration, digest, resolver)
	if err != nil || resolver.calls == 0 || document.ConfigurationDigest != api.Digest(configuration) || document.ConfigurationRevision != configuration.Revision || document.GatewayPolicyDigest != digest {
		t.Fatal("typed producer lost source identity")
	}
	files, err := RenderFiles(document)
	if err != nil || len(files) != 3 {
		t.Fatal("typed producer did not render complete projection")
	}
	var object struct {
		Metadata  struct{ Name string }
		Immutable bool
		Data      map[string]string
	}
	if json.Unmarshal(files["mail-configmap.json"], &object) != nil || !object.Immutable || object.Metadata.Name != "egress-gateway-mail-"+document.Digest()[:24] {
		t.Fatal("projection is not immutable and content-addressed")
	}
	if bytes.Contains(files["mail-configmap.json"], []byte("credential")) || bytes.Contains(files["mail-configmap.json"], []byte("mailbox_id")) {
		t.Fatal("projection copied private mailbox descriptors")
	}
	var readback MailDocument
	if json.Unmarshal([]byte(object.Data["mail-policy.json"]), &readback) != nil || readback.Validate() != nil || readback.Digest() != document.Digest() {
		t.Fatal("render changed wire contract")
	}
	resolver.snapshot.ExpiresAt = time.Now().Add(-time.Second)
	if _, err := Produce(t.Context(), configuration, digest, resolver); err == nil {
		t.Fatal("expired resolver snapshot accepted")
	}
	resolver.snapshot.ExpiresAt = time.Now().Add(time.Minute)
	resolver.snapshot.Addresses = append(resolver.snapshot.Addresses, netip.MustParseAddr("127.0.0.1"))
	if _, err := Produce(t.Context(), configuration, digest, resolver); err == nil {
		t.Fatal("mixed public/private snapshot accepted")
	}
	if _, err := Produce(t.Context(), configuration, "caller-value", resolver); err == nil {
		t.Fatal("invalid gateway digest accepted")
	}
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	before := resolver.calls
	if _, err := Produce(cancelled, configuration, digest, resolver); err == nil || resolver.calls != before {
		t.Fatal("cancelled producer performed DNS work")
	}
}

func TestDisabledMailboxDoesNotKeepDNSOrNetworkAuthority(t *testing.T) {
	raw, err := os.ReadFile("../../../contracts/email-bridge/v1/examples/mailboxes.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var configuration api.Configuration
	if err := api.Decode(raw, &configuration); err != nil {
		t.Fatal(err)
	}
	resolver := &resolverFixture{snapshot: Snapshot{Addresses: []netip.Addr{netip.MustParseAddr("8.8.8.8")}, ExpiresAt: time.Now().Add(time.Minute)}}
	gatewayDigest := strings.Repeat("a", 64)
	active, err := Produce(t.Context(), configuration, gatewayDigest, resolver)
	if err != nil {
		t.Fatal(err)
	}
	configuration.Mailboxes[0].Enabled = false
	configuration.Revision++
	resolver.calls = 0
	disabled, err := Produce(t.Context(), configuration, gatewayDigest, resolver)
	if err != nil || len(disabled.Destinations) != 0 || resolver.calls != 0 || disabled.Digest() == active.Digest() ||
		disabled.ConfigurationDigest != api.Digest(configuration) || disabled.ConfigurationRevision != configuration.Revision {
		t.Fatal("выключенный mailbox сохранил DNS или network authority")
	}
	files, err := RenderFiles(disabled)
	if err != nil {
		t.Fatal(err)
	}
	var policy struct {
		Spec struct{ Egress []json.RawMessage }
	}
	if json.Unmarshal(files["mail-networkpolicy.json"], &policy) != nil || len(policy.Spec.Egress) != 0 {
		t.Fatal("render сохранил egress выключенного mailbox")
	}
	// Другой активный mailbox с тем же endpoint сохраняет только свой допуск.
	enabled := configuration.Mailboxes[0]
	enabled.Id = "active-mailbox"
	enabled.ConnectionId = "active-connection"
	enabled.Enabled = true
	configuration.Mailboxes = append(configuration.Mailboxes, enabled)
	mixed, err := Produce(t.Context(), configuration, gatewayDigest, resolver)
	if err != nil || len(mixed.Destinations) != len(active.Destinations) || resolver.calls != len(active.Destinations) {
		t.Fatal("выключенный mailbox изменил допуск активного endpoint")
	}
}
