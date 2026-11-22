package kubernetestarget

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	strictjson "sigs.k8s.io/json"
)

// prepareObservation keeps collection proposals separate from authenticated object observations.
// CREATE performs no fabricated NotFound probe; APPLY may become ABSENT only after an exact
// authenticated Kubernetes NotFound, and never when the caller pinned a prior object's UID/RV.
// prepareObservation отделяет collection proposal от authenticated object observation.
// CREATE не выдумывает NotFound probe; APPLY становится ABSENT только после exact authenticated
// Kubernetes NotFound и никогда при caller pin прежнего object's UID/RV.
func (prepared *PreparedResourceOperation) prepareObservation(ctx context.Context) error {
	prepared.observation = managedkubernetes.ResourceObservation{Resource: prepared.address, RegisteredSchemaDigest: prepared.schema.digest}
	if prepared.operation.Verb == "list" || prepared.operation.Verb == "create" {
		prepared.observation.State = managedkubernetes.ObservationCollection
		fact, err := json.Marshal(prepared.observation)
		if err != nil {
			return errors.New(resourceOperationInvalid)
		}
		digest := sha256.Sum256(fact)
		prepared.observation.ObservedResourceDigest = hex.EncodeToString(digest[:])
		if prepared.observation.Validate() != nil {
			return errors.New(resourceOperationInvalid)
		}
		return prepared.authorizeResource(ctx)
	}
	read := AccessRequirement{Group: prepared.address.Group, Version: prepared.address.Version, Resource: prepared.address.Resource,
		Namespace: prepared.address.Namespace, Name: prepared.address.Name, Subresource: prepared.address.Subresource, Verb: "get", Namespaced: prepared.address.Namespaced}
	if prepared.credential.Authorize(ctx, prepared.subjectDigest, []AccessRequirement{read}) != nil {
		return errors.New(resourceOperationInvalid)
	}
	client, closeIdle, err := prepared.credential.NewClient(30 * time.Second)
	if err != nil {
		return errors.New(resourceOperationInvalid)
	}
	defer closeIdle()
	request, err := prepared.credential.ReadRequest(ctx, prepared.address)
	if err != nil {
		return errors.New(resourceOperationInvalid)
	}
	raw, status, err := readResourceHTTPDocument(client, request, resourceResponseBytes)
	if err != nil {
		return err
	}
	defer clear(raw)
	digest := sha256.Sum256(raw)
	prepared.observation.ObservedResourceDigest = hex.EncodeToString(digest[:])
	if status == http.StatusNotFound {
		if prepared.operation.Key != "kubernetes.apply" || prepared.address.UID != "" || prepared.address.ResourceVersion != "" || !resourceNotFoundMatches(raw, prepared.address) {
			return errors.New(resourceOperationInvalid)
		}
		prepared.observation.State = managedkubernetes.ObservationAbsent
	} else {
		object, decodeErr := decodeResourceJSON(raw, resourceResponseBytes, 64)
		if decodeErr != nil || prepared.schema.compiled.Validate(object) != nil {
			return errors.New(resourceOperationInvalid)
		}
		observed, identityErr := resourceObjectIdentity(object, prepared.address, prepared.schema)
		if identityErr != nil && prepared.operation.Risk == integrationpackage.RiskRead && prepared.schema.unversionedRead &&
			prepared.address.UID == "" && prepared.address.ResourceVersion == "" && resourceUnversionedIdentityMatches(object, prepared.address, prepared.schema) {
			prepared.current = object
			prepared.observation.State = managedkubernetes.ObservationUnversionedRead
			if prepared.observation.Validate() != nil {
				return errors.New(resourceOperationInvalid)
			}
			return prepared.authorizeResource(ctx)
		}
		if identityErr != nil || prepared.address.UID != "" && prepared.address.UID != observed.UID ||
			prepared.address.ResourceVersion != "" && prepared.address.ResourceVersion != observed.ResourceVersion {
			return errors.New(resourceOperationInvalid)
		}
		prepared.address, prepared.current = observed, object
		prepared.observation.State, prepared.observation.Resource = managedkubernetes.ObservationPresent, observed
	}
	if prepared.observation.Validate() != nil {
		return errors.New(resourceOperationInvalid)
	}
	if prepared.operation.Key == "kubernetes.scale" {
		if err := prepared.prepareScaleParent(ctx, client); err != nil {
			return err
		}
	}
	return prepared.authorizeResource(ctx)
}

// resourceUnversionedIdentityMatches checks named scope/GVK without manufacturing storage facts.
// It is used only after read-only discovery classification and READ/NONE owner admission.
// resourceUnversionedIdentityMatches проверяет named scope/GVK без выдуманных storage facts.
// Используется только после read-only discovery classification и READ/NONE owner admission.
func resourceUnversionedIdentityMatches(object map[string]any, expected ResourceAddress, registered *registeredResourceSchema) bool {
	metadata, ok := object["metadata"].(map[string]any)
	for _, key := range []string{"uid", "resourceVersion"} {
		if value, present := metadata[key]; present && value != "" {
			return false
		}
	}
	return ok && object["apiVersion"] == registered.kind.GroupVersion().String() && object["kind"] == registered.kind.Kind &&
		metadata["name"] == expected.Name && (expected.Namespaced && metadata["namespace"] == expected.Namespace ||
		!expected.Namespaced && (metadata["namespace"] == nil || metadata["namespace"] == ""))
}

// authorizeResource performs fresh selected-identity RBAC for the exact wire operation and schema
// freshness read. An absent APPLY requires both named patch and collection create rights; it
// never uses its input UID as authorization or silently turns a failed GET into absence.
// authorizeResource выполняет fresh selected-identity RBAC для exact wire operation и schema
// freshness read. ABSENT APPLY требует named patch и collection create rights; input UID не
// становится authorization, а failed GET не превращается в absence молча.
func (prepared *PreparedResourceOperation) authorizeResource(ctx context.Context) error {
	var requirements []AccessRequirement
	if prepared.operation.Key == "kubernetes.apply" && prepared.observation.State == managedkubernetes.ObservationAbsent {
		collection := prepared.address
		collection.Name, collection.UID, collection.ResourceVersion = "", "", ""
		var err error
		requirements, err = OperationAccessRequirements("kubernetes.create", collection)
		if err != nil {
			return err
		}
		requirements = append(requirements, AccessRequirement{Group: prepared.address.Group, Version: prepared.address.Version, Resource: prepared.address.Resource,
			Namespace: prepared.address.Namespace, Name: prepared.address.Name, Verb: "patch", Namespaced: prepared.address.Namespaced})
	} else {
		var err error
		requirements, err = OperationAccessRequirements(prepared.operation.Key, prepared.address)
		if err != nil {
			return err
		}
	}
	requirements = append(requirements, AccessRequirement{Discovery: managedkubernetes.DiscoveryAddress{Kind: managedkubernetes.DiscoveryOpenAPIIndex}, Verb: "get"})
	if prepared.credential.Authorize(ctx, prepared.subjectDigest, requirements) != nil {
		return errors.New(resourceOperationInvalid)
	}
	return nil
}

// resourceObjectIdentity accepts only exact selected GVK/name/namespace with a genuine API UID/RV.
// The returned metadata is still an observation; it does not authorize the next effect.
// resourceObjectIdentity принимает только exact selected GVK/name/namespace с настоящим API UID/RV.
// Возвращённые metadata остаются observation и не разрешают следующий effect.
func resourceObjectIdentity(object map[string]any, expected ResourceAddress, registered *registeredResourceSchema) (ResourceAddress, error) {
	metadata, ok := object["metadata"].(map[string]any)
	if !ok || object["apiVersion"] != registered.kind.GroupVersion().String() || object["kind"] != registered.kind.Kind ||
		metadata["name"] != expected.Name ||
		(expected.Namespaced && metadata["namespace"] != expected.Namespace) ||
		(!expected.Namespaced && metadata["namespace"] != nil && metadata["namespace"] != "") {
		return ResourceAddress{}, errors.New(resourceOperationInvalid)
	}
	actual := expected
	actual.UID, _ = metadata["uid"].(string)
	actual.ResourceVersion, _ = metadata["resourceVersion"].(string)
	if actual.UID == "" || actual.ResourceVersion == "" || actual.Validate() != nil {
		return ResourceAddress{}, errors.New(resourceOperationInvalid)
	}
	return actual, nil
}

// resourceNotFoundMatches refuses proxy errors and denied/unknown responses as absence evidence.
// resourceNotFoundMatches не допускает proxy errors и denied/unknown responses как absence evidence.
func resourceNotFoundMatches(raw []byte, expected ResourceAddress) bool {
	if _, err := decodeResourceJSON(raw, resourceResponseBytes, 64); err != nil {
		return false
	}
	var status metav1.Status
	strictErrors, err := strictjson.UnmarshalStrict(raw, &status)
	return err == nil && len(strictErrors) == 0 && status.Kind == "Status" && status.APIVersion == "v1" &&
		status.Status == metav1.StatusFailure && status.Reason == metav1.StatusReasonNotFound && status.Code == http.StatusNotFound &&
		status.Details != nil && status.Details.Name == expected.Name && status.Details.Group == expected.Group && status.Details.Kind == expected.Resource
}

// prepareScaleParent validates the current full parent at the same UID/RV as Scale before
// workload policy classification. Scale alone cannot prove that its Pod template is safe.
// prepareScaleParent проверяет current full parent с тем же UID/RV, что Scale, до workload
// policy classification. Сам Scale не доказывает безопасность его Pod template.
func (prepared *PreparedResourceOperation) prepareScaleParent(ctx context.Context, client *http.Client) error {
	parent := prepared.address
	parent.Subresource = ""
	operation, _ := integrationpackage.KubernetesOperationByKey("kubernetes.get")
	registered, err := prepared.credential.discoverResource(ctx, prepared.subjectDigest, parent, operation)
	if err != nil || registered.digest != prepared.schema.digest {
		return errors.New(resourceOperationInvalid)
	}
	requirements, err := OperationAccessRequirements("kubernetes.get", parent)
	if err != nil || prepared.credential.Authorize(ctx, prepared.subjectDigest, requirements) != nil {
		return errors.New(resourceOperationInvalid)
	}
	request, err := prepared.credential.ReadRequest(ctx, parent)
	if err != nil {
		return errors.New(resourceOperationInvalid)
	}
	raw, status, err := readResourceHTTPDocument(client, request, resourceResponseBytes)
	if err != nil || status != http.StatusOK {
		return errors.New(resourceOperationInvalid)
	}
	defer clear(raw)
	object, err := decodeResourceJSON(raw, resourceResponseBytes, 64)
	if err != nil || registered.compiled.Validate(object) != nil {
		return errors.New(resourceOperationInvalid)
	}
	observed, err := resourceObjectIdentity(object, parent, registered)
	if err != nil || observed.UID != prepared.address.UID || observed.ResourceVersion != prepared.address.ResourceVersion ||
		prepared.validateResourcePolicy(parent, registered.kind.Kind, object) != nil {
		return errors.New(resourceOperationInvalid)
	}
	prepared.parent = object
	return nil
}
