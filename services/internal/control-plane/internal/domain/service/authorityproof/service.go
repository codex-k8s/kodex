// Package authorityproof issues short-lived proofs after observed peer and
// application-credential checks and owner-resolved domain authorization;
// signature verification alone does not confer actor or tenant authority.
// Пакет authorityproof выпускает краткоживущие доказательства после проверки
// наблюдаемой стороны, учётных данных приложения и разрешённых владельцем
// доменных полномочий; одной подписи для прав исполнителя или организации мало.
package authorityproof

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth"
	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/authorityclaims"
	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/policydeclaration"
	"github.com/codex-k8s/kodex/libs/go/oidcverifier"
	domainerrs "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/google/uuid"
)

const (
	// workerGrantType pins the protected JWS type accepted as a worker application credential, not an
	// authority-proof or user-session token.
	// workerGrantType закрепляет защищённый тип JWS учётных данных рабочего процесса, не доказательства
	// полномочий или токена пользовательской сессии.
	workerGrantType = "kodex-application-grant+jws"
	// maximumFileSize sets the post-read one-MiB length bound for configured policy/key/grant files; the
	// current reader does not impose a streaming allocation bound.
	// maximumFileSize задаёт предел длины файлов политики, ключей и grant в один МиБ после чтения;
	// потокового ограничения выделения памяти текущий reader не обеспечивает.
	maximumFileSize = 1 << 20
	// workerGrantTTL caps the signed worker-credential lifetime at four minutes in the shared time
	// validator; its clock-skew allowance is supplied separately.
	// workerGrantTTL ограничивает срок подписанных учётных данных рабочего процесса четырьмя минутами в
	// общем валидаторе времени; допуск рассинхронизации часов задаётся отдельно.
	workerGrantTTL = 4 * time.Minute
	// controlPlaneSPIFFE pins the expected owner/proof issuer in policy and signing-key trust checks; the
	// literal does not authenticate a transport by itself.
	// controlPlaneSPIFFE закрепляет ожидаемого владельца и подписанта доказательств в политике и проверке
	// доверия ключу; сама строка транспорт не удостоверяет.
	controlPlaneSPIFFE = "spiffe://kodex.local/ns/kodex-system/sa/control-plane"
	// runtimeMaterializationDiagnosticMessage is the fixed warning text for the two materialization proof
	// paths; credentials and arbitrary provider error text are not interpolated.
	// runtimeMaterializationDiagnosticMessage — фиксированный текст предупреждения двух путей доказательств
	// материализации; учётные данные и произвольная ошибка провайдера в него не подставляются.
	runtimeMaterializationDiagnosticMessage = "runtime materialization authority failed"
)

// AuthorityOwner defines domain-owned authority resolution, durable worker-grant acceptance, proof
// revision allocation and owner readiness; request fields are not a substitute for these decisions.
// AuthorityOwner задаёт принадлежащие домену разрешение полномочий, устойчивое принятие grant, выделение
// ревизии доказательства и готовность владельца; поля запроса не заменяют эти решения.
type AuthorityOwner interface {
	// ResolveProofAuthority resolves actor, organization, project and optional runtime-execution provenance
	// from the verified principal and owner state before proof claims are constructed.
	// ResolveProofAuthority разрешает исполнителя, организацию, проект и необязательное происхождение
	// runtime-выполнения по проверенному субъекту и состоянию владельца до построения доказательства.
	ResolveProofAuthority(context.Context, platformrepo.ProofPrincipalInput) (platformrepo.ProofAuthority, error)
	// AcceptWorkerGrant records or checks the exact workload/instance/envelope/generation/time binding at
	// the durable owner boundary; successful signature verification alone does not perform this acceptance.
	// AcceptWorkerGrant фиксирует либо проверяет точную привязку нагрузки, экземпляра, конверта, поколения и
	// срока у устойчивого владельца; одной проверки подписи для такого принятия недостаточно.
	AcceptWorkerGrant(context.Context, platformrepo.WorkerGrantInput) error
	// NextAuthorityProofRevision allocates the next owner-controlled proof revision before signing; it is
	// distinct from signer and caller-credential generations.
	// NextAuthorityProofRevision выделяет следующую управляемую владельцем ревизию доказательства до
	// подписи; это не поколение подписанта или учётных данных вызывающего.
	NextAuthorityProofRevision(context.Context) (uint64, error)
	// Ready checks the authoritative domain dependency before resolver readiness can succeed; it does not
	// itself issue a proof.
	// Ready проверяет авторитетную доменную зависимость до успешной готовности resolver; самого выпуска
	// доказательства здесь нет.
	Ready(context.Context) error
}

// Config supplies policy/signing/trust paths, workload-key files, optional readiness grant, OIDC
// settings and evaluation clock; constructor validation does not make these fields authority claims.
// Config задаёт пути политики, подписи и доверия, файлы ключей нагрузок, необязательный grant
// готовности, настройки OIDC и часы проверки; валидация конструктора не превращает поля в полномочия.
type Config struct {
	PolicyFile, SignerPrivateJWKFile, SignerTrustFile string
	WorkerGrantTrustFiles                             map[string]string
	ReadinessWorkerGrantFile                          string
	OIDC                                              oidcverifier.Config
	Now                                               func() time.Time
}

// ResolveInput carries transport-observed peer and credential plus operation, project, digest and
// request IDs. ResourceReference is present in the input but not consumed by Resolve; owner-derived
// output supplies identity.
// ResolveInput содержит наблюдаемую транспортом сторону, учётные данные, операцию, проект, хеш и ID
// запроса. ResourceReference присутствует во входе, но не используется Resolve; личность задаёт
// результат владельца.
type ResolveInput struct {
	PeerSPIFFEID, Authorization, OperationID, ResourceReference, ProjectReference string
	IdempotencyKey, CorrelationID, RequestDigestSHA256                            string
}

// ResolveResult returns the sensitive signed proof with compact-token digest, expiry and independent
// proof/policy/signer revisions; a response is not downstream authorization-context consumption.
// ResolveResult возвращает конфиденциальное подписанное доказательство с хешем компактного токена,
// сроком и независимыми ревизиями доказательства, политики и подписанта; ответ не является потреблением
// downstream-контекста авторизации.
type ResolveResult struct {
	CompactJWS, DigestSHA256                        string
	ExpiresAt                                       time.Time
	ProofRevision, PolicyRevision, SignerGeneration uint64
}

// Readiness exposes configured policy and signer versions with safe digests, not a proof token or a new
// authorization grant.
// Readiness возвращает версии настроенной политики и подписанта с безопасными хешами, не токен
// доказательства и не новое разрешение.
type Readiness struct {
	PolicyRevision, SignerGeneration           uint64
	PolicyDigestSHA256, SignerThumbprintSHA256 string
}

// Service owns indexed resolver policy, signing/trust material and the shared OIDC verifier while
// delegating persistent grant, revision and authority decisions to AuthorityOwner. Private key and
// credential-bearing results must not be logged.
// Service владеет индексированной политикой resolver, материалами подписи и доверия и общим OIDC
// verifier, передавая устойчивые решения по grant, ревизиям и полномочиям AuthorityOwner. Закрытый ключ
// и результаты с учётными полномочиями не журналируются.
type Service struct {
	owner            AuthorityOwner
	oidc             *oidcverifier.Verifier
	policy           policyDocument
	policyDigest     string
	bindings         map[string]operationBinding
	producers        map[string]proofProducer
	signer           internalrpcauth.ES256Key
	signerGeneration uint64
	signerThumbprint string
	workerKeys       map[string]internalrpcauth.ES256Key
	readinessGrant   string
	oidcIssuer       string
	oidcAudience     string
	now              func() time.Time
}

// policyDocument is this resolver's strict unsigned configuration envelope with version/ABI pins and
// local producer/binding types; it is not self-authenticating signed authority.
// policyDocument — строгая неподписанная оболочка конфигурации resolver с привязками версии и ABI и
// локальными типами производителей и операций; сама она не удостоверяет полномочия.
type policyDocument struct {
	Version        int    `json:"v"`
	PolicyRevision uint64 `json:"policy_revision"`
	Policy         struct {
		AuthorityABIVersion     uint32             `json:"authority_abi_version"`
		TrustDomain             string             `json:"trust_domain"`
		DefaultDecision         string             `json:"default_decision"`
		TokenTTLSeconds         int64              `json:"token_ttl_seconds"`
		AllowedClockSkewSeconds int64              `json:"allowed_clock_skew_seconds"`
		MaxCompactJWSBytes      int                `json:"max_compact_jws_bytes"`
		ProofProducers          []proofProducer    `json:"authority_proof_producers"`
		OperationBindings       []operationBinding `json:"operation_bindings"`
	} `json:"policy"`
}

// proofProducer retains this domain's nominal policy input with the shared JSON
// field layout; indexPolicy and request-time checks still own credential,
// operation, identity and authority validation before any proof is signed.
// proofProducer сохраняет отдельный доменный вход политики с общей формой JSON;
// indexPolicy и проверки запроса по-прежнему проверяют учётные данные, операцию,
// личность и полномочия до подписи любого доказательства.
type proofProducer policydeclaration.ProofProducer

// operationBinding declares the exact caller/target/RPC/permission and proof producer together with
// local peer, request and optional continuation profiles; indexPolicy decides which root operations
// enter the resolvable map.
// operationBinding объявляет точные стороны, RPC, право и производителя доказательств вместе с
// локальными сторонами, профилем запроса и необязательным продолжением; indexPolicy решает, какие
// корневые операции входят в доступную карту.
type operationBinding struct {
	OperationID         string               `json:"operation_id"`
	CallerWorkloadID    string               `json:"caller_workload_id"`
	CallerSPIFFEID      string               `json:"caller_spiffe_id"`
	Issuer              string               `json:"issuer"`
	TargetWorkloadID    string               `json:"target_workload_id"`
	TargetSPIFFEID      string               `json:"target_spiffe_id"`
	Audience            string               `json:"audience"`
	FullMethod          string               `json:"full_method"`
	TargetTLSServerName string               `json:"target_tls_server_name"`
	TargetTrustBundleID string               `json:"target_trust_bundle_id"`
	Permission          string               `json:"permission"`
	ProducerID          string               `json:"authority_proof_producer_id"`
	AuthoritySources    []string             `json:"authority_sources"`
	ProjectRequired     bool                 `json:"project_required"`
	LocalCaller         localPeer            `json:"local_caller"`
	LocalTarget         localPeer            `json:"local_target"`
	RequestProfile      requestProfile       `json:"request_profile"`
	Continuation        *continuationProfile `json:"continuation,omitempty"`
}

// requestProfile keeps this resolver's nominal request-binding declaration over
// the shared field layout; validRequestProfile still enforces its closed modes
// and required/forbidden dimensions before a binding can be used.
// requestProfile сохраняет отдельную декларацию привязки resolver на общей форме;
// validRequestProfile по-прежнему проверяет закрытые режимы и обязательность
// либо запрет измерений до использования привязки.
type requestProfile policydeclaration.RequestProfile

// continuationProfile retains the domain's exact parent-operation/method input;
// type reuse does not accept a parent context or create delegation authority.
// continuationProfile сохраняет точный доменный ввод родительской операции
// и метода; общий тип не принимает контекст родителя и не выдаёт делегирование.
type continuationProfile policydeclaration.ContinuationProfile

// localPeer retains configured Unix-peer pins as a distinct policy type, not
// observed credentials; transport authentication remains independently required.
// localPeer сохраняет настройки Unix-стороны отдельным типом политики, а не
// наблюдаемыми учётными данными; проверка транспорта требуется независимо.
type localPeer policydeclaration.LocalPeer

// provenance records the source/reference/version/digest of an owner-resolved identity; populating these
// fields does not establish source eligibility.
// provenance фиксирует источник, ссылку, версию и хеш разрешённой владельцем личности; заполнение полей
// не устанавливает допустимость источника.
type provenance authorityclaims.Provenance

// identity pairs an owner-resolved identifier with provenance in outgoing proof claims; it is not an
// identifier copied as authority from the request body.
// identity связывает разрешённый владельцем идентификатор с происхождением в исходящем доказательстве;
// это не идентификатор из тела запроса, самовольно ставший полномочием.
type identity authorityclaims.Identity[provenance]

// workload carries the configured caller workload and exact SPIFFE ID in signed proof claims;
// observed-peer verification happens before this value is emitted.
// workload содержит настроенную нагрузку вызывающего и точный SPIFFE ID в подписанном доказательстве;
// наблюдаемая сторона проверяется до выдачи значения.
type workload authorityclaims.Workload

// authority groups actor kind, owner-resolved actor/tenant and optional project for an outgoing proof;
// operation permission remains in policy rather than an extra field here.
// authority объединяет вид исполнителя, разрешённых владельцем исполнителя и организацию и
// необязательный проект исходящего доказательства; право операции остаётся в политике, не дополнительном
// поле этой структуры.
type authority authorityclaims.Authority[identity]

// credentialAuthentication preserves verified human authentication time and optional ACR/AMR in the
// proof; the metadata does not reauthenticate or elevate the principal.
// credentialAuthentication сохраняет проверенное время аутентификации человека и необязательные ACR и
// AMR в доказательстве; метаданные не аутентифицируют повторно и не повышают права.
type credentialAuthentication authorityclaims.CredentialAuthentication

// proofClaims is the outgoing owner-signed authority-proof payload binding caller, operation, resolved
// authority, independent revisions, request profile and one-time identity/time fields; encoding alone
// supplies no signature.
// proofClaims — исходящее подписываемое владельцем доказательство с привязками вызывающего, операции,
// разрешённых полномочий, независимых ревизий, профиля запроса и одноразовых ID и сроков; одно
// кодирование подпись не даёт.
type proofClaims authorityclaims.Proof[workload, authority, credentialAuthentication]

// workerGrantClaims is the unverified decoded workload credential envelope until signature, lifetime,
// instance, generation and exact producer binding checks succeed; its actor/tenant-looking fields are
// not used as root authority.
// workerGrantClaims — непроверенный декодированный конверт нагрузки до успешных проверок подписи, срока,
// экземпляра, поколения и точного производителя; похожие на исполнителя и организацию поля не
// используются как корневые полномочия.
type workerGrantClaims internalrpcauth.PlatformWorkerGrantClaims

// New validates dependencies, reads policy and signing/trust inputs, creates an OIDC verifier and
// indexes the closed policy before returning. It closes the newly created verifier if indexing fails; no
// proof is issued by construction.
// New проверяет зависимости, читает политику и материалы подписи и доверия, создаёт OIDC verifier и
// индексирует закрытую политику до возврата. При ошибке индексации новый verifier закрывается;
// конструктор доказательств не выпускает.
func New(ctx context.Context, owner AuthorityOwner, config Config) (*Service, error) {
	if ctx == nil || owner == nil || config.PolicyFile == "" || config.SignerPrivateJWKFile == "" || config.SignerTrustFile == "" || len(config.WorkerGrantTrustFiles) == 0 {
		return nil, errors.New("authority proof service configuration is invalid")
	}
	raw, err := readBounded(config.PolicyFile)
	if err != nil {
		return nil, errors.New("read authority proof policy")
	}
	var document policyDocument
	if err := internalrpcauth.DecodeStrictJSON(raw, &document); err != nil || document.Version != 1 || document.PolicyRevision == 0 || document.Policy.AuthorityABIVersion != internalrpcauth.AuthorityABIVersion || document.Policy.TrustDomain != "kodex.local" || document.Policy.DefaultDecision != "DENY" {
		return nil, errors.New("authority proof policy is invalid")
	}
	digest := sha256.Sum256(raw)
	privateRaw, err := readBounded(config.SignerPrivateJWKFile)
	if err != nil {
		return nil, errors.New("read authority proof signing key")
	}
	signer, err := internalrpcauth.ParsePrivateJWK(privateRaw)
	if err != nil {
		return nil, errors.New("parse authority proof signing key")
	}
	generation, err := verifySignerTrust(config.SignerTrustFile, signer, time.Now().UTC())
	if err != nil {
		return nil, err
	}
	thumbprint, err := internalrpcauth.PublicJWKThumbprintSHA256(signer.PublicOnly())
	if err != nil {
		return nil, errors.New("derive authority proof signer thumbprint")
	}
	workerKeys := make(map[string]internalrpcauth.ES256Key, len(config.WorkerGrantTrustFiles))
	for workloadID, path := range config.WorkerGrantTrustFiles {
		keyRaw, readErr := readBounded(path)
		if readErr != nil {
			return nil, fmt.Errorf("read %s worker grant trust", workloadID)
		}
		key, parseErr := internalrpcauth.ParsePublicJWK(keyRaw)
		if parseErr != nil {
			return nil, fmt.Errorf("parse %s worker grant trust", workloadID)
		}
		workerKeys[workloadID] = key
	}
	oidc, err := oidcverifier.New(ctx, config.OIDC)
	if err != nil {
		return nil, errors.New("initialize authority OIDC verifier")
	}
	service := &Service{
		owner: owner, oidc: oidc, policy: document, policyDigest: hex.EncodeToString(digest[:]),
		signer: signer, signerGeneration: generation, signerThumbprint: thumbprint,
		workerKeys: workerKeys, oidcIssuer: config.OIDC.Issuer, oidcAudience: config.OIDC.Audience,
		readinessGrant: config.ReadinessWorkerGrantFile, now: config.Now,
	}
	if service.now == nil {
		service.now = time.Now
	}
	if err := service.indexPolicy(); err != nil {
		oidc.Close()
		return nil, err
	}
	return service, nil
}

// indexPolicy validates configured producer identities and credential kinds, then indexes eligible root
// bindings with exact caller/proof/request-profile links. Continuation declarations are shape-checked
// separately and not inserted as independently resolvable root operations.
// indexPolicy проверяет личности производителей и виды учётных данных, затем индексирует допустимые
// корневые привязки с точными связями вызывающего, доказательства и профиля. Декларации продолжений
// проверяются отдельно и не добавляются как самостоятельно разрешаемые корневые операции.
func (service *Service) indexPolicy() error {
	service.bindings = make(map[string]operationBinding, len(service.policy.Policy.OperationBindings))
	service.producers = make(map[string]proofProducer, len(service.policy.Policy.ProofProducers))
	for _, producer := range service.policy.Policy.ProofProducers {
		if producer.ProducerID == "" || producer.OwnerWorkloadID != "control-plane" || producer.OwnerSPIFFEID != controlPlaneSPIFFE || producer.AuthorityProofIssuer != controlPlaneSPIFFE || producer.AuthorityProofMaxAgeSeconds < 1 || producer.AuthorityProofMaxAgeSeconds > 15 {
			return errors.New("authority proof producer is invalid")
		}
		if producer.ApplicationCredentialMetadata != "authorization" ||
			producer.CallerWorkloadID == "control-api-gateway" &&
				(producer.ApplicationCredential != "OIDC_BEARER" || producer.ApplicationCredentialIssuer != service.oidcIssuer || producer.ApplicationCredentialAudience != service.oidcAudience) ||
			producer.CallerWorkloadID != "control-api-gateway" && producer.ApplicationCredential != "PLATFORM_WORKER_GRANT" {
			return errors.New("authority proof application credential binding is invalid")
		}
		if _, duplicate := service.producers[producer.ProducerID]; duplicate {
			return errors.New("authority proof producer is duplicated")
		}
		service.producers[producer.ProducerID] = producer
	}
	for _, binding := range service.policy.Policy.OperationBindings {
		if binding.Continuation != nil {
			if binding.ProducerID != "" || binding.OperationID == "" || binding.CallerWorkloadID == "" ||
				binding.CallerSPIFFEID == "" || binding.Audience == "" || binding.FullMethod == "" ||
				binding.Permission == "" || binding.Continuation.ParentOperationID == "" ||
				binding.Continuation.ParentFullMethod == "" || !validRequestProfile(binding.RequestProfile) {
				return errors.New("authority continuation operation binding is invalid")
			}
			continue
		}
		producer, ok := service.producers[binding.ProducerID]
		if !ok || binding.OperationID == "" || binding.CallerWorkloadID != producer.CallerWorkloadID || binding.CallerSPIFFEID != producer.CallerSPIFFEID || binding.Audience == "" || binding.FullMethod == "" || binding.Permission == "" || !contains(producer.AllowedOperationIDs, binding.OperationID) || !validRequestProfile(binding.RequestProfile) {
			return errors.New("authority proof operation binding is invalid")
		}
		if _, duplicate := service.bindings[binding.OperationID]; duplicate {
			return errors.New("authority proof operation binding is duplicated")
		}
		service.bindings[binding.OperationID] = binding
	}
	return nil
}

// Resolve checks the indexed operation, observed peer, project and digest profile, verifies an OIDC
// token or worker grant and asks the owner for authority/revision before signing a short-lived proof.
// Worker acceptance and revision allocation can precede a later failure; this flow is not one
// transaction or automatic retry.
// Resolve проверяет индексированную операцию, сторону, проект и профиль хеша, проверяет токен OIDC либо
// grant нагрузки и получает у владельца полномочия и ревизию до краткоживущей подписи. Принятие grant и
// выделение ревизии могут предшествовать последующей ошибке; это не одна транзакция и не автоматический
// повтор.
func (service *Service) Resolve(ctx context.Context, input ResolveInput) (ResolveResult, error) {
	binding, ok := service.bindings[input.OperationID]
	if !ok {
		return ResolveResult{}, errors.New("operation is not permitted")
	}
	producer := service.producers[binding.ProducerID]
	if input.PeerSPIFFEID != binding.CallerSPIFFEID || input.PeerSPIFFEID != producer.CallerSPIFFEID || uuid.Validate(input.IdempotencyKey) != nil || uuid.Validate(input.CorrelationID) != nil {
		return ResolveResult{}, errors.New("authority proof request binding is invalid")
	}
	if binding.ProjectRequired && input.ProjectReference == "" || !binding.ProjectRequired && input.ProjectReference != "" {
		return ResolveResult{}, errors.New("authority proof project binding is invalid")
	}
	if binding.RequestProfile.Mode == internalrpcauth.RequestBindingUnary && !validDigest(input.RequestDigestSHA256) ||
		binding.RequestProfile.Mode == internalrpcauth.RequestBindingStream && input.RequestDigestSHA256 != "" {
		return ResolveResult{}, errors.New("authority proof request digest binding is invalid")
	}
	credential := strings.TrimPrefix(input.Authorization, "Bearer ")
	if credential == input.Authorization || credential == "" || strings.TrimSpace(credential) != credential {
		return ResolveResult{}, errors.New("application credential is invalid")
	}
	principal := platformrepo.ProofPrincipalInput{CallerWorkload: producer.CallerWorkloadID, Operation: input.OperationID, ProjectRef: input.ProjectReference, RequestDigestSHA256: input.RequestDigestSHA256}
	actorKind := "SERVICE"
	actorSource := "DOMAIN_STATE"
	actorReference := ""
	actorRevision := uint64(1)
	callerCredentialRevision := uint64(1)
	var workerGrantExpiresAt time.Time
	credentialDigest := sha256.Sum256([]byte(credential))
	if producer.CallerWorkloadID == "control-api-gateway" {
		verified, err := service.oidc.VerifyToken(ctx, credential)
		if errors.Is(err, oidcverifier.ErrSigningKeysUnavailable) {
			return ResolveResult{}, domainerrs.ErrUnavailable
		}
		if err != nil {
			return ResolveResult{}, domainerrs.ErrUnauthorized
		}
		principal.ExternalActorID, principal.ExternalTenantID = verified.Subject, verified.OrganizationID
		principal.ExternalDisplayName, principal.ExternalEmailHint = verified.DisplayName, verified.EmailHint
		principal.ExternalIssuer, principal.ExternalGroups = verified.Issuer, verified.Groups
		principal.ExternalSessionRevision, principal.OwnerClaim = verified.SessionRevision, verified.OwnerClaim
		principal.ExternalAuthenticatedAt, principal.ExternalACR, principal.ExternalAMR = verified.AuthenticatedAt, verified.ACR, append([]string(nil), verified.AMR...)
		actorKind, actorSource, actorReference, actorRevision = "HUMAN", "OIDC_SESSION", verified.SessionID, verified.SessionRevision
		callerCredentialRevision = verified.SessionRevision
	} else {
		grant, err := service.verifyWorkerGrant(credential, producer)
		if err != nil {
			logRuntimeMaterializationFailure(ctx, input, "grant_verify")
			return ResolveResult{}, err
		}
		if err := service.owner.AcceptWorkerGrant(ctx, platformrepo.WorkerGrantInput{
			WorkloadID: producer.CallerWorkloadID, Revision: grant.Revision,
			InstanceID: grant.InstanceID, EnvelopeSHA256: hex.EncodeToString(credentialDigest[:]),
			CredentialGeneration: grant.CredentialGeneration,
			IssuedAt:             time.Unix(grant.IssuedAt, 0), ExpiresAt: time.Unix(grant.ExpiresAt, 0),
		}); err != nil {
			logRuntimeMaterializationFailure(ctx, input, "grant_accept")
			return ResolveResult{}, fmt.Errorf("accept worker grant: %w", err)
		}
		principal.ExternalActorID, principal.ExternalTenantID = "kodex-system-subject", "kodex-installation"
		callerCredentialRevision = grant.CredentialGeneration
		workerGrantExpiresAt = time.Unix(grant.ExpiresAt, 0)
	}
	resolved, err := service.owner.ResolveProofAuthority(ctx, principal)
	if err != nil {
		logRuntimeMaterializationFailure(ctx, input, "owner_resolve")
		return ResolveResult{}, fmt.Errorf("resolve proof authority: %w", err)
	}
	if actorReference == "" {
		actorReference = resolved.ActorID
		actorRevision = resolved.ActorVersion
	}
	actor := identity{ID: resolved.ActorID, Provenance: provenance{Source: actorSource, Reference: actorReference, Revision: actorRevision, DigestSHA256: hex.EncodeToString(credentialDigest[:])}}
	if runtimeMaterializationProofRequired(input.OperationID) || resolved.RuntimeExecution != nil {
		actor, actorKind, err = runtimeExecutionActor(resolved, producer.CallerWorkloadID, input.OperationID)
		if err != nil {
			logRuntimeMaterializationFailure(ctx, input, "execution_actor")
			return ResolveResult{}, err
		}
	}
	tenant := domainIdentity("DOMAIN_STATE", resolved.OrganizationID, resolved.OrganizationVersion)
	proofAuthority := authority{ActorKind: actorKind, Actor: actor, Tenant: tenant}
	if binding.ProjectRequired {
		if resolved.ProjectID == "" {
			return ResolveResult{}, errors.New("project authority is unavailable")
		}
		project := domainIdentity("DOMAIN_STATE", resolved.ProjectID, resolved.ProjectVersion)
		proofAuthority.Project = &project
	}
	revision, err := service.owner.NextAuthorityProofRevision(ctx)
	if err != nil {
		logRuntimeMaterializationFailure(ctx, input, "proof_revision")
		return ResolveResult{}, fmt.Errorf("advance authority proof revision: %w", err)
	}
	now := service.now().UTC().Truncate(time.Second)
	expiresAt := now.Add(time.Duration(producer.AuthorityProofMaxAgeSeconds) * time.Second)
	if resolved.RuntimeExecution != nil {
		expiresAt, err = runtimeExecutionProofExpiry(now, expiresAt, resolved.RuntimeExecution.ExpiresAt, workerGrantExpiresAt)
		if err != nil {
			logRuntimeMaterializationFailure(ctx, input, "expiry")
			return ResolveResult{}, err
		}
	}
	var authentication *credentialAuthentication
	if actorKind == "HUMAN" && !principal.ExternalAuthenticatedAt.IsZero() {
		authentication = &credentialAuthentication{AuthenticatedAt: principal.ExternalAuthenticatedAt.Unix(), ACR: principal.ExternalACR, AMR: append([]string(nil), principal.ExternalAMR...)}
	}
	claims := proofClaims{
		Version: 1, Issuer: producer.AuthorityProofIssuer, Audience: producer.AuthorityProofAudience,
		Caller:      workload{WorkloadID: producer.CallerWorkloadID, SPIFFEID: producer.CallerSPIFFEID},
		OperationID: input.OperationID, AuthorizationContextAudience: binding.Audience, Authority: proofAuthority,
		ProofRevision: revision, SignerGeneration: service.signerGeneration, CallerCredentialRevision: callerCredentialRevision, CredentialAuthentication: authentication, JTI: uuid.NewString(),
		IssuedAt: now.Unix(), NotBefore: now.Unix(), ExpiresAt: expiresAt.Unix(),
		RequestDigestSHA256: input.RequestDigestSHA256, RequestBindingMode: binding.RequestProfile.Mode,
		AuthorityABIVersion: internalrpcauth.AuthorityABIVersion,
	}
	compact, err := internalrpcauth.SignCanonicalJSON(claims, service.signer, internalrpcauth.ProtectedHeaderExpectation{Type: internalrpcauth.AuthorityProofProtectedType, KeyID: service.signer.KeyID})
	if err != nil {
		logRuntimeMaterializationFailure(ctx, input, "sign")
		return ResolveResult{}, errors.New("sign authority proof")
	}
	proofDigest := sha256.Sum256([]byte(compact))
	return ResolveResult{CompactJWS: compact, DigestSHA256: hex.EncodeToString(proofDigest[:]), ExpiresAt: expiresAt, ProofRevision: revision, PolicyRevision: service.policy.PolicyRevision, SignerGeneration: service.signerGeneration}, nil
}

// logRuntimeMaterializationFailure emits a fixed warning only for the two exact materialization
// operations, with the caller-supplied internal stage and an optional validated digest; it never
// includes the credential or error cause.
// logRuntimeMaterializationFailure выдаёт фиксированное предупреждение только двух точных операций
// материализации со стадией внутреннего вызова и необязательным проверенным хешем, без учётных данных и
// причины ошибки.
func logRuntimeMaterializationFailure(ctx context.Context, input ResolveInput, stage string) {
	if !runtimeMaterializationProofRequired(input.OperationID) {
		return
	}
	attributes := []any{"operation", input.OperationID, "stage", stage}
	if validDigest(input.RequestDigestSHA256) {
		attributes = append(attributes, "request_digest_sha256", input.RequestDigestSHA256)
	}
	slog.WarnContext(ctx, runtimeMaterializationDiagnosticMessage, attributes...)
}

// verifyWorkerGrant selects registered workload trust before canonical JWS verification, then enforces
// lifetime, supported instance format, ABI, issuer/audience/workload/SPIFFE, revision, key generation
// and JTI, rejecting project or owner escalation. Persistent acceptance remains a separate owner call.
// verifyWorkerGrant выбирает зарегистрированное доверие нагрузки до проверки канонического JWS, затем
// проверяет срок, формат экземпляра, ABI, подписанта, аудиторию, нагрузку, SPIFFE, ревизию, поколение
// ключа и JTI, отклоняя проект и повышение до владельца. Устойчивое принятие выполняется отдельным
// вызовом владельца.
func (service *Service) verifyWorkerGrant(compact string, producer proofProducer) (workerGrantClaims, error) {
	key, ok := service.workerKeys[producer.CallerWorkloadID]
	if !ok {
		return workerGrantClaims{}, errors.New("worker grant trust is unavailable")
	}
	verified, err := internalrpcauth.VerifyCanonicalJSON(compact, key, internalrpcauth.ProtectedHeaderExpectation{Type: workerGrantType, KeyID: key.KeyID})
	if err != nil {
		return workerGrantClaims{}, errors.New("worker application grant is rejected")
	}
	var claims workerGrantClaims
	if err := internalrpcauth.DecodeCanonicalJSON(verified.CanonicalPayload, &claims); err != nil {
		return workerGrantClaims{}, errors.New("worker application grant claims are rejected")
	}
	now := service.now().UTC().Truncate(time.Second)
	credentialGeneration, generationErr := internalrpcauth.KeyGeneration(key.KeyID)
	if err := internalrpcauth.ValidateTimes(now, time.Unix(claims.IssuedAt, 0), time.Unix(claims.NotBefore, 0), time.Unix(claims.ExpiresAt, 0), workerGrantTTL, 5*time.Second); err != nil ||
		generationErr != nil || !validWorkerGrantInstance(claims) || claims.AuthorityABIVersion != internalrpcauth.AuthorityABIVersion || claims.Issuer != producer.ApplicationCredentialIssuer || claims.Audience != producer.ApplicationCredentialAudience || claims.WorkloadID != producer.CallerWorkloadID || claims.CallerSPIFFEID != producer.CallerSPIFFEID || claims.Revision == 0 || claims.CredentialGeneration != credentialGeneration || uuid.Validate(claims.JTI) != nil || claims.ProjectID != "" || claims.TenantOwner {
		return workerGrantClaims{}, errors.New("worker application grant binding is rejected")
	}
	return claims, nil
}

// RefreshOIDC delegates explicit JWKS refresh to the shared verifier with the supplied context; it does
// not resolve domain authority or issue proof claims.
// RefreshOIDC передаёт явное обновление JWKS общему verifier с указанным контекстом без разрешения
// доменных полномочий и выпуска доказательства.
func (service *Service) RefreshOIDC(ctx context.Context) error { return service.oidc.Refresh(ctx) }

// VerifyToken delegates token verification to the existing OIDC cache and returns its principal without
// issuing a proof or performing owner-domain authorization.
// VerifyToken передаёт проверку токена существующему кэшу OIDC и возвращает субъект без выпуска
// доказательства и доменной авторизации владельцем.
func (service *Service) VerifyToken(ctx context.Context, token string) (oidcverifier.Principal, error) {
	return service.oidc.VerifyToken(ctx, token)
}

// Close delegates cleanup of the verifier's idle HTTP connections; it does not close AuthorityOwner or
// revoke previously issued credentials.
// Close передаёт очистку неактивных HTTP-соединений verifier; AuthorityOwner не закрывается и ранее
// выданные учётные данные не отзываются.
func (service *Service) Close() { service.oidc.Close() }

// Ready checks owner readiness and, when configured, verifies and durably accepts the readiness worker
// grant before returning policy/signer metadata; it is not a purely side-effect-free health query or an
// OIDC-key readiness check.
// Ready проверяет готовность владельца и при настройке проверяет и устойчиво принимает grant готовности
// до возврата метаданных политики и подписанта; это не чистый запрос здоровья без эффектов и не проверка
// готовности ключей OIDC.
func (service *Service) Ready(ctx context.Context) (Readiness, error) {
	if err := service.owner.Ready(ctx); err != nil {
		return Readiness{}, err
	}
	if service.readinessGrant != "" {
		raw, err := readBounded(service.readinessGrant)
		if err != nil {
			return Readiness{}, errors.New("read readiness worker grant")
		}
		producer, ok := serviceProducer(service.producers, "control-plane")
		if !ok {
			return Readiness{}, errors.New("readiness worker grant profile is unavailable")
		}
		grant, err := service.verifyWorkerGrant(string(raw), producer)
		if err != nil {
			return Readiness{}, errors.New("readiness worker grant is incompatible")
		}
		digest := sha256.Sum256(raw)
		if err := service.owner.AcceptWorkerGrant(ctx, platformrepo.WorkerGrantInput{WorkloadID: producer.CallerWorkloadID, InstanceID: grant.InstanceID, EnvelopeSHA256: hex.EncodeToString(digest[:]), Revision: grant.Revision, CredentialGeneration: grant.CredentialGeneration, IssuedAt: time.Unix(grant.IssuedAt, 0), ExpiresAt: time.Unix(grant.ExpiresAt, 0)}); err != nil {
			return Readiness{}, errors.New("readiness worker grant is rejected")
		}
	}
	return Readiness{PolicyRevision: service.policy.PolicyRevision, SignerGeneration: service.signerGeneration, PolicyDigestSHA256: service.policyDigest, SignerThumbprintSHA256: service.signerThumbprint}, nil
}

// serviceProducer returns the first map entry matching a workload, with explicit absence; it does not
// establish uniqueness or deterministic preference among multiple matching producer IDs.
// serviceProducer возвращает первую запись карты указанной нагрузки с явным отсутствием; уникальность и
// воспроизводимый выбор между несколькими ID производителей не устанавливаются.
func serviceProducer(values map[string]proofProducer, workloadID string) (proofProducer, bool) {
	for _, value := range values {
		if value.CallerWorkloadID == workloadID {
			return value, true
		}
	}
	return proofProducer{}, false
}

// validRequestProfile accepts only unary-hash or stream-session mode and REQUIRED/FORBIDDEN values for
// every dimension; it checks declarations, not actual request pins.
// validRequestProfile принимает только режим хеша unary либо сессии stream и REQUIRED или FORBIDDEN для
// каждого измерения; проверяются декларации, не фактические привязки запроса.
func validRequestProfile(value requestProfile) bool {
	if value.Mode != internalrpcauth.RequestBindingUnary && value.Mode != internalrpcauth.RequestBindingStream {
		return false
	}
	for _, binding := range []string{value.Resource, value.Version, value.Attempt, value.Idempotency} {
		if binding != internalrpcauth.ProfileBindingRequired && binding != internalrpcauth.ProfileBindingForbidden {
			return false
		}
	}
	return true
}

// validDigest requires exactly 64 lowercase hexadecimal bytes without computing a digest or accepting
// uppercase normalization.
// validDigest требует ровно 64 строчных шестнадцатеричных байта без вычисления хеша и нормализации
// верхнего регистра.
func validDigest(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, character := range value {
		if character < '0' || character > '9' && character < 'a' || character > 'f' {
			return false
		}
	}
	return true
}

// domainIdentity derives a deterministic SHA-256 provenance digest from NUL-separated source, identifier
// and revision while preserving the supplied identity; authority must already come from the owner.
// domainIdentity вычисляет воспроизводимый SHA-256 происхождения из разделённых NUL источника,
// идентификатора и ревизии, сохраняя переданную личность; полномочия уже должны происходить от
// владельца.
func domainIdentity(source, id string, revision uint64) identity {
	digest := sha256.Sum256([]byte(source + "\x00" + id + "\x00" + fmt.Sprint(revision)))
	return identity{ID: id, Provenance: provenance{Source: source, Reference: id, Revision: revision, DigestSHA256: hex.EncodeToString(digest[:])}}
}

// readBounded reads the whole configured file, rejects errors/empty/oversized contents and removes at
// most one trailing LF. It does not enforce read-only modes, no-follow or owner checks and is not a
// streaming size limiter.
// readBounded читает весь настроенный файл, отклоняет ошибку, пустое или слишком большое содержимое и
// удаляет не более одного завершающего LF. Режим только для чтения, запрет ссылок и владелец не
// проверяются; это не потоковый ограничитель размера.
func readBounded(path string) ([]byte, error) {
	raw, err := os.ReadFile(path)
	if err != nil || len(raw) == 0 || len(raw) > maximumFileSize {
		return nil, errors.New("protected file is unavailable")
	}
	return bytes.TrimSuffix(raw, []byte{'\n'}), nil
}

// verifySignerTrust reads the shared proof-trust document and selects a CURRENT key with exact fixed
// issuer, gateway audience, purpose, valid time and public-key match, returning its positive generation;
// it does not itself verify a root signature or activate persisted trust.
// verifySignerTrust читает общий документ доверия доказательствам и выбирает CURRENT с точными
// фиксированными подписантом, аудиторией шлюза, назначением, сроком и совпадением открытого ключа,
// возвращая положительное поколение; корневую подпись и устойчивую активацию здесь не проверяют.
func verifySignerTrust(path string, signer internalrpcauth.ES256Key, now time.Time) (uint64, error) {
	raw, err := readBounded(path)
	if err != nil {
		return 0, errors.New("read authority proof signer trust")
	}
	document, decodeErr := internalrpcauth.DecodeAuthorityProofTrustDocument(raw)
	if decodeErr == nil {
		for _, record := range document.Keys {
			key, parseErr := internalrpcauth.ParsePublicJWK(record.JWK)
			if parseErr == nil && record.Issuer == controlPlaneSPIFFE && record.Generation > 0 && record.Status == "CURRENT" && record.Purpose == "AUTHORITY_PROOF" && contains(record.Audiences, "urn:kodex:internal-rpc-authority-issuer:control-api-gateway") && !now.Before(time.Unix(record.NotBefore, 0)) && now.Before(time.Unix(record.NotAfter, 0)) && sameKey(key, signer) {
				return record.Generation, nil
			}
		}
	}
	return 0, errors.New("authority proof signer CURRENT trust binding is rejected")
}

// sameKey compares canonical public-only JWK encodings, including their encoded key identity, and
// rejects marshal failure without inspecting or logging private scalars.
// sameKey сравнивает канонические открытые JWK, включая закодированный идентификатор ключа, и отклоняет
// ошибку кодирования без просмотра или журналирования закрытых скаляров.
func sameKey(left, right internalrpcauth.ES256Key) bool {
	leftRaw, leftErr := internalrpcauth.MarshalPublicJWK(left.PublicOnly())
	rightRaw, rightErr := internalrpcauth.MarshalPublicJWK(right.PublicOnly())
	return leftErr == nil && rightErr == nil && bytes.Equal(leftRaw, rightRaw)
}

// contains checks an exact string element in a small list without substring matching, normalization or
// mutation.
// contains проверяет точное строковое значение небольшого списка без поиска подстроки, нормализации и
// изменения списка.
func contains(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}
