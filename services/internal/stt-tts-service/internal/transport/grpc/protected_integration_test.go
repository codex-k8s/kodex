package grpc

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"github.com/codex-k8s/kodex/libs/go/sttapi"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth"
	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/authorityclient"
	av1 "github.com/codex-k8s/kodex/libs/go/internalrpcauth/gen/internalrpcauthority/v1"
	observability "github.com/codex-k8s/kodex/libs/go/observability"
	"github.com/codex-k8s/kodex/libs/go/serviceruntime"
	sttv1 "github.com/codex-k8s/kodex/libs/go/sttapi/gen/stt/v1"
	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/clients/projection"
	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/clients/protectedrpc"
	transcription "github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/domain/service/transcription"
	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/domain/types/value"
	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/integration/audio/ffmpeg"
	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/integration/provider/openai"
	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/testdata"
	"github.com/google/uuid"
	googlegrpc "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Fake заменяет только внешние authority/producer/provider, сохраняя настоящие
// mTLS, verifier middleware, continuation client, transport, spool и decoder.
type integrationBoundary struct {
	av1.AuthorizationVerifierServiceClient
	sttv1.UnimplementedTranscriptionPolicyProjectionServiceServer
	sttv1.UnimplementedTranscriptionCredentialProjectionServiceServer
	mu                sync.Mutex
	children          map[string]string
	deny              atomic.Bool
	missingPolicy     atomic.Bool
	missingCredential atomic.Bool
	posts             atomic.Int32
	probes            atomic.Int32
	providerStatus    atomic.Int32
	providerTimeout   atomic.Bool
}

type integrationIssuer struct {
	av1.AuthorizationIssuerServiceClient
	boundary *integrationBoundary
}

func (issuer integrationIssuer) IssueContinuationAuthorizationContext(ctx context.Context, request *av1.IssueContinuationAuthorizationContextRequest, options ...googlegrpc.CallOption) (*av1.IssueAuthorizationContextResponse, error) {
	return issuer.boundary.IssueContinuationAuthorizationContext(ctx, request, options...)
}

func (fake *integrationBoundary) VerifyAuthorizationContext(_ context.Context, request *av1.VerifyAuthorizationContextRequest, _ ...googlegrpc.CallOption) (*av1.VerifyAuthorizationContextResponse, error) {
	if fake.deny.Load() || request.GetCompactJws() != "test-parent" {
		return nil, status.Error(codes.PermissionDenied, "rejected")
	}
	digest := strings.Repeat("a", 64)
	identity := func(id string) *av1.AuthorityIdentity {
		return &av1.AuthorityIdentity{Id: id, Provenance: &av1.AuthorityProvenance{Source: av1.AuthoritySource_AUTHORITY_SOURCE_DOMAIN_STATE, Reference: "fixture:" + id, Revision: 1, DigestSha256: digest}}
	}
	return &av1.VerifyAuthorizationContextResponse{Context: &av1.VerifiedAuthorizationContext{
		ContractVersion: 1, AuthorityAbiVersion: internalrpcauth.AuthorityABIVersion, RequestBindingMode: internalrpcauth.RequestBindingStream,
		Audience: "urn:kodex:internal-rpc:stt-tts-service", TargetWorkloadId: "stt-tts-service", CallerWorkloadId: "control-api-gateway",
		FullMethod: sttv1.SpeechToTextService_Transcribe_FullMethodName, OperationId: "platform.stt.transcribe", Permission: value.TransportPermissionTranscribe,
		Jti: uuid.NewString(), SourceRevision: 1, SourceDigestSha256: digest, ExpiresAt: timestamppb.New(time.Now().Add(time.Minute)),
		Authority: &av1.CallerAuthority{ActorKind: av1.ActorKind_ACTOR_KIND_HUMAN, Actor: identity("user"), Tenant: identity("organization")},
	}}, nil
}

func (fake *integrationBoundary) IssueContinuationAuthorizationContext(_ context.Context, request *av1.IssueContinuationAuthorizationContextRequest, _ ...googlegrpc.CallOption) (*av1.IssueAuthorizationContextResponse, error) {
	if fake.deny.Load() || request.GetParentAuthorizationContextCompactJws() != "test-parent" || len(request.GetRequestDigestSha256()) != 64 || request.GetRequestId() == "" || request.GetCorrelationId() == "" {
		return nil, status.Error(codes.PermissionDenied, "rejected")
	}
	if request.GetOperationId() != "platform.stt.policy.resolve" && request.GetOperationId() != "platform.stt.credential.project" {
		return nil, status.Error(codes.PermissionDenied, "rejected")
	}
	token := uuid.NewString()
	fake.mu.Lock()
	fake.children[token] = request.GetRequestDigestSha256()
	fake.mu.Unlock()
	return &av1.IssueAuthorizationContextResponse{CompactJws: token}, nil
}

func (fake *integrationBoundary) verifyChild(ctx context.Context, request any, _ *googlegrpc.UnaryServerInfo, handler googlegrpc.UnaryHandler) (any, error) {
	md, _ := metadata.FromIncomingContext(ctx)
	tokens := md.Get(authorityclient.AuthorizationMetadata)
	if len(tokens) != 1 {
		return nil, status.Error(codes.PermissionDenied, "missing child")
	}
	raw, err := proto.MarshalOptions{Deterministic: true}.Marshal(request.(proto.Message))
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(raw)
	fake.mu.Lock()
	expected, ok := fake.children[tokens[0]]
	delete(fake.children, tokens[0])
	fake.mu.Unlock()
	if !ok || expected != hex.EncodeToString(digest[:]) {
		return nil, status.Error(codes.PermissionDenied, "binding mismatch")
	}
	return handler(ctx, request)
}

func (fake *integrationBoundary) ResolveTranscriptionPolicy(_ context.Context, request *sttv1.ResolveTranscriptionPolicyRequest) (*sttv1.ResolveTranscriptionPolicyResponse, error) {
	if fake.missingPolicy.Load() {
		return nil, status.Error(codes.FailedPrecondition, "not configured")
	}
	if request.GetAuthority().GetTenantId() != "organization" || request.GetAuthority().GetProjectId() != "" || request.GetAuthority().GetProject() != nil {
		return nil, status.Error(codes.PermissionDenied, "scope mismatch")
	}
	return &sttv1.ResolveTranscriptionPolicyResponse{Authority: request.Authority, ConfigRevision: 1, ConfigDigestSha256: strings.Repeat("b", 64), Model: value.DefaultModel,
		Parameters:        &sttv1.TranscriptionParameters{Languages: []string{"ru", "en"}, Keywords: []string{"Kodex"}, Prompt: "Technical discussion", Temperature: 0, ChunkingStrategy: "auto"},
		MaximumAudioBytes: 10 << 20, MaximumAudioDurationMilliseconds: 120000, ProviderTimeoutMilliseconds: 1000, ProviderAccountRef: "test-account", ProviderCredentialGeneration: 1, ExpiresAt: timestamppb.New(time.Now().Add(30 * time.Second))}, nil
}
func (fake *integrationBoundary) ProjectTranscriptionCredential(_ context.Context, request *sttv1.ProjectTranscriptionCredentialRequest) (*sttv1.ProjectTranscriptionCredentialResponse, error) {
	if fake.missingCredential.Load() {
		return nil, status.Error(codes.PermissionDenied, "revoked")
	}
	return &sttv1.ProjectTranscriptionCredentialResponse{Authority: request.Authority, ConfigRevision: request.ConfigRevision, ConfigDigestSha256: request.ConfigDigestSha256, ProviderAccountRef: request.ProviderAccountRef, ProviderCredentialGeneration: request.ProviderCredentialGeneration, ApiKey: []byte("synthetic-test-only"), ExpiresAt: timestamppb.New(time.Now().Add(10 * time.Second))}, nil
}
func (fake *integrationBoundary) Do(request *http.Request) (*http.Response, error) {
	if request.URL.Scheme != "https" || request.URL.Host != "api.openai.com" || request.Header.Get("Authorization") != "Bearer synthetic-test-only" {
		return nil, status.Error(codes.PermissionDenied, "provider boundary")
	}
	if request.Method == http.MethodGet && request.URL.Path == "/v1/models/gpt-transcribe" {
		fake.probes.Add(1)
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"id":"gpt-transcribe","object":"model"}`))}, nil
	}
	if request.Method != http.MethodPost || request.URL.Path != "/v1/audio/transcriptions" {
		return nil, status.Error(codes.InvalidArgument, "unexpected provider operation")
	}
	fake.posts.Add(1)
	if fake.providerTimeout.Load() {
		<-request.Context().Done()
		return nil, request.Context().Err()
	}
	if err := request.ParseMultipartForm(1 << 20); err != nil {
		return nil, err
	}
	defer request.MultipartForm.RemoveAll()
	fields := request.MultipartForm.Value
	if strings.Join(fields["languages[]"], ",") != "ru,en" || len(fields["language"]) != 0 || strings.Join(fields["keywords[]"], ",") != "Kodex" || strings.Join(fields["prompt"], "") != "Technical discussion" || strings.Join(fields["chunking_strategy"], "") != "auto" {
		return nil, errors.New("projection parameters mismatch")
	}
	code := int(fake.providerStatus.Load())
	if code == 0 {
		code = 200
	}
	return &http.Response{StatusCode: code, Body: io.NopCloser(strings.NewReader(`{"text":"раз два три четыре пять","languages":[{"code":"ru"}]}`))}, nil
}

func TestProtectedFakeIntegration(t *testing.T) {
	dir := t.TempDir()
	certificate, pool, certFile, keyFile, caFile := integrationTLS(t, dir)
	serverTLS := &tls.Config{MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS13, Certificates: []tls.Certificate{certificate}, ClientAuth: tls.RequireAndVerifyClientCert, ClientCAs: pool}
	fake := &integrationBoundary{children: map[string]string{}}
	producer := googlegrpc.NewServer(googlegrpc.Creds(credentials.NewTLS(serverTLS)), googlegrpc.UnaryInterceptor(fake.verifyChild))
	sttv1.RegisterTranscriptionPolicyProjectionServiceServer(producer, fake)
	sttv1.RegisterTranscriptionCredentialProjectionServiceServer(producer, fake)
	producerAddress := serveIntegration(t, producer)
	deps, err := protectedrpc.Dial(t.Context(), protectedrpc.Config{Policy: protectedrpc.TargetConfig{Target: producerAddress, TLSServerName: "stt-test", CAFile: caFile}, Credential: protectedrpc.TargetConfig{Target: producerAddress, TLSServerName: "stt-test", CAFile: caFile}, CertificateFile: certFile, PrivateKeyFile: keyFile, DialTimeout: time.Second, Issuer: integrationIssuer{boundary: fake}})
	if err != nil {
		t.Fatal(err)
	}
	defer deps.Close()
	policy, _ := projection.NewPolicy(deps.Policy, deps)
	credential, _ := projection.NewCredential(deps.Credential, deps)
	provider, _ := openai.NewWithHTTPClient(fake)
	domain, err := transcription.New(policy, credential, provider, transcription.ObserverFunc(func(value.Stage, value.ErrorClass) {}), 10*time.Second, ffmpeg.New(dir))
	if err != nil {
		t.Fatal(err)
	}
	readiness := serviceruntime.NewReadiness()
	readiness.Set(true, "ready")
	handler, err := New(domain, dir, readiness, 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	server := googlegrpc.NewServer(googlegrpc.Creds(credentials.NewTLS(serverTLS)), googlegrpc.ChainStreamInterceptor(observability.StreamCorrelationServerInterceptor(), authorityclient.VerifierStreamServerInterceptor(fake), handler.StreamServerInterceptor()))
	sttv1.RegisterSpeechToTextServiceServer(server, handler)
	address := serveIntegration(t, server)
	connection, err := googlegrpc.NewClient(address, googlegrpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS13, ServerName: "stt-test", RootCAs: pool, Certificates: []tls.Certificate{certificate}})))
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	client := sttv1.NewSpeechToTextServiceClient(connection)
	call := func(probe bool, raw []byte) (*sttv1.TranscribeResponse, error) {
		ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
		defer cancel()
		if probe {
			availability, err := sttapi.CheckAvailability(metadata.NewOutgoingContext(ctx, metadata.Pairs(authorityclient.AuthorizationMetadata, "test-parent")), client)
			return &sttv1.TranscribeResponse{Availability: availability}, err
		}
		stream, err := client.Transcribe(metadata.NewOutgoingContext(ctx, metadata.Pairs(authorityclient.AuthorizationMetadata, "test-parent")))
		if err != nil {
			return nil, err
		}
		if probe {
			err = stream.Send(&sttv1.TranscribeRequest{Body: &sttv1.TranscribeRequest_AvailabilityCheck{AvailabilityCheck: &sttv1.CheckProtectedPathRequest{}}})
		} else {
			err = stream.Send(&sttv1.TranscribeRequest{Body: &sttv1.TranscribeRequest_Metadata{Metadata: &sttv1.TranscribeMetadata{MediaType: "audio/mpeg", SizeBytes: uint64(len(raw))}}})
			if err == nil {
				err = stream.Send(&sttv1.TranscribeRequest{Body: &sttv1.TranscribeRequest_Chunk{Chunk: raw}})
			}
			digest := sha256.Sum256(raw)
			if err == nil {
				err = stream.Send(&sttv1.TranscribeRequest{Body: &sttv1.TranscribeRequest_Commit{Commit: &sttv1.TranscribeCommit{SizeBytes: uint64(len(raw)), Sha256: hex.EncodeToString(digest[:])}}})
			}
		}
		if err != nil && err != io.EOF {
			return nil, err
		}
		return stream.CloseAndRecv()
	}
	response, err := call(false, testdata.RussianNumbers)
	if err != nil || response.GetReceipt().GetTenantId() != "organization" || response.GetReceipt().GetProjectId() != "" || response.GetText() == "" || fake.posts.Load() != 1 {
		t.Fatalf("protected success failed: %v", err)
	}
	response, err = call(true, nil)
	if err != nil || !response.GetAvailability().GetReady() || response.GetAvailability().GetValidUntil() == nil || len(response.GetAvailability().GetCatalog().GetModels()) != 5 || response.GetText() != "" || fake.posts.Load() != 1 || fake.probes.Load() != 1 {
		t.Fatalf("availability failed: %v", err)
	}
	for _, tc := range []struct {
		name  string
		set   func(bool)
		probe bool
	}{
		{"revoked authority", fake.deny.Store, false}, {"missing policy", fake.missingPolicy.Store, false}, {"revoked credential", fake.missingCredential.Store, false}, {"probe credential", fake.missingCredential.Store, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.set(true)
			defer tc.set(false)
			response, err := call(tc.probe, testdata.RussianNumbers)
			if err == nil && (!tc.probe || response.GetAvailability().GetReady()) {
				t.Fatal("negative accepted")
			}
			if fake.posts.Load() != 1 {
				t.Fatal("billable effect before authorization")
			}
		})
	}
	if _, err := call(false, []byte("malformed")); err == nil || fake.posts.Load() != 1 {
		t.Fatal("malformed audio reached provider")
	}
	fake.providerStatus.Store(429)
	if _, err := call(false, testdata.RussianNumbers); err == nil || fake.posts.Load() != 2 {
		t.Fatal("provider failure retried or accepted")
	}
	fake.providerStatus.Store(0)
	fake.providerTimeout.Store(true)
	start := time.Now()
	if _, err := call(false, testdata.RussianNumbers); err == nil || time.Since(start) > 2500*time.Millisecond || fake.posts.Load() != 3 {
		t.Fatal("provider timeout is not bounded/single effect")
	}
	files, _ := filepath.Glob(filepath.Join(dir, "request-*.audio"))
	if len(files) != 0 {
		t.Fatal("request spool leaked")
	}
}

func serveIntegration(t *testing.T, server *googlegrpc.Server) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { defer close(done); _ = server.Serve(listener) }()
	t.Cleanup(func() { server.Stop(); <-done })
	return listener.Addr().String()
}
func integrationTLS(t *testing.T, dir string) (tls.Certificate, *x509.CertPool, string, string, string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	uri, _ := url.Parse("spiffe://kodex.local/ns/kodex-system/sa/control-api-gateway")
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "stt-test"}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour), DNSNames: []string{"stt-test"}, URIs: []*url.URL{uri}, IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth}}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyDER, _ := x509.MarshalPKCS8PrivateKey(key)
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
	certFile, keyFile, caFile := filepath.Join(dir, "certificate.pem"), filepath.Join(dir, "key.pem"), filepath.Join(dir, "ca.pem")
	for path, raw := range map[string][]byte{certFile: certPEM, keyFile: keyPEM, caFile: certPEM} {
		if err := os.WriteFile(path, raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	certificate, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(certPEM)
	return certificate, pool, certFile, keyFile, caFile
}
