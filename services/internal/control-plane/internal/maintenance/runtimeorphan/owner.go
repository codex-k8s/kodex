package runtimeorphan

import (
	"context"
	_ "embed"
	"os/exec"
	"strings"
)

// OwnerSQL embeds the operator-only read query for authoritative references to the exact
// Secret locators; this package does not expose database access to application callers.
// OwnerSQL встраивает только операторский запрос чтения авторитетных references по точным
// Secret locators; пакет не предоставляет доступ к базе прикладным callers.
//
//go:embed orphan_references.sql
var OwnerSQL string

// OwnerReferences runs the embedded read-only owner-reference query through the fixed operator
// kubectl/psql path with exact metadata locators, accepting only one boolean t/f response.
// OwnerReferences исполняет embedded read-only owner reference запрос через фиксированный
// операторский kubectl и psql путь с точными metadata locators, принимая только один boolean t/f
// ответ.
func OwnerReferences(ctx context.Context, d Descriptor) (bool, error) {
	command := exec.CommandContext(ctx, "kubectl", "-n", "kodex-system", "exec", "-i", "kodex-postgresql-0", "--",
		"psql", "-X", "-qAt", "-U", "postgres", "-d", "control_plane", "-v", "ON_ERROR_STOP=1",
		"-v", "secret_ref="+d.SecretRef, "-v", "secret_name="+d.Name, "-v", "secret_uid="+d.UID, "-v", "operation_ref="+d.Operation)
	command.Stdin = strings.NewReader(OwnerSQL)
	out, err := command.Output()
	if err != nil {
		return false, ErrGuard
	}
	answer := strings.TrimSpace(string(out))
	if answer != "t" && answer != "f" {
		return false, ErrGuard
	}
	return answer == "t", nil
}
