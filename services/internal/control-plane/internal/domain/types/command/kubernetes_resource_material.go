package command

import (
	"strings"
	"unicode/utf8"

	apivalidation "k8s.io/apimachinery/pkg/api/validation"
	"k8s.io/apimachinery/pkg/util/validation"
)

// KubernetesResourceMaterialBegin supplies only private content checksum/size and an exact
// Secret destination. Identity/target/subject/policy and material eligibility belong to the owner.
// KubernetesResourceMaterialBegin задаёт только private content checksum/size и exact Secret
// destination. Identity/target/subject/policy и material eligibility разрешает owner.
type KubernetesResourceMaterialBegin struct {
	ConnectionRef, Operation, Namespace, Name, GenerateName, SecretType, PatchType, ContentSHA256 string
	ExpectedConnectionVersion                                                                     int64
	ContentBytes                                                                                  uint32
}

// ValidDestination checks only the finite public Secret coordinates and native patch dialect.
// These syntax checks grant no ownership or effect authority; the locked owner resolves both.
// ValidDestination проверяет лишь finite public Secret coordinates/native patch dialect.
// Syntax checks не дают ownership/effect authority; их разрешает locked owner.
func (input KubernetesResourceMaterialBegin) ValidDestination() bool {
	if len(validation.IsDNS1123Label(input.Namespace)) != 0 || !utf8.ValidString(input.SecretType) || len(input.SecretType) == 0 || len(input.SecretType) > 253 ||
		strings.ContainsAny(input.SecretType, "\x00\r\n") || input.SecretType == "kubernetes.io/service-account-token" {
		return false
	}
	if input.Operation == "kubernetes.create" {
		if (input.Name == "") == (input.GenerateName == "") || input.PatchType != "" {
			return false
		}
		if input.GenerateName != "" {
			return len(input.GenerateName) <= 253 && len(apivalidation.NameIsDNSSubdomain(input.GenerateName, true)) == 0
		}
		return len(validation.IsDNS1123Subdomain(input.Name)) == 0
	}
	if input.GenerateName != "" || len(validation.IsDNS1123Subdomain(input.Name)) != 0 {
		return false
	}
	switch input.Operation {
	case "kubernetes.replace", "kubernetes.apply", "kubernetes.edit":
		return input.PatchType == ""
	case "kubernetes.patch":
		return input.PatchType == "JSON" || input.PatchType == "MERGE" || input.PatchType == "STRATEGIC"
	default:
		return false
	}
}

// KubernetesResourceMaterialRead selects one owner-local artifact or its original Begin key.
// Neither selector grants target access or recovers private material/upload permit.
// KubernetesResourceMaterialRead выбирает один owner-local artifact либо original Begin key.
// Ни один selector не выдаёт target access и не восстанавливает material/upload permit.
type KubernetesResourceMaterialRead struct {
	ConnectionRef, MaterialRef, BeginIdempotencyKey string
}

// KubernetesResourceMaterialCancel addresses one owner-resolved artifact before OCC/idempotency.
// Cancellation closes future use without asserting that a prior remote Secret effect was undone.
// KubernetesResourceMaterialCancel адресует owner-resolved artifact до OCC/idempotency.
// Cancellation закрывает future use без утверждения об отмене prior remote Secret effect.
type KubernetesResourceMaterialCancel struct {
	ConnectionRef, MaterialRef string
}
