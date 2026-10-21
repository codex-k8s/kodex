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

// ActorMode selects the additional authority check required after transport
// admission; a permitted RPC method alone does not establish an actor.
// RU: ActorMode выбирает следующую обязательную проверку полномочий после
// допуска транспорта; разрешённый RPC-метод сам по себе не назначает actor.
type ActorMode string

const (
	// ServiceActor requires the target domain to resolve the service actor and
	// resource owner instead of trusting a caller-supplied project.
	// RU: ServiceActor требует от домена-получателя разрешить служебного actor
	// и владельца ресурса, а не доверять project от вызывающей стороны.
	ServiceActor ActorMode = "SERVICE_OWNER_RESOLVED"
	// UserActor requires a separate verified user credential on the admitted RPC.
	// RU: UserActor требует отдельного проверенного пользовательского credential.
	UserActor ActorMode = "USER_CREDENTIAL_REQUIRED"
	// TaskActor requires exact task delegation beyond service transport identity.
	// RU: TaskActor требует точного делегирования задачи сверх служебной
	// транспортной идентичности.
	TaskActor ActorMode = "TASK_DELEGATION_REQUIRED"
)

var (
	// methodPattern accepts only a complete gRPC method name in a target binding.
	// RU: methodPattern допускает в binding только полное имя gRPC-метода.
	methodPattern = regexp.MustCompile(`^/[A-Za-z][A-Za-z0-9_.]*\.[A-Za-z][A-Za-z0-9_]*/[A-Za-z][A-Za-z0-9_]*$`)
	// permissionPattern bounds operation and permission identifiers in policy.
	// RU: permissionPattern ограничивает идентификаторы операции и разрешения.
	permissionPattern = regexp.MustCompile(`^[a-z][a-z0-9.-]{0,127}$`)
	// identityPathPattern restricts SPIFFE identities to one namespace and service account.
	// RU: identityPathPattern ограничивает SPIFFE-идентичность namespace и ServiceAccount.
	identityPathPattern = regexp.MustCompile(`^/ns/[a-z0-9][a-z0-9-]{0,62}/sa/[a-z0-9][a-z0-9-]{0,62}$`)
	// trustDomainPattern restricts the spelling and length of the SPIFFE trust domain.
	// RU: trustDomainPattern ограничивает формат и длину домена доверия SPIFFE.
	trustDomainPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]{0,252}$`)
)

// Binding is a target-owned grant for one caller identity and exact RPC method;
// operation, permission and downstream actor mode cannot come from payload.
// RU: Binding является правилом target для одной идентичности caller и точного
// RPC-метода; операция, разрешение и режим actor не берутся из payload.
type Binding struct {
	CallerSPIFFEID  string    `json:"caller_spiffe_id"`
	FullMethod      string    `json:"full_method"`
	OperationID     string    `json:"operation_id"`
	Permission      string    `json:"permission"`
	ActorMode       ActorMode `json:"actor_mode"`
	ProjectRequired bool      `json:"project_required"`
}

// PeerIdentity records verified certificate properties of one transport peer;
// it is not a user principal or a task-specific capability.
// RU: PeerIdentity хранит проверенные свойства сертификата транспортного peer,
// но не является пользовательским principal или правом конкретной задачи.
type PeerIdentity struct {
	SPIFFEID          string
	CertificateSHA256 [32]byte
	NotAfter          time.Time
}

// RevocationBoundary checks locally loaded authoritative revocation state;
// implementations must retain a durable floor across process replacement and
// fail closed when the state is missing or unreadable, without synchronous RPC.
// RU: RevocationBoundary проверяет локально загруженное авторитетное состояние
// отзыва; реализация сохраняет durable floor при замене процесса и закрыто
// отклоняет отсутствие/ошибку состояния без синхронного RPC к authority.
type RevocationBoundary interface {
	// CheckPeer returns an error when the exact verified peer is revoked or
	// authoritative revocation state is unavailable; callers must fail closed.
	// RU: CheckPeer возвращает ошибку при отзыве проверенного peer или
	// недоступности авторитетного состояния; вызывающий код закрыто отказывает.
	CheckPeer(context.Context, PeerIdentity) error
}

// CertificateLifetimeBoundary is the explicit MVP profile that uses the
// certificate lifetime instead of a per-RPC emergency deny-list; exact method
// bindings and certificate checks still apply in Authorizer.Admit.
// RU: CertificateLifetimeBoundary — явный MVP-профиль со сроком сертификата
// вместо emergency deny-list на каждом RPC; точные bindings методов и проверка
// сертификата в Authorizer.Admit остаются обязательными.
type CertificateLifetimeBoundary struct{}

// CheckPeer rejects a cancelled context but performs no emergency revocation
// lookup in this explicitly selected certificate-lifetime profile.
// RU: CheckPeer отклоняет отменённый контекст, но не читает emergency-список
// отзыва в явно выбранном профиле по сроку сертификата.
func (CertificateLifetimeBoundary) CheckPeer(ctx context.Context, _ PeerIdentity) error {
	return ctx.Err()
}

// Admission carries the target's transport and method decision downstream;
// it does not replace user credentials, ownership checks or task grants.
// RU: Admission передаёт решение target о транспорте и методе дальше, но не
// заменяет credential пользователя, проверку владения или grant задачи.
type Admission struct {
	// RPCProfile is set by the server authorizer, never by request metadata.
	// RU: RPCProfile назначает серверный authorizer, а не metadata запроса.
	RPCProfile      string
	Peer            PeerIdentity
	TargetSPIFFEID  string
	FullMethod      string
	OperationID     string
	Permission      string
	ActorMode       ActorMode
	ProjectRequired bool
}

// Authorizer owns an immutable exact caller/method binding map for one target
// and applies the current peer validity and revocation boundary on each RPC.
// RU: Authorizer хранит неизменяемую карту точных caller/method для target и
// проверяет актуальность peer и границу отзыва на каждом RPC.
type Authorizer struct {
	target      string
	domain      string
	bindings    map[string]Binding
	revocations RevocationBoundary
	now         func() time.Time
}

// RPCProfile identifies this certificate-based server profile; an incoming
// header cannot switch its transport or admission policy.
// RU: RPCProfile называет серверный профиль с сертификатом; входящий заголовок
// не может переключить транспорт либо политику допуска.
func (*Authorizer) RPCProfile() string { return "service-v1" }

// New validates one target SPIFFE identity and closed bindings, rejecting
// malformed, cross-domain and duplicate caller/method grants before serving.
// RU: New проверяет SPIFFE-идентичность target и закрытый набор bindings,
// отклоняя некорректные, междоменные и повторные caller/method до обслуживания.
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

// Admit verifies the established mTLS peer, matching certificate chain,
// lifetime, exact caller/method binding and revocation on every RPC, including
// long-lived HTTP/2 connections; it issues no token or domain authority.
// RU: Admit на каждом RPC проверяет установленный mTLS peer, цепочку
// сертификата, срок, точный caller/method и отзыв даже при долгом HTTP/2
// соединении; токены и доменные полномочия здесь не выдаются.
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

// identityDomain accepts only canonical SPIFFE URIs with the expected
// namespace/service-account path and returns their trust domain.
// RU: identityDomain принимает только канонические SPIFFE URI с ожидаемым
// путём namespace/ServiceAccount и возвращает домен доверия.
func identityDomain(raw string) (string, bool) {
	value, err := url.Parse(raw)
	if err != nil || value.Scheme != "spiffe" || !trustDomainPattern.MatchString(value.Host) || value.User != nil || value.RawQuery != "" || value.ForceQuery || value.Fragment != "" || value.RawPath != "" || !identityPathPattern.MatchString(value.Path) || value.String() != raw {
		return "", false
	}
	return value.Host, true
}

// clientCertificate requires the client-auth extended key usage on the peer
// certificate before it can identify an RPC caller.
// RU: clientCertificate требует назначение client-auth у сертификата peer
// перед его использованием для идентификации RPC caller.
func clientCertificate(certificate *x509.Certificate) bool {
	for _, usage := range certificate.ExtKeyUsage {
		if usage == x509.ExtKeyUsageClientAuth {
			return true
		}
	}
	return false
}
