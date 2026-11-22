package managedkubernetes

import (
	"slices"
	"strings"
)

// operationInputRegistry is the finite input language shared by owner, package, gateway and broker.
// Each entry registers its own options; commands cannot inherit arbitrary kubectl flags.
// operationInputRegistry — конечный input language для owner, package, gateway и broker.
// Каждая запись задаёт собственные options; commands не наследуют произвольные kubectl flags.
var operationInputRegistry = makeOperationInputRegistry()

// makeOperationInputRegistry assigns explicit resource/body/option variants to every known command.
// This is a data contract, not a declaration that a handler, grant or target is ready.
// makeOperationInputRegistry назначает explicit resource/body/option variants каждой known command.
// Это data contract, не декларация готовности handler, grant либо target.
func makeOperationInputRegistry() map[string]operationInputSpec {
	registry := map[string]operationInputSpec{}
	add := func(keys, mode, body, options, requiredOptions string) {
		for _, key := range strings.Fields(keys) {
			spec := operationInputSpec{options: strings.Fields(options), requiredOptions: strings.Fields(requiredOptions), body: body}
			switch mode {
			case "cluster":
				spec.cluster = true
			case "collection", "all":
				spec.fields = strings.Fields("group version resource namespace selector")
				spec.required = strings.Fields("group version resource namespace")
				spec.collection, spec.all = true, mode == "all"
			case "object", "effect":
				spec.fields = strings.Fields("group version resource namespace name uid resourceVersion subresource")
				spec.required = strings.Fields("group version resource namespace name")
				spec.existing = mode == "effect"
				if spec.existing {
					spec.required = append(spec.required, "uid", "resourceVersion")
				}
			}
			if body != "" {
				spec.fields = append(spec.fields, "body")
				if body != "delete" {
					spec.required = append(spec.required, "body")
				}
			}
			if len(spec.options) > 0 {
				spec.fields = append(spec.fields, "options")
				if len(spec.requiredOptions) > 0 {
					spec.required = append(spec.required, "options")
				}
			}
			if strings.HasPrefix(key, "kubernetes.status.") || key == "kubernetes.scale" {
				spec.required = append(spec.required, "subresource")
			}
			registry[key] = spec
		}
	}
	add("kubernetes.get kubernetes.status.get", "object", "", "", "")
	add("kubernetes.list kubernetes.events", "collection", "", "limit continue fieldSelector resourceVersion resourceVersionMatch", "")
	add("kubernetes.list.all-namespaces", "all", "", "limit continue fieldSelector resourceVersion resourceVersionMatch", "")
	add("kubernetes.watch", "collection", "", "timeoutSeconds sendInitialEvents allowWatchBookmarks resourceVersion fieldSelector", "timeoutSeconds")
	add("kubernetes.watch.all-namespaces", "all", "", "timeoutSeconds sendInitialEvents allowWatchBookmarks resourceVersion fieldSelector", "timeoutSeconds")
	add("kubernetes.create", "collection", "full", "fieldManager", "")
	add("kubernetes.replace kubernetes.edit kubernetes.status.update", "effect", "full", "fieldManager", "")
	add("kubernetes.apply", "object", "full", "fieldManager forceConflicts", "fieldManager")
	add("kubernetes.patch kubernetes.status.patch", "effect", "patch", "patchType fieldManager", "patchType")
	add("kubernetes.delete", "effect", "delete", "", "")
	add("kubernetes.scale", "effect", "", "replicas currentReplicas", "replicas")
	add("kubernetes.label kubernetes.annotate", "effect", "", "assignments removals overwrite", "")
	add("kubernetes.describe", "object", "", "includeEvents", "")
	add("kubernetes.logs", "object", "", "container previous sinceSeconds tailLines timestamps limitBytes follow timeoutSeconds", "timeoutSeconds")
	add("kubernetes.top", "collection", "", "containers", "")
	add("kubernetes.wait", "object", "", "condition conditionValue timeoutSeconds", "condition timeoutSeconds")
	add("kubernetes.discovery", "cluster", "", "apiGroup namespaced verbs", "")
	add("kubernetes.explain", "collection", "", "fieldPath recursive", "")
	add("kubernetes.version kubernetes.cluster-info kubernetes.identity kubernetes.config.metadata", "cluster", "", "", "")
	add("kubernetes.access", "collection", "", "verb", "verb")
	add("kubernetes.diff", "object", "full", "fieldManager", "")
	add("kubernetes.run", "collection", "", "podName image command args environment restartPolicy port labels", "podName image")
	add("kubernetes.expose", "effect", "", "serviceName ports serviceType selectorAssignments", "serviceName ports")
	add("kubernetes.autoscale", "effect", "", "minimumReplicas maximumReplicas cpuPercent", "maximumReplicas")
	add("kubernetes.set.image", "effect", "", "assignments", "assignments")
	add("kubernetes.set.env", "effect", "", "assignments removals containerNames", "")
	add("kubernetes.set.resources", "effect", "", "requests limits containerNames", "")
	add("kubernetes.set.selector", "effect", "", "selectorAssignments", "selectorAssignments")
	add("kubernetes.set.serviceaccount", "effect", "", "serviceAccount", "serviceAccount")
	add("kubernetes.set.subject", "effect", "", "subjects", "subjects")
	add("kubernetes.rollout.history", "object", "", "revision", "")
	add("kubernetes.rollout.status", "object", "", "revision timeoutSeconds", "timeoutSeconds")
	add("kubernetes.rollout.restart kubernetes.rollout.pause kubernetes.rollout.resume", "effect", "", "", "")
	add("kubernetes.rollout.undo", "effect", "", "revision", "revision")
	add("kubernetes.cordon kubernetes.uncordon kubernetes.certificate.approve kubernetes.certificate.deny", "effect", "", "", "")
	add("kubernetes.taint", "effect", "", "taints removals overwrite", "")
	add("kubernetes.drain", "effect", "", "gracePeriodSeconds timeoutSeconds deleteEmptyDirData ignoreDaemonSets podSelector", "timeoutSeconds")
	add("kubernetes.auth.reconcile", "effect", "full", "removeExtraPermissions removeExtraSubjects", "")
	add("kubernetes.exec", "effect", "", "container command stdin tty timeoutSeconds", "command timeoutSeconds")
	add("kubernetes.attach", "effect", "", "container stdin tty timeoutSeconds", "timeoutSeconds")
	add("kubernetes.debug", "effect", "", "container image command args targetContainer timeoutSeconds", "image timeoutSeconds")
	add("kubernetes.cp.upload", "effect", "", "container artifactRef artifactSHA256 transferFormat remotePath maximumBytes timeoutSeconds", "artifactRef artifactSHA256 transferFormat remotePath maximumBytes timeoutSeconds")
	add("kubernetes.cp.download", "effect", "", "container transferFormat remotePath maximumBytes timeoutSeconds", "transferFormat remotePath maximumBytes timeoutSeconds")
	add("kubernetes.port-forward", "effect", "", "ports timeoutSeconds", "ports timeoutSeconds")
	add("kubernetes.kustomize", "cluster", "", "artifactRef artifactSHA256 entryPath", "artifactRef artifactSHA256 entryPath")
	add("kubernetes.help", "cluster", "", "commandName", "")
	for _, operation := range strings.Fields("kubernetes.cordon kubernetes.uncordon kubernetes.taint kubernetes.drain kubernetes.certificate.approve kubernetes.certificate.deny") {
		spec := registry[operation]
		spec.cluster = true
		registry[operation] = spec
	}
	for _, operation := range strings.Fields("kubernetes.create kubernetes.replace kubernetes.apply kubernetes.edit kubernetes.patch") {
		spec := registry[operation]
		spec.options = append(spec.options, "secretMaterialRef")
		registry[operation] = spec
	}
	for operation, spec := range registry {
		if !slices.Contains(strings.Fields("kubernetes.list kubernetes.list.all-namespaces kubernetes.events kubernetes.watch kubernetes.watch.all-namespaces kubernetes.top"), operation) {
			spec.fields = slices.DeleteFunc(spec.fields, func(field string) bool { return field == "selector" })
		}
		registry[operation] = spec
	}
	return registry
}

// operationSchema exposes only registered keys; resource bodies retain their GVR-specific shape
// until the broker validates the pinned discovery schema and the complete current object.
// operationSchema открывает только registered keys; resource bodies сохраняют GVR-specific shape
// до проверки broker pinned discovery schema и полного current object.
func operationSchema(spec operationInputSpec) map[string]any {
	properties := map[string]any{}
	for _, field := range spec.fields {
		switch field {
		case "body":
			properties[field] = operationBodySchema(spec.body)
		case "options":
			options := map[string]any{}
			for _, key := range spec.options {
				options[key] = operationOptionSchema(key)
			}
			properties[field] = closedInputObject(options, spec.requiredOptions)
		default:
			maximum := 4096
			if field == "uid" || field == "resourceVersion" {
				maximum = 256
			}
			minimum := 1
			if field == "group" || field == "namespace" || field == "selector" {
				minimum = 0
			}
			properties[field] = inputString(minimum, maximum)
		}
	}
	document := closedInputObject(properties, spec.required)
	document["$schema"] = "https://json-schema.org/draft/2020-12/schema"
	return document
}

// operationBodySchema distinguishes complete resources, patch arrays and deletion metadata.
// Nullable values are intentional inside merge patches and JSON Patch values, never scalar options.
// operationBodySchema различает complete resources, patch arrays и deletion metadata.
// Nullable values намеренны внутри merge patches и JSON Patch values, не scalar options.
func operationBodySchema(mode string) map[string]any {
	switch mode {
	case "full":
		return map[string]any{"type": "object", "maxProperties": 128,
			"required": []string{"apiVersion", "kind", "metadata"}, "properties": map[string]any{
				"apiVersion": inputString(1, 320), "kind": map[string]any{"type": "string", "pattern": "^[A-Za-z][A-Za-z0-9]{0,127}$"},
				"metadata": map[string]any{"type": "object", "maxProperties": 64}}}
	case "patch":
		patchItem := closedInputObject(map[string]any{
			"op":   inputEnum("add", "remove", "replace", "move", "copy", "test"),
			"path": inputString(1, 4096), "from": inputString(1, 4096), "value": map[string]any{},
		}, []string{"op", "path"})
		patchItem["allOf"] = []any{
			map[string]any{"if": map[string]any{"properties": map[string]any{"op": inputEnum("add", "replace", "test")}}, "then": map[string]any{"required": []string{"value"}}},
			map[string]any{"if": map[string]any{"properties": map[string]any{"op": inputEnum("move", "copy")}}, "then": map[string]any{"required": []string{"from"}}},
		}
		return map[string]any{"oneOf": []any{
			map[string]any{"type": "object", "minProperties": 0, "maxProperties": 128},
			inputArray(patchItem, 0, 256),
		}}
	case "delete":
		return closedInputObject(map[string]any{
			"apiVersion": inputEnum("v1"), "kind": inputEnum("DeleteOptions"),
			"gracePeriodSeconds": inputInteger(0, 3600),
			"propagationPolicy":  inputEnum("Orphan", "Background", "Foreground"),
			"preconditions":      closedInputObject(map[string]any{"uid": inputString(1, 256), "resourceVersion": inputString(1, 256)}, nil),
		}, nil)
	default:
		return map[string]any{"not": map[string]any{}}
	}
}

// operationOptionSchema assigns bounded typed values to the shared command options vocabulary.
// Maps carry user object data only; no key can select a target, identity, transport URL or raw flag.
// operationOptionSchema назначает bounded typed values общему command options vocabulary.
// Maps несут только user object data; ключ не выбирает target, identity, transport URL либо raw flag.
func operationOptionSchema(key string) map[string]any {
	switch key {
	case "limit":
		return inputInteger(1, 500)
	case "timeoutSeconds":
		return inputInteger(1, 300)
	case "sinceSeconds", "gracePeriodSeconds":
		return inputInteger(0, 86400)
	case "tailLines":
		return inputInteger(1, 10000)
	case "limitBytes", "maximumBytes":
		return inputInteger(1, 16*1024*1024)
	case "replicas", "currentReplicas", "minimumReplicas", "maximumReplicas", "revision":
		return inputInteger(0, 100000)
	case "cpuPercent":
		return inputInteger(1, 1000)
	case "port":
		return inputInteger(1, 65535)
	case "patchType":
		return inputEnum("JSON", "MERGE", "STRATEGIC")
	case "resourceVersionMatch":
		return inputEnum("Exact", "NotOlderThan")
	case "verb":
		return inputEnum("get", "list", "watch", "create", "update", "patch", "delete", "deletecollection")
	case "restartPolicy":
		return inputEnum("Always", "OnFailure", "Never")
	case "serviceType":
		return inputEnum("ClusterIP", "NodePort", "LoadBalancer")
	case "condition":
		return inputEnum("Ready", "Available", "Complete", "Deleted", "Established")
	case "forceConflicts", "overwrite", "previous", "timestamps", "follow", "sendInitialEvents", "allowWatchBookmarks", "includeEvents",
		"containers", "recursive", "stdin", "tty", "deleteEmptyDirData", "ignoreDaemonSets", "namespaced", "removeExtraPermissions", "removeExtraSubjects":
		return map[string]any{"type": "boolean"}
	case "command", "args", "removals", "containerNames", "verbs", "fieldPath":
		return inputArray(inputString(1, 4096), 1, 128)
	case "assignments", "environment", "labels", "selectorAssignments", "requests", "limits":
		return map[string]any{"type": "object", "minProperties": 1, "maxProperties": 128,
			"propertyNames": inputString(1, 253), "additionalProperties": inputString(0, 4096)}
	case "subjects":
		return inputArray(closedInputObject(map[string]any{"kind": inputEnum("User", "Group", "ServiceAccount"),
			"name": inputString(1, 253), "namespace": inputString(1, 63), "apiGroup": inputEnum("", "rbac.authorization.k8s.io")}, []string{"kind", "name"}), 1, 128)
	case "ports":
		return inputArray(closedInputObject(map[string]any{
			"port": inputInteger(1, 65535), "targetPort": inputInteger(1, 65535),
			"protocol": inputEnum("TCP", "UDP", "SCTP"), "name": inputString(1, 63),
		}, []string{"port", "targetPort"}), 1, 16)
	case "taints":
		return inputArray(closedInputObject(map[string]any{"key": inputString(1, 253), "value": inputString(0, 63),
			"effect": inputEnum("NoSchedule", "PreferNoSchedule", "NoExecute")}, []string{"key", "effect"}), 1, 64)
	case "secretMaterialRef":
		return map[string]any{"type": "string", "pattern": "^krs_[A-Za-z0-9_-]{4,92}$", "maxLength": 96}
	case "transferFormat":
		return inputEnum("FILE", "TAR")
	case "artifactSHA256":
		return map[string]any{"type": "string", "pattern": "^[a-f0-9]{64}$"}
	default:
		return inputString(1, 4096)
	}
}

// closedInputObject rejects unknown and case-aliased property names without a reflection decoder.
// closedInputObject отклоняет unknown и case-aliased property names без reflection decoder.
func closedInputObject(properties map[string]any, required []string) map[string]any {
	result := map[string]any{"type": "object", "properties": properties, "additionalProperties": false}
	if len(required) > 0 {
		result["required"] = required
	}
	return result
}

// inputString specifies Unicode length bounds; byte and nesting limits are checked before decode.
// inputString задаёт Unicode length bounds; byte и nesting limits проверяются до decode.
func inputString(minimum, maximum int) map[string]any {
	return map[string]any{"type": "string", "minLength": minimum, "maxLength": maximum}
}

// inputInteger rejects fractional numbers and applies the finite command budget.
// inputInteger отклоняет fractional numbers и применяет конечный command budget.
func inputInteger(minimum, maximum int) map[string]any {
	return map[string]any{"type": "integer", "minimum": minimum, "maximum": maximum}
}

// inputEnum defines an exact case-sensitive vocabulary with no normalized fallback.
// inputEnum определяет exact case-sensitive vocabulary без normalized fallback.
func inputEnum(values ...string) map[string]any {
	return map[string]any{"type": "string", "enum": values}
}

// inputArray bounds both elements and cardinality before any owner plan can expand them.
// inputArray ограничивает elements и cardinality до раскрытия owner plan.
func inputArray(items map[string]any, minimum, maximum int) map[string]any {
	return map[string]any{"type": "array", "items": items, "minItems": minimum, "maxItems": maximum}
}
