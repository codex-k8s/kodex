package managedkubernetes

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth"
	"github.com/google/uuid"
	"k8s.io/apimachinery/pkg/util/validation"
)

const (
	// MaximumResourcePolicyBytes bounds one inline operator profile before decoding or hashing.
	// MaximumResourcePolicyBytes ограничивает один inline operator profile до decoding/hashing.
	MaximumResourcePolicyBytes = 128 << 10
	// StorageProviderCSIAppDataV1 requires exact noncredential CSI data-volume qualification.
	// StorageProviderCSIAppDataV1 требует exact noncredential CSI data-volume qualification.
	StorageProviderCSIAppDataV1 = "CSI_APP_DATA_V1"
	// ControllerConsumerDataOnlyV1 is an operator-qualified nonworkload controller classification.
	// ControllerConsumerDataOnlyV1 — operator-qualified классификация nonworkload controller.
	ControllerConsumerDataOnlyV1 = "DATA_ONLY_V1"
	// ControllerConsumerPodTemplateV1 applies the same registered Pod security policy to an extracted template.
	// ControllerConsumerPodTemplateV1 применяет ту же registered Pod policy к извлечённому template.
	ControllerConsumerPodTemplateV1 = "POD_TEMPLATE_V1"
)

// ErrResourcePolicyInvalid hides operator metadata and never implies provider permission.
// ErrResourcePolicyInvalid скрывает operator metadata и никогда не означает provider permission.
var ErrResourcePolicyInvalid = errors.New("invalid managed Kubernetes resource policy")

// resourcePolicyKind matches the API kind syntax without accepting arbitrary path expressions.
// resourcePolicyKind проверяет API kind syntax без arbitrary path expressions.
var resourcePolicyKind = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9]{0,127}$`)

// ResourcePolicy is bounded operator qualification inside the existing target revision/digest.
// It is not caller input, an independent grant or a schema-derived safety assertion. Fresh
// provider evidence and owner scope are still required for every use; protected namespaces,
// EACH floors, private material and host/credential-deny rules cannot be overridden here.
// ResourcePolicy — bounded operator qualification внутри существующего target revision/digest.
// Это не caller input, не самостоятельный grant и не schema-derived safety assertion. Каждый
// use требует fresh provider evidence и owner scope; protected namespaces, EACH floors, private
// material и host/credential-deny rules здесь не переопределяются.
type ResourcePolicy struct {
	Storage     []StorageQualification    `json:"storage,omitempty"`
	Controllers []ControllerQualification `json:"controllers,omitempty"`
}

// StorageQualification pins an operator-qualified application-data PVC/PV/SC/CSI chain.
// Provisioner annotations and managedFields prove none of these facts. Source/spec/template
// digests are canonical JSON SHA256; the broker compares actual current objects before effect.
// StorageQualification закрепляет operator-qualified application-data PVC/PV/SC/CSI chain.
// Provisioner annotations/managedFields не доказывают эти facts. Source/spec/template digests —
// canonical JSON SHA256; broker сверяет actual current objects до effect.
type StorageQualification struct {
	Provider            string `json:"provider"`
	PVCNamespace        string `json:"pvcNamespace"`
	PVCName             string `json:"pvcName"`
	PVCUID              string `json:"pvcUID"`
	PVName              string `json:"pvName"`
	PVUID               string `json:"pvUID"`
	StorageClassName    string `json:"storageClassName"`
	StorageClassUID     string `json:"storageClassUID"`
	Provisioner         string `json:"provisioner"`
	CSIDriver           string `json:"csiDriver"`
	CSIDriverUID        string `json:"csiDriverUID"`
	CSIDriverSpecSHA256 string `json:"csiDriverSpecSHA256"`
	VolumeSourceSHA256  string `json:"volumeSourceSHA256"`
	MountOptionsSHA256  string `json:"mountOptionsSHA256"`
	VolumeMode          string `json:"volumeMode"`
	MountReadOnly       bool   `json:"mountReadOnly"`
}

// ControllerQualification binds exact CRD/GVR/schema to an operator-reviewed controller and a
// finite consumer/extractor. No executable, arbitrary JSONPath, annotation or caller allow flag
// selects semantics. Full controller template pin includes init/sidecars/SA/env/mounts.
// ControllerQualification связывает exact CRD/GVR/schema с operator-reviewed controller и
// finite consumer/extractor. Executable, arbitrary JSONPath, annotation либо caller allow flag
// не выбирает semantics. Full controller template pin включает init/sidecars/SA/env/mounts.
type ControllerQualification struct {
	Group                 string `json:"group"`
	Version               string `json:"version"`
	Resource              string `json:"resource"`
	Kind                  string `json:"kind"`
	SchemaSHA256          string `json:"schemaSHA256"`
	ControllerKind        string `json:"controllerKind"`
	ControllerNamespace   string `json:"controllerNamespace"`
	ControllerName        string `json:"controllerName"`
	ControllerUID         string `json:"controllerUID"`
	ControllerContainer   string `json:"controllerContainer"`
	ControllerImageDigest string `json:"controllerImageDigest"`
	TemplateSHA256        string `json:"templateSHA256"`
	Consumer              string `json:"consumer"`
	Extractor             string `json:"extractor"`
}

// Validate rejects unknown/ambiguous policy classifications and conflicting pins. It proves
// syntax and consistent operator intent only; import/revision/retirement are owner operations.
// Validate отклоняет unknown/ambiguous policy classifications и conflicting pins. Доказывает
// только syntax/consistent operator intent; import/revision/retirement принадлежат owner.
func (policy ResourcePolicy) Validate() error {
	if len(policy.Storage)+len(policy.Controllers) == 0 || len(policy.Storage) > 32 || len(policy.Controllers) > 16 {
		return ErrResourcePolicyInvalid
	}
	claims, volumes, classes, resources, controllers := map[string]bool{}, map[string]bool{}, map[string]string{}, map[string]bool{}, map[string]string{}
	for _, qualification := range policy.Storage {
		if qualification.Validate() != nil {
			return ErrResourcePolicyInvalid
		}
		claim := qualification.PVCNamespace + "/" + qualification.PVCName
		classPin := qualification.StorageClassUID + "/" + qualification.Provisioner + "/" + qualification.CSIDriver + "/" + qualification.CSIDriverUID + "/" + qualification.CSIDriverSpecSHA256
		if claims[claim] || volumes[qualification.PVName] || classes[qualification.StorageClassName] != "" && classes[qualification.StorageClassName] != classPin {
			return ErrResourcePolicyInvalid
		}
		claims[claim], volumes[qualification.PVName], classes[qualification.StorageClassName] = true, true, classPin
	}
	for _, qualification := range policy.Controllers {
		if qualification.Validate() != nil {
			return ErrResourcePolicyInvalid
		}
		resource := qualification.Group + "/" + qualification.Version + "/" + qualification.Resource
		controller := qualification.ControllerKind + "/" + qualification.ControllerNamespace + "/" + qualification.ControllerName
		controllerPin := qualification.ControllerUID + "/" + qualification.ControllerContainer + "/" + qualification.ControllerImageDigest + "/" + qualification.TemplateSHA256
		if resources[resource] || controllers[controller] != "" && controllers[controller] != controllerPin {
			return ErrResourcePolicyInvalid
		}
		resources[resource], controllers[controller] = true, controllerPin
	}
	raw, err := json.Marshal(policy)
	if err != nil || len(raw) > MaximumResourcePolicyBytes {
		return ErrResourcePolicyInvalid
	}
	return nil
}

// Validate checks exact application-data storage locators and the finite supported provider.
// Matching hashes do not replace live PVC/PV claimRef/SC/driver and no-secret checks.
// Validate проверяет exact application-data storage locators и finite supported provider.
// Hash match не заменяет live PVC/PV claimRef/SC/driver и no-secret checks.
func (policy StorageQualification) Validate() error {
	if policy.Provider != StorageProviderCSIAppDataV1 || len(validation.IsDNS1123Label(policy.PVCNamespace)) != 0 ||
		len(validation.IsDNS1123Subdomain(policy.PVCName)) != 0 || len(validation.IsDNS1123Subdomain(policy.PVName)) != 0 ||
		len(validation.IsDNS1123Subdomain(policy.StorageClassName)) != 0 || len(validation.IsDNS1123Subdomain(policy.CSIDriver)) != 0 ||
		policy.Provisioner != policy.CSIDriver || !resourcePolicyUID(policy.PVCUID) || !resourcePolicyUID(policy.PVUID) ||
		!resourcePolicyUID(policy.StorageClassUID) || !resourcePolicyUID(policy.CSIDriverUID) ||
		!ValidDigest(policy.CSIDriverSpecSHA256) || !ValidDigest(policy.VolumeSourceSHA256) || !ValidDigest(policy.MountOptionsSHA256) ||
		policy.VolumeMode != "Filesystem" && policy.VolumeMode != "Block" {
		return ErrResourcePolicyInvalid
	}
	return nil
}

// Validate accepts only explicitly registered controller consumers and fixed template locations.
// An object schema alone cannot declare a controller safe or override a built-in Pod deny.
// Validate допускает только explicitly registered controller consumers и fixed template paths.
// Object schema сама не объявляет controller safe и не отменяет built-in Pod deny.
func (policy ControllerQualification) Validate() error {
	address := ResourceAddress{Group: policy.Group, Version: policy.Version, Resource: policy.Resource}
	if address.Validate() != nil || policy.Group == "" || !resourcePolicyKind.MatchString(policy.Kind) || !ValidDigest(policy.SchemaSHA256) ||
		policy.ControllerKind != "Deployment" && policy.ControllerKind != "StatefulSet" ||
		len(validation.IsDNS1123Label(policy.ControllerNamespace)) != 0 || len(validation.IsDNS1123Subdomain(policy.ControllerName)) != 0 ||
		len(validation.IsDNS1123Label(policy.ControllerContainer)) != 0 || !resourcePolicyUID(policy.ControllerUID) ||
		!ValidDigest(policy.ControllerImageDigest) || !ValidDigest(policy.TemplateSHA256) {
		return ErrResourcePolicyInvalid
	}
	switch policy.Consumer {
	case ControllerConsumerDataOnlyV1:
		if policy.Extractor != "NONE" {
			return ErrResourcePolicyInvalid
		}
	case ControllerConsumerPodTemplateV1:
		if policy.Extractor != "SPEC_TEMPLATE" && policy.Extractor != "SPEC_JOB_TEMPLATE" && policy.Extractor != "SPEC_POD_TEMPLATE" {
			return ErrResourcePolicyInvalid
		}
	default:
		return ErrResourcePolicyInvalid
	}
	return nil
}

// resourcePolicyUID requires canonical nonzero UUID witnesses without assigning ownership.
// resourcePolicyUID требует canonical nonzero UUID witnesses без назначения ownership.
func resourcePolicyUID(raw string) bool {
	id, err := uuid.Parse(raw)
	return err == nil && id != uuid.Nil && id.String() == raw
}

// DecodeResourcePolicy accepts only bounded exact compact canonical operator metadata.
// Duplicate/case-alias/unknown/invalid UTF-8 fields fail before policy classification. The digest
// is a checksum, never an authorization token; the owner independently supplies target pins.
// DecodeResourcePolicy принимает только bounded exact compact canonical operator metadata.
// Duplicate/case-alias/unknown/invalid UTF-8 fields отклоняются до classification. Digest —
// checksum, не authorization token; target pins независимо предоставляет owner.
func DecodeResourcePolicy(raw []byte) (ResourcePolicy, string, error) {
	var result ResourcePolicy
	if len(raw) == 0 || len(raw) > MaximumResourcePolicyBytes || internalrpcauth.DecodeExactJSON(raw, &result) != nil || result.Validate() != nil {
		return ResourcePolicy{}, "", ErrResourcePolicyInvalid
	}
	canonical, err := json.Marshal(result)
	if err != nil || !bytes.Equal(raw, canonical) {
		return ResourcePolicy{}, "", ErrResourcePolicyInvalid
	}
	digest := sha256.Sum256(canonical)
	return result, hex.EncodeToString(digest[:]), nil
}
