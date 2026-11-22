package value

import "github.com/codex-k8s/kodex/libs/go/managedkubernetes"

// KubernetesIdentityLocator keeps the broker private purpose locator identical to the owner contract.
// It is not a generic runtime-secret descriptor and contains no credential material.
// KubernetesIdentityLocator сохраняет единый с owner контракт приватного purpose locator.
// Это не общий runtime-secret descriptor; credential material в нём отсутствует.
type KubernetesIdentityLocator = managedkubernetes.IdentityLocator

// KubernetesIdentityProvision separates admitted materialization input from a UID/RV execution locator.
// KubernetesIdentityProvision отделяет допущенный materialization input от UID/RV execution locator.
type KubernetesIdentityProvision = managedkubernetes.IdentityProvision

// KubernetesIdentityAcknowledgementRejection shares only the bounded private rejected-ACK receipt.
// KubernetesIdentityAcknowledgementRejection разделяет лишь bounded private rejected-ACK receipt.
type KubernetesIdentityAcknowledgementRejection = managedkubernetes.AcknowledgementRejection

// ErrKubernetesIdentityLocator preserves the shared closed refusal without exposing private pins.
// ErrKubernetesIdentityLocator сохраняет общий закрытый отказ без раскрытия приватных pins.
var ErrKubernetesIdentityLocator = managedkubernetes.ErrIdentityLocator

// KubernetesIdentitySecretName derives only the registered purpose storage name, never authority.
// KubernetesIdentitySecretName выводит только зарегистрированное purpose имя, не полномочия.
func KubernetesIdentitySecretName(ref string, revision int64) (string, error) {
	return managedkubernetes.IdentitySecretName(ref, revision)
}
