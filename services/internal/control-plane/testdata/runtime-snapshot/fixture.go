// Package runtimesnapshot содержит только обезличенную межмодульную test fixture.
package runtimesnapshot

import (
	_ "embed"
	"encoding/json"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

// snapshot embeds the synthetic owner snapshot shared by module fixtures; it contains no live data.
// snapshot встраивает синтетический owner snapshot для межмодульных fixtures без живых данных.
//
//go:embed snapshot.json
var snapshot []byte

// Claim embeds the synthetic claim envelope paired with the snapshot for consumer contract tests.
// Claim встраивает синтетический claim envelope, связанный со snapshot для consumer contract tests.
//
//go:embed claim.json
var Claim []byte

// Snapshot decodes the embedded synthetic snapshot and restores exact typed runtime values without
// recomputing its existing digest or reading live owner data.
// Snapshot декодирует встроенный синтетический snapshot и восстанавливает точные типизированные
// runtime значения без пересчёта существующего digest либо чтения живых данных owner.
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
