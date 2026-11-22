package entity

import (
	"regexp"
	"time"
)

// brokerIdentityMetadataRef admits only bounded owner-generated opaque metadata references.
// brokerIdentityMetadataRef принимает только bounded owner-generated opaque metadata refs.
var brokerIdentityMetadataRef = regexp.MustCompile(`^[A-Za-z0-9_-]{8,96}$`)

// IntegrationCredentialKind separates generic token delivery from broker-owned identities.
// An empty or unknown value is invalid; changing kind requires a new owner revision.
// IntegrationCredentialKind разделяет доставку обычного токена и broker-owned identities.
// Пустое либо неизвестное значение недопустимо; смена kind требует новой owner revision.
type IntegrationCredentialKind string

const (
	// IntegrationCredentialToken retains the existing exact generic Secret descriptor path.
	// IntegrationCredentialToken сохраняет существующий путь exact generic Secret descriptor.
	IntegrationCredentialToken IntegrationCredentialKind = "TOKEN"
	// IntegrationCredentialBrokerIdentity exposes metadata only to the integration gateway.
	// IntegrationCredentialBrokerIdentity раскрывает integration gateway только метаданные.
	IntegrationCredentialBrokerIdentity IntegrationCredentialKind = "BROKER_IDENTITY"
)

// Valid recognizes a purpose family without asserting that its private consumer is installed.
// Valid распознаёт purpose family без утверждения об установленном private consumer.
func (purpose IntegrationBrokerIdentityPurpose) Valid() bool {
	return purpose == IntegrationBrokerIdentityKubernetes || purpose == IntegrationBrokerIdentitySSH
}

// IntegrationBrokerIdentityPurpose binds metadata to a registered private broker resolver.
// It never authorizes a generic Secret reader, arbitrary transport or caller-selected purpose.
// IntegrationBrokerIdentityPurpose связывает metadata с зарегистрированным private broker resolver.
// Не разрешает generic Secret reader, произвольный transport либо purpose из caller payload.
type IntegrationBrokerIdentityPurpose string

const (
	// IntegrationBrokerIdentityKubernetes keeps kubeconfig within the Kubernetes broker boundary.
	// IntegrationBrokerIdentityKubernetes оставляет kubeconfig внутри Kubernetes broker boundary.
	IntegrationBrokerIdentityKubernetes IntegrationBrokerIdentityPurpose = "KUBERNETES_TARGET"
	// IntegrationBrokerIdentitySSH reserves the separate SSH resolver without granting Kubernetes credit.
	// IntegrationBrokerIdentitySSH выделяет отдельный SSH resolver без выдачи Kubernetes credit.
	IntegrationBrokerIdentitySSH IntegrationBrokerIdentityPurpose = "SSH_TARGET"
)

// IntegrationBrokerIdentityRevision is an immutable metadata locator, not a Secret descriptor.
// Its owner resolves purpose, target and revision before the broker receives private credentials;
// no Kubernetes key, Secret reference, kubeconfig or credential hash is present here.
// IntegrationBrokerIdentityRevision — immutable metadata locator, а не Secret descriptor.
// Владелец разрешает purpose, target и revision до выдачи private credentials broker;
// Kubernetes key, Secret reference, kubeconfig либо credential hash здесь отсутствуют.
type IntegrationBrokerIdentityRevision struct {
	Ref, TargetRef string
	Revision       int64
	Purpose        IntegrationBrokerIdentityPurpose
	CreatedAt      time.Time
}

// ValidMetadata checks only immutable locator syntax and known purpose; current owner access,
// expiry and materialization receipt validation remain mandatory at every authoritative read.
// ValidMetadata проверяет только immutable locator syntax и known purpose; current owner access,
// expiry и materialization receipt проверяются при каждом authoritative read.
func (identity IntegrationBrokerIdentityRevision) ValidMetadata() bool {
	return brokerIdentityMetadataRef.MatchString(identity.Ref) && brokerIdentityMetadataRef.MatchString(identity.TargetRef) &&
		identity.Revision > 0 && identity.Revision <= 9007199254740991 && identity.Purpose.Valid() && !identity.CreatedAt.IsZero()
}

// IntegrationCredentialSource carries exactly one owner-resolved credential representation.
// Consumers validate Kind and reject mixed representations before returning either branch.
// IntegrationCredentialSource переносит ровно одно разрешённое владельцем представление credential.
// Consumers проверяют Kind и отвергают смешанные представления до выдачи любой ветки.
type IntegrationCredentialSource struct {
	Kind           IntegrationCredentialKind          `json:"-"`
	Credential     *IntegrationCredentialRevision     `json:"-"`
	BrokerIdentity *IntegrationBrokerIdentityRevision `json:"-"`
}

// TokenIntegrationCredentialSource wraps existing token metadata without changing its meaning.
// Nil stays absent; the selection service still validates the complete exact descriptor.
// TokenIntegrationCredentialSource оборачивает прежние token metadata без смены их значения.
// Nil остаётся отсутствием; selection service проверяет полный exact descriptor.
func TokenIntegrationCredentialSource(credential *IntegrationCredentialRevision) *IntegrationCredentialSource {
	if credential == nil {
		return nil
	}
	return &IntegrationCredentialSource{Kind: IntegrationCredentialToken, Credential: credential}
}
