package kubernetestarget

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"

	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/util/validation"
)

// prepareResourceCommandPatch derives one deterministic native patch from the exact observed object.
// The existing patch builder supplies UID/RV conditions; full schema and current/proposed policy checks still run.
// prepareResourceCommandPatch выводит один deterministic native patch из exact observed object.
// Existing patch builder добавляет UID/RV conditions; полные schema и current/proposed policy checks сохраняются.
func (prepared *PreparedResourceOperation) prepareResourceCommandPatch(options map[string]any) ([]byte, string, map[string]any, error) {
	object, err := cloneResourceObject(prepared.current)
	if err != nil || prepared.address.Subresource != "" || prepared.address.Name == "" {
		return nil, "", nil, errors.New(resourceOperationInvalid)
	}
	operation := prepared.operation.Key
	kind := prepared.schema.kind.Kind
	patchObject := map[string]any{}
	switch operation {
	case "kubernetes.set.image", "kubernetes.set.env", "kubernetes.set.resources", "kubernetes.set.serviceaccount":
		spec, workload, failure := resourcePodSpec(prepared.address, kind, object)
		if failure != nil || !workload {
			err = errors.New(resourceOperationInvalid)
			break
		}
		if operation == "kubernetes.set.serviceaccount" {
			name, ok := options["serviceAccount"].(string)
			if !ok || len(validation.IsDNS1123Subdomain(name)) != 0 {
				err = errors.New(resourceOperationInvalid)
				break
			}
			spec["serviceAccountName"] = name
			delete(spec, "serviceAccount")
		} else {
			err = mutateResourceContainers(spec, operation, options)
		}
		fields := map[string]any{}
		if operation == "kubernetes.set.serviceaccount" {
			fields["serviceAccountName"] = spec["serviceAccountName"]
			fields["serviceAccount"] = nil
		} else {
			for _, name := range []string{"containers", "initContainers"} {
				if value, exists := spec[name]; exists {
					fields[name] = value
				}
			}
		}
		// Keep exact integers in the selected arrays; generic diff decoders can round them through float64.
		// Сохраняем точные целые числа в выбранных массивах: универсальный diff может округлить их через float64.
		path := resourcePodSpecPath(prepared.address, kind)
		for i := len(path) - 1; i >= 0; i-- {
			fields = map[string]any{path[i]: fields}
		}
		patchObject = fields
	case "kubernetes.set.selector":
		if prepared.address.Group != "" || prepared.address.Resource != "services" || kind != "Service" {
			err = errors.New(resourceOperationInvalid)
			break
		}
		selector, ok := options["selectorAssignments"].(map[string]any)
		if !ok || len(selector) == 0 {
			err = errors.New(resourceOperationInvalid)
			break
		}
		for key, raw := range selector {
			value, ok := raw.(string)
			if !ok || len(validation.IsQualifiedName(key)) != 0 || len(validation.IsValidLabelValue(value)) != 0 {
				err = errors.New(resourceOperationInvalid)
				break
			}
		}
		spec, valid := resourceCommandObjectField(object, "spec")
		if !valid {
			err = errors.New(resourceOperationInvalid)
			break
		}
		// Merge-patch maps require explicit deletions to replace the selector rather than merge old keys.
		// Для замены selector merge patch явно удаляет прежние ключи, отсутствующие в новом наборе.
		selection := map[string]any{}
		if old, ok := spec["selector"].(map[string]any); ok {
			for key := range old {
				selection[key] = nil
			}
		}
		for key, value := range selector {
			selection[key] = value
		}
		patchObject["spec"] = map[string]any{"selector": selection}
	case "kubernetes.set.subject":
		if prepared.address.Group != "rbac.authorization.k8s.io" || !((prepared.address.Resource == "rolebindings" && kind == "RoleBinding") || (prepared.address.Resource == "clusterrolebindings" && kind == "ClusterRoleBinding")) {
			err = errors.New(resourceOperationInvalid)
			break
		}
		err = addResourceSubjects(object, options)
		patchObject["subjects"] = object["subjects"]
	case "kubernetes.rollout.pause", "kubernetes.rollout.resume":
		if prepared.address.Group != "apps" || prepared.address.Resource != "deployments" || kind != "Deployment" {
			err = errors.New(resourceOperationInvalid)
			break
		}
		spec, valid := resourceCommandObjectField(object, "spec")
		if !valid {
			err = errors.New(resourceOperationInvalid)
			break
		}
		spec["paused"] = operation == "kubernetes.rollout.pause"
		patchObject["spec"] = map[string]any{"paused": spec["paused"]}
	case "kubernetes.cordon", "kubernetes.uncordon", "kubernetes.taint":
		if prepared.address.Group != "" || prepared.address.Resource != "nodes" || kind != "Node" || prepared.address.Namespaced {
			err = errors.New(resourceOperationInvalid)
			break
		}
		spec, valid := resourceCommandObjectField(object, "spec")
		if !valid {
			err = errors.New(resourceOperationInvalid)
			break
		}
		if operation == "kubernetes.taint" {
			err = mutateResourceTaints(spec, options)
			patchObject["spec"] = map[string]any{"taints": spec["taints"]}
		} else {
			spec["unschedulable"] = operation == "kubernetes.cordon"
			patchObject["spec"] = map[string]any{"unschedulable": spec["unschedulable"]}
		}
	default:
		err = errors.New(resourceOperationInvalid)
	}
	if err != nil {
		return nil, "", nil, err
	}
	patch, err := json.Marshal(patchObject)
	if err != nil || len(patch) > resourceResponseBytes {
		return nil, "", nil, errors.New(resourceOperationInvalid)
	}
	defer clear(patch)
	return prepared.prepareResourcePatch(patch, map[string]any{"patchType": "MERGE"})
}

// resourceCommandObjectField creates only a missing known field and refuses a malformed existing value.
// resourceCommandObjectField создаёт лишь missing known field и отклоняет malformed existing value.
func resourceCommandObjectField(object map[string]any, key string) (map[string]any, bool) {
	if raw, exists := object[key]; exists {
		value, ok := raw.(map[string]any)
		return value, ok
	}
	value := map[string]any{}
	object[key] = value
	return value, true
}

// mutateResourceContainers selects existing regular/init containers without changing their order or creating new ones.
// Every explicitly named container must match; wildcard and explicit selection cannot overlap ambiguously.
// mutateResourceContainers выбирает existing regular/init containers без перестановки либо создания новых.
// Каждый explicit name обязан совпасть; wildcard не смешивается с explicit selection.
func mutateResourceContainers(spec map[string]any, operation string, options map[string]any) error {
	containers := []map[string]any{}
	seen := map[string]bool{}
	for _, field := range []string{"initContainers", "containers"} {
		raw, exists := spec[field]
		if !exists {
			continue
		}
		values, ok := raw.([]any)
		if !ok {
			return errors.New(resourceOperationInvalid)
		}
		for _, raw := range values {
			container, ok := raw.(map[string]any)
			name, valid := container["name"].(string)
			if !ok || !valid || name == "" || seen[name] {
				return errors.New(resourceOperationInvalid)
			}
			seen[name] = true
			containers = append(containers, container)
		}
	}
	if len(containers) == 0 {
		return errors.New(resourceOperationInvalid)
	}
	if operation == "kubernetes.set.image" {
		assignments, ok := options["assignments"].(map[string]any)
		if !ok || len(assignments) == 0 {
			return errors.New(resourceOperationInvalid)
		}
		for name, raw := range assignments {
			image, ok := raw.(string)
			if !ok || strings.TrimSpace(image) != image || image == "" || strings.ContainsAny(image, "\r\n\x00") || name == "*" && len(assignments) != 1 || name != "*" && !seen[name] {
				return errors.New(resourceOperationInvalid)
			}
			for _, container := range containers {
				if name == "*" || name == container["name"] {
					container["image"] = image
				}
			}
		}
		return nil
	}
	names := []string{}
	if raw, exists := options["containerNames"]; exists {
		values, ok := raw.([]any)
		if !ok || len(values) == 0 {
			return errors.New(resourceOperationInvalid)
		}
		for _, raw := range values {
			name, ok := raw.(string)
			if !ok || slices.Contains(names, name) || name != "*" && !seen[name] {
				return errors.New(resourceOperationInvalid)
			}
			names = append(names, name)
		}
		if slices.Contains(names, "*") && len(names) != 1 {
			return errors.New(resourceOperationInvalid)
		}
	}
	for _, container := range containers {
		name := container["name"].(string)
		if len(names) > 0 && !slices.Contains(names, "*") && !slices.Contains(names, name) {
			continue
		}
		var err error
		if operation == "kubernetes.set.env" {
			err = mutateResourceEnvironment(container, options)
		} else {
			err = mutateResourceRequirements(container, options)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// mutateResourceEnvironment replaces named values in place, preserves unrelated entries and appends only sorted new names.
// mutateResourceEnvironment заменяет named values на месте, сохраняет unrelated entries и сортирует лишь новые names.
func mutateResourceEnvironment(container, options map[string]any) error {
	assignments, _ := options["assignments"].(map[string]any)
	removals, _ := options["removals"].([]any)
	if len(assignments)+len(removals) == 0 {
		return errors.New(resourceOperationInvalid)
	}
	removed := map[string]bool{}
	for _, raw := range removals {
		name, ok := raw.(string)
		_, conflict := assignments[name]
		if !ok || len(validation.IsRelaxedEnvVarName(name)) != 0 || removed[name] || conflict {
			return errors.New(resourceOperationInvalid)
		}
		removed[name] = true
	}
	for name, raw := range assignments {
		if _, ok := raw.(string); !ok || len(validation.IsRelaxedEnvVarName(name)) != 0 {
			return errors.New(resourceOperationInvalid)
		}
	}
	var existing []any
	if raw, ok := container["env"]; ok {
		var valid bool
		existing, valid = raw.([]any)
		if !valid {
			return errors.New(resourceOperationInvalid)
		}
	}
	result := []any{}
	seen := map[string]bool{}
	for _, raw := range existing {
		item, ok := raw.(map[string]any)
		name, valid := item["name"].(string)
		if !ok || !valid || seen[name] {
			return errors.New(resourceOperationInvalid)
		}
		seen[name] = true
		if removed[name] {
			continue
		}
		if value, exists := assignments[name]; exists {
			item = map[string]any{"name": name, "value": value}
		}
		result = append(result, item)
	}
	keys := []string{}
	for name := range assignments {
		if !seen[name] {
			keys = append(keys, name)
		}
	}
	slices.Sort(keys)
	for _, name := range keys {
		result = append(result, map[string]any{"name": name, "value": assignments[name]})
	}
	container["env"] = result
	return nil
}

// mutateResourceRequirements merges explicitly selected request/limit quantities while preserving unrelated resource keys.
// mutateResourceRequirements объединяет explicit request/limit quantities, сохраняя unrelated resource keys.
func mutateResourceRequirements(container, options map[string]any) error {
	resources, ok := resourceCommandObjectField(container, "resources")
	if !ok {
		return errors.New(resourceOperationInvalid)
	}
	changed := false
	for _, field := range []string{"requests", "limits"} {
		raw, present := options[field]
		if !present {
			continue
		}
		values, ok := raw.(map[string]any)
		if !ok || len(values) == 0 {
			return errors.New(resourceOperationInvalid)
		}
		existing, ok := resourceCommandObjectField(resources, field)
		if !ok {
			return errors.New(resourceOperationInvalid)
		}
		for key, raw := range values {
			text, ok := raw.(string)
			quantity, err := resource.ParseQuantity(text)
			if !ok || err != nil || quantity.Sign() < 0 || len(validation.IsQualifiedName(key)) != 0 {
				return errors.New(resourceOperationInvalid)
			}
			existing[key] = quantity.String()
			changed = true
		}
	}
	if !changed {
		return errors.New(resourceOperationInvalid)
	}
	return nil
}

// addResourceSubjects appends typed RBAC subjects without dropping current subjects or changing roleRef.
// addResourceSubjects добавляет typed RBAC subjects без удаления existing subjects либо изменения roleRef.
func addResourceSubjects(object, options map[string]any) error {
	values, ok := options["subjects"].([]any)
	if !ok || len(values) == 0 {
		return errors.New(resourceOperationInvalid)
	}
	var subjects []any
	if raw, present := object["subjects"]; present {
		var valid bool
		subjects, valid = raw.([]any)
		if !valid {
			return errors.New(resourceOperationInvalid)
		}
	}
	for _, raw := range values {
		subject, ok := raw.(map[string]any)
		if !ok {
			return errors.New(resourceOperationInvalid)
		}
		kind, _ := subject["kind"].(string)
		name, _ := subject["name"].(string)
		namespace, _ := subject["namespace"].(string)
		group, _ := subject["apiGroup"].(string)
		if name == "" {
			return errors.New(resourceOperationInvalid)
		}
		switch kind {
		case "ServiceAccount":
			if group != "" || len(validation.IsDNS1123Subdomain(name)) != 0 || len(validation.IsDNS1123Label(namespace)) != 0 {
				return errors.New(resourceOperationInvalid)
			}
		case "User", "Group":
			if namespace != "" || group != "" && group != "rbac.authorization.k8s.io" {
				return errors.New(resourceOperationInvalid)
			}
			group = "rbac.authorization.k8s.io"
		default:
			return errors.New(resourceOperationInvalid)
		}
		next := map[string]any{"kind": kind, "name": name, "apiGroup": group}
		if namespace != "" {
			next["namespace"] = namespace
		}
		found := false
		for _, raw := range subjects {
			item, valid := raw.(map[string]any)
			if !valid {
				return errors.New(resourceOperationInvalid)
			}
			oldKind, _ := item["kind"].(string)
			oldName, _ := item["name"].(string)
			oldGroup, _ := item["apiGroup"].(string)
			oldNamespace, _ := item["namespace"].(string)
			if oldKind == kind && oldName == name && oldGroup == group && oldNamespace == namespace {
				found = true
			}
		}
		if !found {
			subjects = append(subjects, next)
		}
	}
	object["subjects"] = subjects
	return nil
}

// mutateResourceTaints preserves list order and timeAdded; changes require exact key/effect and explicit overwrite.
// mutateResourceTaints сохраняет list order и timeAdded; изменения требуют exact key/effect и explicit overwrite.
func mutateResourceTaints(spec, options map[string]any) error {
	additions, _ := options["taints"].([]any)
	removals, _ := options["removals"].([]any)
	overwrite, _ := options["overwrite"].(bool)
	if len(additions)+len(removals) == 0 {
		return errors.New(resourceOperationInvalid)
	}
	var existing []any
	if raw, present := spec["taints"]; present {
		var ok bool
		existing, ok = raw.([]any)
		if !ok {
			return errors.New(resourceOperationInvalid)
		}
	}
	remove := map[string]bool{}
	for _, raw := range removals {
		text, ok := raw.(string)
		key, effect, found := strings.Cut(text, ":")
		if !ok || len(validation.IsQualifiedName(key)) != 0 || found && !validResourceTaintEffect(effect) || remove[text] {
			return errors.New(resourceOperationInvalid)
		}
		remove[text] = true
	}
	changes := map[string]map[string]any{}
	order := []string{}
	for _, raw := range additions {
		item, ok := raw.(map[string]any)
		key, _ := item["key"].(string)
		effect, _ := item["effect"].(string)
		value, _ := item["value"].(string)
		id := key + ":" + effect
		if !ok || len(validation.IsQualifiedName(key)) != 0 || !validResourceTaintEffect(effect) || len(validation.IsValidLabelValue(value)) != 0 || changes[id] != nil || remove[key] || remove[id] {
			return errors.New(resourceOperationInvalid)
		}
		changes[id] = item
		order = append(order, id)
	}
	result := []any{}
	seen := map[string]bool{}
	removed := map[string]bool{}
	for _, raw := range existing {
		item, ok := raw.(map[string]any)
		key, _ := item["key"].(string)
		effect, _ := item["effect"].(string)
		id := key + ":" + effect
		if !ok || seen[id] {
			return errors.New(resourceOperationInvalid)
		}
		seen[id] = true
		if remove[key] || remove[id] {
			if remove[key] {
				removed[key] = true
			}
			if remove[id] {
				removed[id] = true
			}
			continue
		}
		if next, found := changes[id]; found {
			old, _ := item["value"].(string)
			value, _ := next["value"].(string)
			if old != value && !overwrite {
				return errors.New(resourceOperationInvalid)
			}
			item["value"] = value
		}
		result = append(result, item)
	}
	for key := range remove {
		if !removed[key] {
			return errors.New(resourceOperationInvalid)
		}
	}
	for _, id := range order {
		if !seen[id] {
			result = append(result, changes[id])
		}
	}
	spec["taints"] = result
	return nil
}

// validResourceTaintEffect closes the scheduler effect vocabulary before patch construction.
// validResourceTaintEffect закрывает scheduler effect vocabulary до создания patch.
func validResourceTaintEffect(effect string) bool {
	return effect == "NoSchedule" || effect == "PreferNoSchedule" || effect == "NoExecute"
}
