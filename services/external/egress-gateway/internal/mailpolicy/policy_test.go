package mailpolicy

import (
	"context"
	"encoding/json"
	"net/netip"
	"os"
	"strings"
	"testing"
	"time"

	api "github.com/codex-k8s/kodex/libs/go/emailbridgeapi"
	"github.com/codex-k8s/kodex/services/external/egress-gateway/internal/dnsresolver"
	"github.com/codex-k8s/kodex/services/external/egress-gateway/internal/policy"
)

type fixtureResolver struct {
	snapshot dnsresolver.Snapshot
	calls    int
}

func (r *fixtureResolver) Resolve(context.Context, string) (dnsresolver.Snapshot, error) {
	r.calls++
	return r.snapshot, nil
}

func fixtureBase(t *testing.T) *policy.Active {
	t.Helper()
	path := "../../../../../deploy/k8s/base/egress-gateway/policy.json"
	digest, err := policy.DigestFile(path)
	if err != nil {
		t.Fatal(err)
	}
	active, err := policy.LoadFile(path, "2026-09-05.1", digest)
	if err != nil {
		t.Fatal(err)
	}
	return active
}

func fixtureDocument(t *testing.T) MailDocument {
	t.Helper()
	return MailDocument{Schema: MailSchema, ConfigurationRevision: 1, ConfigurationDigest: strings.Repeat("a", 64), GatewayPolicyDigest: fixtureBase(t).Digest(), Destinations: []MailDestination{{Hostname: "mail.example.test", Port: 587, Protocol: "smtp", TLSMode: "starttls", Addresses: []string{"8.8.8.8"}}}}
}

func TestClosedMailTransportRegistry(t *testing.T) {
	for _, p := range []struct {
		name            string
		implicit, start int
	}{{"smtp", 465, 587}, {"pop3", 995, 110}, {"imap", 993, 143}} {
		if !MailEndpointValid(p.name, p.implicit, "implicit") || !MailEndpointValid(p.name, p.start, "starttls") {
			t.Fatal("registered endpoint rejected")
		}
		for _, port := range []int{25, 443, 8080, 8081, 8082, p.implicit, p.start} {
			if MailEndpointValid(p.name, port, "plaintext") || MailEndpointValid("https", port, "implicit") {
				t.Fatal("unregistered protocol accepted")
			}
		}
		if MailEndpointValid(p.name, p.implicit, "starttls") || MailEndpointValid(p.name, p.start, "implicit") {
			t.Fatal("transport mode substituted")
		}
	}
}

func TestMailPolicyStrictLoading(t *testing.T) {
	base := fixtureBase(t)
	doc := fixtureDocument(t)
	raw, _ := json.Marshal(doc)
	active, err := LoadMail(raw, doc.Digest(), base)
	if err != nil || !active.Allows("mail.example.test", 587) || active.Allows("mail.example.test", 443) {
		t.Fatal("exact policy unavailable", err)
	}
	for name, mutate := range map[string]func(*MailDocument){
		"foreign-source": func(d *MailDocument) { d.GatewayPolicyDigest = strings.Repeat("b", 64) },
		"private":        func(d *MailDocument) { d.Destinations[0].Addresses = []string{"10.0.0.1"} },
		"mixed":          func(d *MailDocument) { d.Destinations[0].Addresses = []string{"8.8.8.8", "127.0.0.1"} },
		"wildcard":       func(d *MailDocument) { d.Destinations[0].Hostname = "*.example.test" },
		"ip-host":        func(d *MailDocument) { d.Destinations[0].Hostname = "8.8.8.8" },
		"mode":           func(d *MailDocument) { d.Destinations[0].TLSMode = "plaintext" },
		"port":           func(d *MailDocument) { d.Destinations[0].Port = 25 },
		"duplicate":      func(d *MailDocument) { d.Destinations = append(d.Destinations, d.Destinations[0]) },
	} {
		t.Run(name, func(t *testing.T) {
			d := fixtureDocument(t)
			mutate(&d)
			b, _ := json.Marshal(d)
			if _, e := LoadMail(b, d.Digest(), base); e == nil {
				t.Fatal("invalid policy accepted")
			}
		})
	}
	for _, b := range [][]byte{append(raw, raw...), []byte(`{"schema":"a","schema":"b"}`), append([]byte(`{"extra":true,`), raw[1:]...)} {
		if _, e := LoadMail(b, doc.Digest(), base); e == nil {
			t.Fatal("invalid JSON accepted")
		}
	}
	if _, e := LoadMail(raw, strings.Repeat("c", 64), base); e == nil {
		t.Fatal("digest mismatch accepted")
	}
}

func TestProducerBindsRealMailboxSourceAndRejectsStaleDNS(t *testing.T) {
	raw, err := os.ReadFile("../../../../../contracts/email-bridge/v1/examples/mailboxes.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var source api.Configuration
	if err := api.Decode(raw, &source); err != nil {
		t.Fatal(err)
	}
	resolver := &fixtureResolver{snapshot: dnsresolver.Snapshot{Addresses: []netip.Addr{netip.MustParseAddr("8.8.8.8")}, ExpiresAt: time.Now().Add(time.Minute)}}
	doc, err := Produce(context.Background(), raw, fixtureBase(t), resolver)
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Destinations) == 0 || resolver.calls == 0 || doc.ConfigurationDigest != api.Digest(source) || doc.ConfigurationRevision != source.Revision {
		t.Fatal("source binding missing")
	}
	encoded, _ := json.Marshal(doc)
	if strings.Contains(string(encoded), "secret") || strings.Contains(string(encoded), "mailbox_id") {
		t.Fatal("source metadata leaked")
	}
	resolver.snapshot.ExpiresAt = time.Now().Add(-time.Second)
	if _, err := Produce(context.Background(), raw, fixtureBase(t), resolver); err == nil {
		t.Fatal("stale DNS accepted")
	}
}

func TestMailReadinessRevokesOnRebindingAndEmptySource(t *testing.T) {
	doc := fixtureDocument(t)
	raw, _ := json.Marshal(doc)
	active, err := LoadMail(raw, doc.Digest(), fixtureBase(t))
	if err != nil {
		t.Fatal(err)
	}
	resolver := &fixtureResolver{snapshot: dnsresolver.Snapshot{Addresses: []netip.Addr{netip.MustParseAddr("8.8.8.8")}, ExpiresAt: time.Now().Add(time.Minute)}}
	r := NewReadiness(active, resolver)
	r.Check(context.Background())
	if ready, _ := r.Ready(); !ready {
		t.Fatal("matching DNS rejected")
	}
	resolver.snapshot.Addresses = []netip.Addr{netip.MustParseAddr("1.1.1.1")}
	r.Check(context.Background())
	if ready, _ := r.Ready(); ready {
		t.Fatal("rebound DNS ready")
	}
	empty := []byte(`{"version":"email-bridge/v1","revision":1,"managed_by":"git","source":"fixture","mailboxes":[]}`)
	doc, err = Produce(context.Background(), empty, fixtureBase(t), resolver)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ = json.Marshal(doc)
	active, err = LoadMail(raw, doc.Digest(), fixtureBase(t))
	if err != nil {
		t.Fatal(err)
	}
	r = NewReadiness(active, resolver)
	r.Check(context.Background())
	if ready, _ := r.Ready(); ready {
		t.Fatal("empty source ready")
	}
}

func TestReadinessExpiresWithoutRefreshAndWorkerJoins(t *testing.T) {
	doc := fixtureDocument(t)
	raw, _ := json.Marshal(doc)
	active, err := LoadMail(raw, doc.Digest(), fixtureBase(t))
	if err != nil {
		t.Fatal(err)
	}
	resolver := &fixtureResolver{snapshot: dnsresolver.Snapshot{Addresses: []netip.Addr{netip.MustParseAddr("8.8.8.8")}, ExpiresAt: time.Now().Add(-time.Second)}}
	r := NewReadiness(active, resolver)
	r.validUntil.Store(time.Now().Add(-time.Second).UnixNano())
	if ready, _ := r.Ready(); ready {
		t.Fatal("expired snapshot remains ready")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	done := make(chan error, 1)
	go func() { done <- r.Run(time.Hour)(ctx) }()
	select {
	case err := <-done:
		if err != context.Canceled {
			t.Fatal("unexpected worker outcome", err)
		}
	case <-time.After(time.Second):
		t.Fatal("mail readiness worker did not join")
	}
	if ready, _ := r.Ready(); ready {
		t.Fatal("cancelled worker remains ready")
	}
}
