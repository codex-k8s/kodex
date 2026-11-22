package kubernetestarget

import (
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/url"
	"reflect"
	"strconv"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"k8s.io/apimachinery/pkg/util/validation"
)

// buildResourceRequest creates a private single-attempt request proposal without performing I/O.
// Body reconstruction uses the exact observed object/schema and never changes target authority.
// Protected Secret bytes are supplied separately; no public input parser accepts their values.
// buildResourceRequest создаёт private single-attempt request proposal без I/O.
// Body reconstruction использует exact observed object/schema и не меняет target authority.
// Protected Secret bytes приходят отдельно; public input parser их values не принимает.
func (prepared *PreparedResourceOperation) buildResourceRequest(material []byte) error {
	options, err := resourceOperationOptions(prepared.input)
	if err != nil {
		return err
	}
	prepared.query = make(url.Values)
	prepared.contentType = "application/json"
	if manager, ok := options["fieldManager"].(string); ok {
		if manager == "" || len(manager) > 128 {
			return errors.New(resourceOperationInvalid)
		}
		prepared.query.Set("fieldManager", manager)
	}
	if prepared.operation.Risk != "READ" && prepared.operation.Key != "kubernetes.delete" {
		prepared.query.Set("fieldValidation", "Strict")
	}
	if prepared.current != nil && prepared.operation.Key != "kubernetes.scale" &&
		prepared.validateResourcePolicy(prepared.address, prepared.schema.kind.Kind, prepared.current) != nil {
		return errors.New(resourcePolicyForbidden)
	}
	var proposed map[string]any
	switch prepared.operation.Key {
	case "kubernetes.get", "kubernetes.status.get":
		prepared.method = http.MethodGet
		return nil
	case "kubernetes.list", "kubernetes.list.all-namespaces", "kubernetes.events":
		prepared.method = http.MethodGet
		prepared.query.Set("limit", "100")
		for _, key := range []string{"continue", "fieldSelector", "resourceVersion", "resourceVersionMatch"} {
			if value, ok := options[key].(string); ok {
				prepared.query.Set(key, value)
			}
		}
		if limit, present := options["limit"]; present {
			value, valid := resourceInteger(limit)
			if !valid || value < 1 || value > 500 {
				return errors.New(resourceOperationInvalid)
			}
			prepared.query.Set("limit", strconv.FormatInt(value, 10))
		}
		if prepared.input.Selector != "" {
			prepared.query.Set("labelSelector", prepared.input.Selector)
		}
		return nil
	case "kubernetes.create", "kubernetes.replace", "kubernetes.edit", "kubernetes.status.update", "kubernetes.apply":
		proposed, err = decodeResourceJSON(prepared.input.Body, managedkubernetes.MaximumOperationInputBytes, 64)
		if err != nil {
			return err
		}
		if prepared.materialRef != "" {
			proposed, err = injectResourceSecretContent(proposed, material)
			if err != nil {
				return err
			}
		}
		prepared.method = http.MethodPut
		if prepared.operation.Key == "kubernetes.create" || prepared.observation.State == managedkubernetes.ObservationAbsent {
			prepared.method = http.MethodPost
		} else if prepared.operation.Key == "kubernetes.apply" {
			prepared.method, prepared.contentType = http.MethodPatch, "application/apply-patch+yaml"
			if force, _ := options["forceConflicts"].(bool); force {
				prepared.query.Set("force", "true")
			}
		}
		if prepared.operation.Key == "kubernetes.status.update" && !resourceStatusUpdateMatchesCurrent(proposed, prepared.current) {
			return errors.New(resourceOperationInvalid)
		}
		if prepared.method != http.MethodPost {
			if err = pinResourceBodyIdentity(proposed, prepared.address); err != nil {
				return err
			}
		}
		wire := proposed
		if prepared.operation.Key == "kubernetes.status.update" {
			proposed, err = cloneResourceObject(prepared.current)
			if err != nil {
				return err
			}
			proposed["status"] = wire["status"]
		}
		if prepared.operation.Key == "kubernetes.apply" && prepared.current != nil {
			proposed, err = prepared.reconstructResourceApply(proposed, options)
			if err != nil {
				return err
			}
		}
		prepared.body, err = json.Marshal(wire)
	case "kubernetes.run":
		prepared.method = http.MethodPost
		proposed, err = prepared.prepareResourceRun(options)
		if err == nil {
			prepared.body, err = json.Marshal(proposed)
		}
	case "kubernetes.patch", "kubernetes.status.patch":
		prepared.method = http.MethodPatch
		patch := prepared.input.Body
		if prepared.materialRef != "" {
			if len(material) == 0 {
				return errors.New(resourceMaterialRequired)
			}
			patch = material
		}
		prepared.body, prepared.contentType, proposed, err = prepared.prepareResourcePatch(patch, options)
	case "kubernetes.label", "kubernetes.annotate":
		prepared.method, prepared.contentType = http.MethodPatch, "application/merge-patch+json"
		prepared.body, proposed, err = prepared.prepareResourceMetadataPatch(options)
	case "kubernetes.set.image", "kubernetes.set.env", "kubernetes.set.resources", "kubernetes.set.selector", "kubernetes.set.serviceaccount", "kubernetes.set.subject", "kubernetes.rollout.pause", "kubernetes.rollout.resume", "kubernetes.cordon", "kubernetes.uncordon", "kubernetes.taint":
		prepared.method = http.MethodPatch
		prepared.body, prepared.contentType, proposed, err = prepared.prepareResourceCommandPatch(options)
	case "kubernetes.scale":
		prepared.method = http.MethodPut
		proposed, err = prepared.prepareResourceScale(options)
		if err == nil {
			prepared.body, err = json.Marshal(proposed)
		}
	case "kubernetes.delete":
		prepared.method = http.MethodDelete
		prepared.body, err = prepared.prepareResourceDelete()
		return err
	default:
		return errors.New(resourceOperationInvalid)
	}
	if err != nil || proposed == nil || len(prepared.body) == 0 || len(prepared.body) > resourceResponseBytes ||
		prepared.schema.compiled.Validate(proposed) != nil {
		return errors.New(resourceOperationInvalid)
	}
	if err = validateProposedResourceIdentity(proposed, prepared.address, prepared.schema, prepared.method == http.MethodPost); err != nil {
		return err
	}
	if proposed["apiVersion"] == "v1" && proposed["kind"] == "Secret" && validateResourceSecretContent(proposed) != nil {
		return errors.New(resourceOperationInvalid)
	}
	if prepared.operation.Key != "kubernetes.scale" {
		if err = prepared.validateResourcePolicy(prepared.address, prepared.schema.kind.Kind, proposed); err != nil {
			return err
		}
		prepared.proposed = proposed
	}
	return nil
}

// pinResourceBodyIdentity refuses a caller's conflicting immutable field before injecting the
// observed UID/RV. It never overwrites a hidden name/namespace change into an accepted proposal.
// pinResourceBodyIdentity отклоняет conflicting immutable field caller до инъекции observed
// UID/RV. Hidden name/namespace change не перезаписывается в допустимый proposal.
func pinResourceBodyIdentity(object map[string]any, expected ResourceAddress) error {
	metadata, present := object["metadata"]
	if !present {
		metadata = map[string]any{}
		object["metadata"] = metadata
	}
	fields, ok := metadata.(map[string]any)
	if !ok {
		return errors.New(resourceOperationInvalid)
	}
	for key, expectedValue := range map[string]string{"name": expected.Name, "namespace": expected.Namespace, "uid": expected.UID, "resourceVersion": expected.ResourceVersion} {
		if value, exists := fields[key]; exists && value != expectedValue {
			return errors.New(resourceOperationInvalid)
		}
	}
	if expected.UID == "" || expected.ResourceVersion == "" {
		return errors.New(resourceOperationInvalid)
	}
	fields["uid"], fields["resourceVersion"] = expected.UID, expected.ResourceVersion
	return nil
}

// validateProposedResourceIdentity keeps create intent unassigned while enforcing exact identity
// on every existing-object effect, including resources whose body GVK differs for a subresource.
// validateProposedResourceIdentity сохраняет create intent без assigned identity и проверяет
// exact identity каждого existing-object effect, включая иной body GVK subresource.
func validateProposedResourceIdentity(object map[string]any, address ResourceAddress, registered *registeredResourceSchema, create bool) error {
	metadata, ok := object["metadata"].(map[string]any)
	if !ok || object["apiVersion"] != registered.kind.GroupVersion().String() || object["kind"] != registered.kind.Kind ||
		(address.Namespaced && metadata["namespace"] != address.Namespace) ||
		(!address.Namespaced && metadata["namespace"] != nil && metadata["namespace"] != "") {
		return errors.New(resourceOperationInvalid)
	}
	if create {
		name, _ := metadata["name"].(string)
		generated, _ := metadata["generateName"].(string)
		if metadata["uid"] != nil || metadata["resourceVersion"] != nil || name == "" && generated == "" || address.Name != "" && name != address.Name {
			return errors.New(resourceOperationInvalid)
		}
		return nil
	}
	if metadata["name"] != address.Name || metadata["uid"] != address.UID || metadata["resourceVersion"] != address.ResourceVersion {
		return errors.New(resourceOperationInvalid)
	}
	return nil
}

// resourceStatusBodyOnly forbids a status-only capability from proposing specification changes.
// resourceStatusBodyOnly не позволяет status-only capability предлагать specification changes.
func resourceStatusBodyOnly(object map[string]any) bool {
	for key := range object {
		if key != "apiVersion" && key != "kind" && key != "metadata" && key != "status" {
			return false
		}
	}
	if metadata, present := object["metadata"]; present {
		fields, ok := metadata.(map[string]any)
		if !ok {
			return false
		}
		for key := range fields {
			if key != "name" && key != "namespace" && key != "uid" && key != "resourceVersion" {
				return false
			}
		}
	}
	return true
}

// resourceStatusUpdateMatchesCurrent permits a normal full-object status update only when all
// supplied nonstatus fields are unchanged. A narrow status object is reconstructed against the
// trusted current object for complete schema and escalation checks without editing its spec.
// resourceStatusUpdateMatchesCurrent допускает normal full-object status update лишь при
// неизменных supplied nonstatus fields. Narrow status object реконструируется по trusted
// current object для complete schema/escalation checks без изменения spec.
func resourceStatusUpdateMatchesCurrent(proposed, current map[string]any) bool {
	if current == nil {
		return false
	}
	for key, value := range proposed {
		if key == "status" {
			continue
		}
		if key == "metadata" {
			fields, ok := value.(map[string]any)
			existing, valid := current[key].(map[string]any)
			if !ok || !valid {
				return false
			}
			for name, item := range fields {
				if !reflect.DeepEqual(existing[name], item) {
					return false
				}
			}
		} else if !reflect.DeepEqual(current[key], value) {
			return false
		}
	}
	_, present := proposed["status"]
	return present
}

// prepareResourceMetadataPatch preserves normal label/annotation overwrite semantics and uses
// a conditional native merge patch. Removed keys become JSON null only in the private patch.
// prepareResourceMetadataPatch сохраняет normal label/annotation overwrite semantics через
// conditional native merge patch. Удалённые keys становятся JSON null только в private patch.
func (prepared *PreparedResourceOperation) prepareResourceMetadataPatch(options map[string]any) ([]byte, map[string]any, error) {
	key := "labels"
	if prepared.operation.Key == "kubernetes.annotate" {
		key = "annotations"
	}
	object, err := cloneResourceObject(prepared.current)
	if err != nil {
		return nil, nil, err
	}
	metadata := object["metadata"].(map[string]any)
	existing, _ := metadata[key].(map[string]any)
	if existing == nil {
		existing = map[string]any{}
		metadata[key] = existing
	}
	changes := map[string]any{}
	overwrite, _ := options["overwrite"].(bool)
	assignments, _ := options["assignments"].(map[string]any)
	for name, raw := range assignments {
		value, ok := raw.(string)
		if !ok || len(validation.IsQualifiedName(name)) != 0 || key == "labels" && len(validation.IsValidLabelValue(value)) != 0 {
			return nil, nil, errors.New(resourceOperationInvalid)
		}
		if previous, exists := existing[name]; exists && previous != value && !overwrite {
			return nil, nil, errors.New(resourceOperationInvalid)
		}
		changes[name], existing[name] = value, value
	}
	removals, _ := options["removals"].([]any)
	for _, raw := range removals {
		name, ok := raw.(string)
		if !ok || len(validation.IsQualifiedName(name)) != 0 {
			return nil, nil, errors.New(resourceOperationInvalid)
		}
		if _, conflict := changes[name]; conflict {
			return nil, nil, errors.New(resourceOperationInvalid)
		}
		changes[name] = nil
		delete(existing, name)
	}
	if len(changes) == 0 {
		return nil, nil, errors.New(resourceOperationInvalid)
	}
	patch := map[string]any{"metadata": map[string]any{"uid": prepared.address.UID, "resourceVersion": prepared.address.ResourceVersion, key: changes}}
	body, err := json.Marshal(patch)
	return body, object, err
}

// prepareResourceScale binds replicas and optional currentReplicas to the already observed Scale.
// The full parent policy was separately checked at the same immutable UID/RV during preparation.
// prepareResourceScale связывает replicas/optional currentReplicas с наблюдённым Scale.
// Full parent policy отдельно проверена с тем же immutable UID/RV при preparation.
func (prepared *PreparedResourceOperation) prepareResourceScale(options map[string]any) (map[string]any, error) {
	replicas, valid := resourceInteger(options["replicas"])
	if !valid || replicas < 0 || replicas > 100000 || prepared.parent == nil {
		return nil, errors.New(resourceOperationInvalid)
	}
	if expected, present := options["currentReplicas"]; present {
		current, ok := prepared.current["spec"].(map[string]any)
		wanted, valid := resourceInteger(expected)
		actual, observed := resourceInteger(current["replicas"])
		if !ok || !valid || !observed || wanted != actual {
			return nil, errors.New(resourceOperationInvalid)
		}
	}
	metadata := map[string]any{"name": prepared.address.Name, "uid": prepared.address.UID, "resourceVersion": prepared.address.ResourceVersion}
	if prepared.address.Namespaced {
		metadata["namespace"] = prepared.address.Namespace
	}
	return map[string]any{"apiVersion": prepared.schema.kind.GroupVersion().String(), "kind": prepared.schema.kind.Kind,
		"metadata": metadata, "spec": map[string]any{"replicas": replicas}}, nil
}

// prepareResourceDelete emits only registered deletion options with exact UID/RV preconditions.
// No dry-run or unsafe store-read override enters this single admitted destructive request.
// prepareResourceDelete выдаёт только registered deletion options с exact UID/RV preconditions.
// Dry-run/unsafe store-read override не попадают в single admitted destructive request.
func (prepared *PreparedResourceOperation) prepareResourceDelete() ([]byte, error) {
	options := map[string]any{}
	if len(prepared.input.Body) != 0 {
		var err error
		options, err = decodeResourceJSON(prepared.input.Body, managedkubernetes.MaximumOperationInputBytes, 16)
		if err != nil {
			return nil, err
		}
	}
	preconditions := map[string]any{"uid": prepared.address.UID, "resourceVersion": prepared.address.ResourceVersion}
	if declared, exists := options["preconditions"].(map[string]any); exists {
		for key, value := range declared {
			if preconditions[key] != value {
				return nil, errors.New(resourceOperationInvalid)
			}
		}
	}
	options["apiVersion"], options["kind"], options["preconditions"] = "v1", "DeleteOptions", preconditions
	return json.Marshal(options)
}

// resourceInteger preserves exact integral option intent after the shared schema check.
// resourceInteger сохраняет exact integral option intent после shared schema check.
func resourceInteger(value any) (int64, bool) {
	switch number := value.(type) {
	case int64:
		return number, true
	case float64:
		if math.IsNaN(number) || math.IsInf(number, 0) || math.Trunc(number) != number || number < math.MinInt64 || number >= math.MaxInt64 {
			return 0, false
		}
		return int64(number), true
	case json.Number:
		value, err := number.Int64()
		return value, err == nil
	}
	return 0, false
}
