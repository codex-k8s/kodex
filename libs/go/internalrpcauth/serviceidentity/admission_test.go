package serviceidentity

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"math/big"
	"net"
	"net/url"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

const caller = "spiffe://kodex.local/ns/kodex-system/sa/runtime-controller"
const target = "spiffe://kodex.local/ns/kodex-system/sa/control-plane"
const method = "/controlplane.v1.RuntimeWorkService/ClaimWork"

type revocationCheck struct {
	err   error
	calls int
}

func (check *revocationCheck) CheckPeer(context.Context, PeerIdentity) error {
	check.calls++
	return check.err
}

func binding() Binding {
	return Binding{CallerSPIFFEID: caller, FullMethod: method, OperationID: "runtime.work.claim", Permission: "runtime.work.claim", ActorMode: ServiceActor}
}
func syntheticPeer(t *testing.T) (context.Context, *x509.Certificate) {
	t.Helper()
	identity, _ := url.Parse(caller)
	cert := &x509.Certificate{Raw: []byte("synthetic certificate"), URIs: []*url.URL{identity}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Minute), ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}
	ctx := peer.NewContext(t.Context(), &peer.Peer{AuthInfo: credentials.TLSInfo{State: tls.ConnectionState{HandshakeComplete: true, PeerCertificates: []*x509.Certificate{cert}, VerifiedChains: [][]*x509.Certificate{{cert}}}}})
	return ctx, cert
}

func TestLocalAdmissionPreservesIdentityAndDoesNotTrustMetadata(t *testing.T) {
	check := &revocationCheck{}
	bindings := []Binding{binding()}
	auth, err := New(target, bindings, check)
	if err != nil {
		t.Fatal(err)
	}
	bindings[0].Permission = "changed.permission"
	ctx, _ := syntheticPeer(t)
	ctx = metadata.NewIncomingContext(ctx, metadata.Pairs("x-kodex-actor", "owner", "x-kodex-tenant", "foreign", "x-kodex-permission", "everything"))
	for range 2 {
		admission, err := auth.Admit(ctx, method)
		if err != nil {
			t.Fatal(err)
		}
		if admission.Permission != "runtime.work.claim" || admission.Peer.SPIFFEID != caller || admission.ActorMode != ServiceActor || admission.TargetSPIFFEID != target {
			t.Fatal("unexpected admission")
		}
	}
	if check.calls != 2 {
		t.Fatal("revocation must be checked for every RPC")
	}
	check.err = errors.New("private credential sentinel")
	_, err = auth.Admit(ctx, method)
	if status.Code(err) != codes.PermissionDenied || strings.Contains(err.Error(), "sentinel") {
		t.Fatal("revocation failure was not closed and redacted")
	}
}

func TestAdmissionRejectsUnverifiedExpiredAndWrongMethod(t *testing.T) {
	auth, err := New(target, []Binding{binding()}, &revocationCheck{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = auth.Admit(t.Context(), method); status.Code(err) != codes.Unauthenticated {
		t.Fatal("missing TLS accepted")
	}
	ctx, cert := syntheticPeer(t)
	if _, err = auth.Admit(ctx, method+"Other"); status.Code(err) != codes.PermissionDenied {
		t.Fatal("unregistered method accepted")
	}
	auth.now = func() time.Time { return cert.NotAfter }
	if _, err = auth.Admit(ctx, method); status.Code(err) != codes.Unauthenticated {
		t.Fatal("expired certificate on existing connection accepted")
	}
	auth.now = time.Now
	for _, mutate := range []func(*tls.ConnectionState){
		func(s *tls.ConnectionState) { s.VerifiedChains = nil },
		func(s *tls.ConnectionState) { s.HandshakeComplete = false },
		func(s *tls.ConnectionState) { s.VerifiedChains = [][]*x509.Certificate{{{Raw: []byte("other")}}} },
	} {
		p, _ := peer.FromContext(ctx)
		info := p.AuthInfo.(credentials.TLSInfo)
		mutate(&info.State)
		if _, err = auth.Admit(peer.NewContext(t.Context(), &peer.Peer{AuthInfo: info}), method); status.Code(err) != codes.Unauthenticated {
			t.Fatal("unverified transport accepted")
		}
	}
}

func TestClosedPolicyAndActorModes(t *testing.T) {
	check := &revocationCheck{}
	if _, err := New(target, []Binding{binding()}, nil); err == nil {
		t.Fatal("missing revocation boundary accepted")
	}
	if _, err := New(target, []Binding{binding(), binding()}, check); err == nil {
		t.Fatal("duplicate binding accepted")
	}
	for _, mode := range []ActorMode{ServiceActor, UserActor, TaskActor} {
		b := binding()
		b.ActorMode = mode
		if _, err := New(target, []Binding{b}, check); err != nil {
			t.Fatal(err)
		}
	}
	for _, mutate := range []func(*Binding){func(b *Binding) { b.ActorMode = "" }, func(b *Binding) { b.CallerSPIFFEID = caller + "?" }, func(b *Binding) { b.CallerSPIFFEID = strings.Replace(caller, "kodex.local", "other.local", 1) }, func(b *Binding) { b.FullMethod = "*" }} {
		b := binding()
		mutate(&b)
		if _, err := New(target, []Binding{b}, check); err == nil {
			t.Fatal("invalid policy accepted")
		}
	}
}

func TestAdmissionFromRealMutualTLSHandshake(t *testing.T) {
	public, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ca := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "synthetic CA"}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	caDER, err := x509.CreateCertificate(rand.Reader, ca, ca, public, key)
	if err != nil {
		t.Fatal(err)
	}
	ca, err = x509.ParseCertificate(caDER)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(ca)
	makeCertificate := func(client bool) tls.Certificate {
		pub, private, e := ed25519.GenerateKey(rand.Reader)
		if e != nil {
			t.Fatal(e)
		}
		certificate := &x509.Certificate{SerialNumber: big.NewInt(2), DNSNames: []string{"localhost"}, NotBefore: ca.NotBefore, NotAfter: ca.NotAfter, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
		if client {
			identity, _ := url.Parse(caller)
			certificate.URIs = []*url.URL{identity}
			certificate.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}
		}
		der, e := x509.CreateCertificate(rand.Reader, certificate, ca, pub, key)
		if e != nil {
			t.Fatal(e)
		}
		return tls.Certificate{Certificate: [][]byte{der, caDER}, PrivateKey: private}
	}
	serverCertificate, clientCertificate := makeCertificate(false), makeCertificate(true)
	listener, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{serverCertificate}, ClientAuth: tls.RequireAndVerifyClientCert, ClientCAs: roots})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	states := make(chan tls.ConnectionState, 1)
	failures := make(chan error, 1)
	go func() {
		connection, e := listener.Accept()
		if e != nil {
			failures <- e
			return
		}
		defer connection.Close()
		_ = connection.SetDeadline(time.Now().Add(5 * time.Second))
		secure := connection.(*tls.Conn)
		if e = secure.HandshakeContext(t.Context()); e != nil {
			failures <- e
			return
		}
		states <- secure.ConnectionState()
	}()
	connection, err := tls.DialWithDialer(&net.Dialer{Timeout: 5 * time.Second}, "tcp", listener.Addr().String(), &tls.Config{MinVersion: tls.VersionTLS13, ServerName: "localhost", RootCAs: roots, Certificates: []tls.Certificate{clientCertificate}})
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	select {
	case state := <-states:
		auth, e := New(target, []Binding{binding()}, &revocationCheck{})
		if e != nil {
			t.Fatal(e)
		}
		admitted, e := auth.Admit(peer.NewContext(t.Context(), &peer.Peer{AuthInfo: credentials.TLSInfo{State: state}}), method)
		if e != nil || admitted.Peer.SPIFFEID != caller {
			t.Fatalf("real mutual TLS admission failed: %v", e)
		}
	case e := <-failures:
		t.Fatal(e)
	case <-time.After(6 * time.Second):
		t.Fatal("mutual TLS handshake timeout")
	}
}
