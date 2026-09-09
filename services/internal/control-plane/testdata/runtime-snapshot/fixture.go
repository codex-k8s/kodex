// Package runtimesnapshot содержит только обезличенную межмодульную test fixture.
package runtimesnapshot

import (
	_ "embed"
	"encoding/json"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

//go:embed snapshot.json
var snapshot []byte

//go:embed claim.json
var Claim []byte

// Snapshot восстанавливает точные типы owner map, не вычисляя и не обновляя digest.
func Snapshot(t testing.TB) map[string]any {
	t.Helper()
	var values map[string]any
	if err := json.Unmarshal(snapshot, &values); err != nil {
		t.Fatal(err)
	}
	var typed struct {
		EnvironmentImage          runtimecontract.RuntimeEnvironmentImage
		EnvironmentTools          []runtimecontract.RuntimeEnvironmentTool
		EnvironmentPolicy         runtimecontract.RuntimeEnvironmentPolicy
		EffectiveKubernetesAccess runtimecontract.RuntimeKubernetesAccess
		WorkspacePolicy           entity.RuntimeWorkspacePolicy
		IntegrationGrants         []map[string]string
		AttachmentSets            []map[string]string
		Artifacts                 []map[string]any
	}
	if err := json.Unmarshal(snapshot, &typed); err != nil {
		t.Fatal(err)
	}
	values["environmentImage"] = typed.EnvironmentImage
	values["environmentTools"] = typed.EnvironmentTools
	values["environmentPolicy"] = typed.EnvironmentPolicy
	values["effectiveKubernetesAccess"] = typed.EffectiveKubernetesAccess
	values["workspacePolicy"] = typed.WorkspacePolicy
	values["integrationGrants"] = typed.IntegrationGrants
	values["attachmentSets"] = typed.AttachmentSets
	values["artifacts"] = typed.Artifacts
	return values
}
