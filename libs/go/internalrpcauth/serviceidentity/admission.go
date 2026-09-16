// Package serviceidentity проверяет локальный допуск сервиса к точному RPC.
// Допуск не назначает actor, tenant, project или права конкретной задачи.
package serviceidentity

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/x509"
	"errors"
	"net/url"
	"regexp"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

// ActorMode определяет следующую обязательную границу после допуска RPC.
type ActorMode string

const (
	ServiceActor ActorMode = "SERVICE_OWNER_RESOLVED"
	UserActor    ActorMode = "USER_CREDENTIAL_REQUIRED"
	TaskActor    ActorMode = "TASK_DELEGATION_REQUIRED"
)

var (
	methodPattern       = regexp.MustCompile(`^/[A-Za-z][A-Za-z0-9_.]*\.[A-Za-z][A-Za-z0-9_]*/[A-Za-z][A-Za-z0-9_]*$`)
	permissionPattern   = regexp.MustCompile(`^[a-z][a-z0-9.-]{0,127}$`)
	identityPathPattern = regexp.MustCompile(`^/ns/[a-z0-9][a-z0-9-]{0,62}/sa/[a-z0-9][a-z0-9-]{0,62}$`)
	trustDomainPattern  = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]{0,252}$`)
)

// Binding устанавливается владельцем target, а не полями запроса.
type Binding struct {
	CallerSPIFFEID  string    `json:"caller_spiffe_id"`
	FullMethod      string    `json:"full_method"`
	OperationID     string    `json:"operation_id"`
	Permission      string    `json:"permission"`
	ActorMode       ActorMode `json:"actor_mode"`
	ProjectRequired bool      `json:"project_required"`
}

// PeerIdentity содержит только проверенные свойства транспортного peer.
type PeerIdentity struct {
	SPIFFEID          string
	CertificateSHA256 [32]byte
	NotAfter          time.Time
}

// RevocationBoundary читает локально загруженное authoritative состояние
// отзыва. Реализация обязана сохранять durable floor при замене процесса;
// отсутствие состояния и ошибка чтения должны возвращать ошибку.
// Синхронный RPC к authority в этом интерфейсе не допускается.
type RevocationBoundary interface {
	CheckPeer(context.Context, PeerIdentity) error
}

// CertificateLifetimeBoundary — явный ускоренный MVP-профиль: допуск
// прекращается по сроку сертификата, а отдельный emergency deny-list пока не
// участвует в каждом RPC. Локальный allowlist методов остаётся обязательным.
type CertificateLifetimeBoundary struct{}

func (CertificateLifetimeBoundary) CheckPeer(ctx context.Context, _ PeerIdentity) error {
	return ctx.Err()
}

// Admission не является пользовательским Principal или task grant.
type Admission struct {
	// RPCProfile назначается серверным authorizer, а не request metadata.
	RPCProfile      string
	Peer            PeerIdentity
	TargetSPIFFEID  string
	FullMethod      string
	OperationID     string
	Permission      string
	ActorMode       ActorMode
	ProjectRequired bool
}

// Authorizer владеет неизменяемой копией локальных разрешений target.
type Authorizer struct {
	target      string
	domain      string
	bindings    map[string]Binding
	revocations RevocationBoundary
	now         func() time.Time
}

func (*Authorizer) RPCProfile() string { return "service-v1" }

func New(target string, bindings []Binding, revocations RevocationBoundary) (*Authorizer, error) {
	domain, ok := identityDomain(target)
	if !ok || len(bindings) == 0 || len(bindings) > 4096 || revocations == nil {
		return nil, errors.New("service identity configuration rejected")
	}
	result := &Authorizer{target: target, domain: domain, bindings: make(map[string]Binding, len(bindings)), revocations: revocations, now: time.Now}
	for _, binding := range bindings {
		callerDomain, valid := identityDomain(binding.CallerSPIFFEID)
		if !valid || callerDomain != domain || !methodPattern.MatchString(binding.FullMethod) || len(binding.FullMethod) > 256 ||
			!permissionPattern.MatchString(binding.OperationID) || !permissionPattern.MatchString(binding.Permission) ||
			(binding.ActorMode != ServiceActor && binding.ActorMode != UserActor && binding.ActorMode != TaskActor) {
			return nil, errors.New("service identity binding rejected")
		}
		key := binding.CallerSPIFFEID + "\x00" + binding.FullMethod
		if _, exists := result.bindings[key]; exists {
			return nil, errors.New("duplicate service identity binding")
		}
		result.bindings[key] = binding
	}
	return result, nil
}

// Admit проверяет уже установленный mTLS transport на каждом RPC, включая
// законный срок сертификата на давно открытом HTTP/2 соединении и отзыв.
// Здесь нет issuer, выдачи токена, общей revision или регистрации Pod.
func (authorizer *Authorizer) Admit(ctx context.Context, fullMethod string) (Admission, error) {
	if ctx == nil {
		return Admission{}, status.Error(codes.Unauthenticated, "mTLS service identity required")
	}
	if err := ctx.Err(); err != nil {
		return Admission{}, status.FromContextError(err).Err()
	}
	transport, ok := peer.FromContext(ctx)
	if !ok {
		return Admission{}, status.Error(codes.Unauthenticated, "mTLS service identity required")
	}
	tlsInfo, ok := transport.AuthInfo.(credentials.TLSInfo)
	if !ok || !tlsInfo.State.HandshakeComplete || len(tlsInfo.State.PeerCertificates) == 0 || len(tlsInfo.State.VerifiedChains) == 0 {
		return Admission{}, status.Error(codes.Unauthenticated, "verified mTLS service identity required")
	}
	certificate := tlsInfo.State.PeerCertificates[0]
	if certificate == nil || len(certificate.Raw) == 0 || len(certificate.URIs) != 1 {
		return Admission{}, status.Error(codes.Unauthenticated, "mTLS service certificate rejected")
	}
	matchedChain := false
	for _, chain := range tlsInfo.State.VerifiedChains {
		if len(chain) > 0 && chain[0] != nil && bytes.Equal(chain[0].Raw, certificate.Raw) {
			matchedChain = true
			break
		}
	}
	if !matchedChain {
		return Admission{}, status.Error(codes.Unauthenticated, "mTLS verified chain binding rejected")
	}
	now := authorizer.now()
	if now.Before(certificate.NotBefore) || !now.Before(certificate.NotAfter) || !clientCertificate(certificate) {
		return Admission{}, status.Error(codes.Unauthenticated, "mTLS service certificate expired or invalid")
	}
	if certificate.URIs[0] == nil {
		return Admission{}, status.Error(codes.Unauthenticated, "mTLS service identity rejected")
	}
	identity := certificate.URIs[0].String()
	domain, valid := identityDomain(identity)
	if !valid || domain != authorizer.domain {
		return Admission{}, status.Error(codes.PermissionDenied, "mTLS service trust domain rejected")
	}
	binding, allowed := authorizer.bindings[identity+"\x00"+fullMethod]
	if !allowed {
		return Admission{}, status.Error(codes.PermissionDenied, "service RPC is not permitted")
	}
	verified := PeerIdentity{SPIFFEID: identity, CertificateSHA256: sha256.Sum256(certificate.Raw), NotAfter: certificate.NotAfter}
	if err := authorizer.revocations.CheckPeer(ctx, verified); err != nil {
		// Причина может содержать private storage details; наружу она не выходит.
		return Admission{}, status.Error(codes.PermissionDenied, "service credential is revoked or revocation state unavailable")
	}
	return Admission{RPCProfile: authorizer.RPCProfile(), Peer: verified, TargetSPIFFEID: authorizer.target, FullMethod: fullMethod, OperationID: binding.OperationID, Permission: binding.Permission, ActorMode: binding.ActorMode, ProjectRequired: binding.ProjectRequired}, nil
}

func identityDomain(raw string) (string, bool) {
	value, err := url.Parse(raw)
	if err != nil || value.Scheme != "spiffe" || !trustDomainPattern.MatchString(value.Host) || value.User != nil || value.RawQuery != "" || value.ForceQuery || value.Fragment != "" || value.RawPath != "" || !identityPathPattern.MatchString(value.Path) || value.String() != raw {
		return "", false
	}
	return value.Host, true
}

func clientCertificate(certificate *x509.Certificate) bool {
	for _, usage := range certificate.ExtKeyUsage {
		if usage == x509.ExtKeyUsageClientAuth {
			return true
		}
	}
	return false
}
