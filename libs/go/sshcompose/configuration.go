package sshcompose

import (
	"encoding/json"
	"strconv"
)

// TargetFromConfiguration derives a deterministic public target from the exact closed fields.
// TargetFromConfiguration выводит детерминированный public target из закрытого набора полей.
func TargetFromConfiguration(configuration map[string]string) (Target, error) {
	if len(configuration) != 9 || !refPattern.MatchString(configuration["ssh_secret_ref"]) {
		return Target{}, ErrorInvalid
	}
	return targetFromPublicValues(configuration)
}

// TargetFromResourceScope validates the exact public scope and recomputes its target reference before display or use.
// TargetFromResourceScope проверяет exact public scope и пересчитывает target reference до показа либо использования.
func TargetFromResourceScope(values map[string]string) (Target, error) {
	if len(values) != 9 {
		return Target{}, ErrorInvalid
	}
	target, err := targetFromPublicValues(values)
	if err != nil || values["target_ref"] != target.Ref {
		return Target{}, ErrorInvalid
	}
	return target, nil
}

// targetFromPublicValues derives one target from the shared eight public configuration pins.
// targetFromPublicValues выводит target из общих восьми public configuration pins.
func targetFromPublicValues(configuration map[string]string) (Target, error) {
	keys := []string{"project_ref", "owner", "repository", "address", "user", "host_key", "published_port", "runtime_image"}
	scope := make(map[string]string, len(keys))
	for _, key := range keys {
		value, ok := configuration[key]
		if !ok || value == "" {
			return Target{}, ErrorInvalid
		}
		scope[key] = value
	}
	raw, err := json.Marshal(scope)
	port, portErr := strconv.Atoi(configuration["published_port"])
	if err != nil || portErr != nil || strconv.Itoa(port) != configuration["published_port"] {
		return Target{}, ErrorInvalid
	}
	target := Target{Ref: "ssh_" + Digest(raw)[:32], ProjectRef: scope["project_ref"], Repository: scope["owner"] + "/" + scope["repository"],
		Address: scope["address"], Port: 22, User: scope["user"], HostKey: scope["host_key"], PublishedPort: port, RuntimeImage: scope["runtime_image"]}
	return target, target.Validate()
}
