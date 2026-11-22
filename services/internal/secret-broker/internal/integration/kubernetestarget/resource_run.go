package kubernetestarget

import (
	"errors"
	"slices"
	"strings"

	"k8s.io/apimachinery/pkg/util/validation"
)

// prepareResourceRun builds one named Pod from the closed run options; it never reads an existing Pod or supplies credentials, host access, interactive streams or implicit cleanup.
// prepareResourceRun создаёт один именованный Pod из закрытых параметров run; не читает существующий Pod и не добавляет credentials, доступ к хосту, интерактивные потоки либо неявное удаление.
func (prepared *PreparedResourceOperation) prepareResourceRun(options map[string]any) (map[string]any, error) {
	address := prepared.address
	if address.Group != "" || address.Version != "v1" || address.Resource != "pods" || address.Subresource != "" || !address.Namespaced || address.AllNamespaces || address.Name != "" || address.UID != "" || address.ResourceVersion != "" || prepared.schema.kind.Kind != "Pod" {
		return nil, errors.New(resourceOperationInvalid)
	}
	name, nameOK := options["podName"].(string)
	image, imageOK := options["image"].(string)
	if !nameOK || len(validation.IsDNS1123Label(name)) != 0 || !imageOK || image == "" || image != strings.TrimSpace(image) || strings.ContainsAny(image, "\x00\r\n") {
		return nil, errors.New(resourceOperationInvalid)
	}
	container := map[string]any{"name": name, "image": image}
	for _, field := range []string{"command", "args"} {
		if raw, present := options[field]; present {
			values, ok := raw.([]any)
			if !ok || len(values) == 0 {
				return nil, errors.New(resourceOperationInvalid)
			}
			for _, raw := range values {
				value, ok := raw.(string)
				if !ok || strings.ContainsRune(value, '\x00') {
					return nil, errors.New(resourceOperationInvalid)
				}
			}
			container[field] = values
		}
	}
	if raw, present := options["environment"]; present {
		values, ok := raw.(map[string]any)
		if !ok {
			return nil, errors.New(resourceOperationInvalid)
		}
		keys := make([]string, 0, len(values))
		for key, raw := range values {
			if _, ok := raw.(string); !ok || len(validation.IsRelaxedEnvVarName(key)) != 0 {
				return nil, errors.New(resourceOperationInvalid)
			}
			keys = append(keys, key)
		}
		slices.Sort(keys)
		environment := make([]any, 0, len(keys))
		for _, key := range keys {
			environment = append(environment, map[string]any{"name": key, "value": values[key]})
		}
		container["env"] = environment
	}
	if raw, present := options["port"]; present {
		port, ok := resourceInteger(raw)
		if !ok || port < 1 || port > 65535 {
			return nil, errors.New(resourceOperationInvalid)
		}
		container["ports"] = []any{map[string]any{"containerPort": port, "protocol": "TCP"}}
	}
	labels := map[string]any{"run": name}
	if raw, present := options["labels"]; present {
		values, ok := raw.(map[string]any)
		if !ok || len(values) == 0 {
			return nil, errors.New(resourceOperationInvalid)
		}
		for key, raw := range values {
			value, ok := raw.(string)
			if !ok || len(validation.IsQualifiedName(key)) != 0 || len(validation.IsValidLabelValue(value)) != 0 {
				return nil, errors.New(resourceOperationInvalid)
			}
		}
		labels = values
	}
	restart := "Always"
	if raw, present := options["restartPolicy"]; present {
		var ok bool
		restart, ok = raw.(string)
		if !ok || restart != "Always" && restart != "OnFailure" && restart != "Never" {
			return nil, errors.New(resourceOperationInvalid)
		}
	}
	return map[string]any{"apiVersion": "v1", "kind": "Pod", "metadata": map[string]any{"name": name, "namespace": address.Namespace, "labels": labels},
		"spec": map[string]any{"automountServiceAccountToken": false, "restartPolicy": restart, "containers": []any{container}}}, nil
}
