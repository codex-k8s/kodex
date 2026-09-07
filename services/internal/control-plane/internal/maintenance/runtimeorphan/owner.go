package runtimeorphan

import (
	"context"
	_ "embed"
	"os/exec"
	"strings"
)

//go:embed orphan_references.sql
var OwnerSQL string

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
