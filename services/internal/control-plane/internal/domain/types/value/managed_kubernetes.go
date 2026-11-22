package value

import (
	"time"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
)

// ManagedKubernetesBinding shares strict owner claim locators without importing transport DTOs.
// ManagedKubernetesBinding использует общие strict owner claim locators без transport DTOs.
type ManagedKubernetesBinding = managedkubernetes.Binding

// ManagedKubernetesResourceAddress shares one strict resource locator predicate with the broker.
// ManagedKubernetesResourceAddress использует один strict resource locator predicate с broker.
type ManagedKubernetesResourceAddress = managedkubernetes.ResourceAddress

// ManagedKubernetesDiscoveryAddress shares a closed non-resource locator with the private broker.
// ManagedKubernetesDiscoveryAddress разделяет closed non-resource locator с private broker.
type ManagedKubernetesDiscoveryAddress = managedkubernetes.DiscoveryAddress

// ManagedKubernetesIdentityLocator shares the broker-only purpose storage pins, never a runtime Secret.
// ManagedKubernetesIdentityLocator разделяет broker-only purpose pins; это не runtime Secret.
type ManagedKubernetesIdentityLocator = managedkubernetes.IdentityLocator

// ManagedKubernetesIdentityProvision describes pre-create pins without inventing a Secret UID/RV.
// ManagedKubernetesIdentityProvision описывает pre-create pins без фиктивного Secret UID/RV.
type ManagedKubernetesIdentityProvision = managedkubernetes.IdentityProvision

// ManagedKubernetesAcknowledgementRejection contains only bounded private evidence, not a locator.
// ManagedKubernetesAcknowledgementRejection содержит только bounded private evidence, не locator.
type ManagedKubernetesAcknowledgementRejection = managedkubernetes.AcknowledgementRejection

// ManagedKubernetesTarget pins operator-owned API metadata; ClusterUID is the observed kube-system
// Namespace UID. Source origins describe accepted kubeconfig metadata and are never dialed.
// ManagedKubernetesTarget закрепляет operator-owned API metadata; ClusterUID — observed kube-system
// Namespace UID. Source origins задают допустимые kubeconfig metadata и не используются для dial.
type ManagedKubernetesTarget struct {
	Ref                        string   `json:"ref"`
	ServerOrigin               string   `json:"serverOrigin"`
	TLSServerName              string   `json:"tlsServerName"`
	CertificateAuthoritySHA256 string   `json:"certificateAuthoritySHA256"`
	EmbeddedContext            string   `json:"embeddedContext"`
	ClusterUID                 string   `json:"clusterUID"`
	Revision                   int64    `json:"revision"`
	AddressIPs                 []string `json:"addressIPs"`
	KubeconfigSourceOrigins    []string `json:"kubeconfigSourceOrigins"`
}

// ManagedKubernetesNodeTransport pins an operator-attested node incarnation to a serving key.
// The owner persists retired generations and rejects serving-key reuse across different NodeUIDs.
// This metadata is not an atomic NodeUID precondition provided by Kubernetes itself.
// ManagedKubernetesNodeTransport связывает operator-attested node incarnation с serving key.
// Owner хранит retired generations и отклоняет serving-key reuse для разных NodeUIDs.
// Metadata не является atomic NodeUID precondition, предоставляемым Kubernetes.
type ManagedKubernetesNodeTransport struct {
	Ref                        string                               `json:"ref"`
	TargetRef                  string                               `json:"targetRef"`
	NodeName                   string                               `json:"nodeName"`
	NodeUID                    string                               `json:"nodeUID"`
	ServerOrigin               string                               `json:"serverOrigin"`
	TLSServerName              string                               `json:"tlsServerName"`
	CertificateAuthoritySHA256 string                               `json:"certificateAuthoritySHA256"`
	ServerPublicKeySHA256      string                               `json:"serverPublicKeySHA256"`
	Generation                 int64                                `json:"generation"`
	TargetRevision             int64                                `json:"targetRevision"`
	AddressIPs                 []string                             `json:"addressIPs"`
	CertificateAuthorityPEM    []byte                               `json:"certificateAuthorityPEM"`
	NotBefore                  time.Time                            `json:"notBefore"`
	ExpiresAt                  time.Time                            `json:"expiresAt"`
	Protocol                   managedkubernetes.PrivilegedProtocol `json:"protocol"`
}
