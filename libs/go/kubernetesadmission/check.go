// Package kubernetesadmission checks the observed Kubernetes admission gate without publishing resources.
// Package kubernetesadmission проверяет наблюдаемый допуск Kubernetes без публикации ресурсов.
package kubernetesadmission

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"

	admissionv1 "k8s.io/api/admissionregistration/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	admissionclient "k8s.io/client-go/kubernetes/typed/admissionregistration/v1"
)

// Failures binds the caller's fixed error identities to source, read and observed-state failures.
// Every field must be nonnil; provider errors and serialized policy bodies are never returned.
// Failures связывает фиксированные ошибки вызывающего с ошибками source, чтения и observed state.
// Каждое поле обязано быть nonnil; provider errors и сериализованные policy bodies не возвращаются.
type Failures struct {
	Invalid, Unavailable, Conflict error
}

// errInvalidConfiguration rejects an incomplete server-owned checker configuration before reads.
// errInvalidConfiguration отклоняет неполную server-owned конфигурацию проверки до чтений.
var errInvalidConfiguration = errors.New("admission checker dependencies and failure errors are required")

// Check reads the named policy, then binding, then invokes the expected-resource source once.
// It preserves JSON nil/empty distinctions and rejects missing identity/version, nonpositive or
// unobserved generation, missing type checking, warnings and unequal policy/binding specs.
// Failures pass through as the caller's exact fixed errors; no provider error is reflected.
// It performs no writes, retry, background work or authority grant. The caller owns client TLS,
// RBAC, name/source registration, namespace restrictions and the bounded request context.
// Check читает именованную policy, затем binding, после чего однократно вызывает source ожидаемых
// ресурсов. Сохраняет JSON различия nil/empty и отклоняет отсутствующие identity/version,
// неположительное или ненаблюдаемое generation, отсутствие type checking, warnings и различные
// policy/binding specs. Возвращает точные fixed errors вызывающего без отражения provider error.
// Writes, retry, фоновую работу и authority grant не выполняет. Client TLS, RBAC, регистрация
// name/source, namespace ограничения и ограниченный request context принадлежат вызывающему.
func Check(ctx context.Context, client admissionclient.AdmissionregistrationV1Interface, name string,
	source func() (map[string]any, map[string]any), failures Failures,
) error {
	if client == nil || source == nil || failures.Invalid == nil || failures.Unavailable == nil || failures.Conflict == nil {
		return errInvalidConfiguration
	}
	policy, err := client.ValidatingAdmissionPolicies().Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return failures.Unavailable
	}
	binding, err := client.ValidatingAdmissionPolicyBindings().Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return failures.Unavailable
	}
	policySource, bindingSource := source()
	policyRaw, _ := json.Marshal(policySource)
	bindingRaw, _ := json.Marshal(bindingSource)
	var expectedPolicy admissionv1.ValidatingAdmissionPolicy
	var expectedBinding admissionv1.ValidatingAdmissionPolicyBinding
	if json.Unmarshal(policyRaw, &expectedPolicy) != nil || json.Unmarshal(bindingRaw, &expectedBinding) != nil {
		return failures.Invalid
	}
	if policy.UID == "" || policy.ResourceVersion == "" || policy.Generation < 1 || policy.Status.ObservedGeneration != policy.Generation || policy.Status.TypeChecking == nil || len(policy.Status.TypeChecking.ExpressionWarnings) != 0 ||
		binding.UID == "" || binding.ResourceVersion == "" || !SameJSON(policy.Spec, expectedPolicy.Spec) || !SameJSON(binding.Spec, expectedBinding.Spec) {
		return failures.Conflict
	}
	return nil
}

// SameJSON compares exact marshaled bytes, returning false on either marshal failure.
// It does not canonicalize or erase nil/empty distinctions in Kubernetes specs.
// SameJSON сравнивает точные marshaled bytes, возвращая false при ошибке любого marshal.
// Канонизацию и стирание различий nil/empty в Kubernetes specs не выполняет.
func SameJSON(left, right any) bool {
	a, err := json.Marshal(left)
	if err != nil {
		return false
	}
	b, err := json.Marshal(right)
	return err == nil && bytes.Equal(a, b)
}
