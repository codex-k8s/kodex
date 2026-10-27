package platform

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	serviceplatform "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

// queryAssistantAgentRuntimeInventoryFixture embeds the exact synthetic artifact inventory update
// used only by disposable component fixtures; production bootstrap/admission are unchanged.
// queryAssistantAgentRuntimeInventoryFixture встраивает точное синтетическое обновление artifact
// inventory только для одноразовых component fixtures; production bootstrap/admission не меняются.
//
//go:embed testdata/sql/assistant_agent_runtime_inventory_fixture.sql
var queryAssistantAgentRuntimeInventoryFixture string

// seedAssistantAgentRuntimeInventoryFixture seeds and reads back MISSING-tool inventory only for
// the exact platform-owned bootstrap artifact with no configured tools or prior evidence. Existing
// inventory returns unchanged; real image observation and provider/build execution are not
// performed.
// seedAssistantAgentRuntimeInventoryFixture создаёт и перечитывает MISSING-tool inventory только
// для точного platform-owned bootstrap artifact без configured tools или прежнего evidence. При
// существующем inventory возвращается без изменений; реальное наблюдение image и исполнение
// provider/build не выполняются.
func seedAssistantAgentRuntimeInventoryFixture(t *testing.T, ctx context.Context, repository *Repository, service *serviceplatform.Service, owner value.Principal, agentRef string) {
	t.Helper()
	view, err := service.GetAgentRuntimeConfiguration(ctx, owner, agentRef)
	if err != nil {
		t.Fatal("read runtime inventory fixture target", err)
	}
	resolved, err := repository.ResolvePrincipal(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	current, err := repository.resolveScope(ctx, resolved)
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := scanRoleImageArtifact(repository.pool.QueryRow(ctx, queryRoleImagesGetActiveArtifact, current.organizationID, view.Environment.CurrentVersion.Image.ArtifactRef))
	if err != nil {
		t.Fatal("read exact runtime inventory fixture artifact", err)
	}
	if artifact.ToolInventory != nil {
		return
	}
	if !view.Environment.CurrentVersion.Image.PlatformOwnedBootstrap || artifact.ToolInventorySHA256 != "" || len(view.Environment.CurrentVersion.Tools) != 0 {
		t.Fatal("unsupported synthetic inventory fixture source")
	}
	raw, digest := imageInventoryFixture(artifact)
	if _, err := runtimecontract.DecodeImageToolInventory([]byte(raw)); err != nil {
		t.Fatal("invalid synthetic runtime inventory", err)
	}
	var ref string
	err = repository.pool.QueryRow(ctx, queryAssistantAgentRuntimeInventoryFixture, pgx.StrictNamedArgs{"organization_id": current.organizationID, "artifact_ref": artifact.Ref, "manifest_digest": artifact.ManifestDigest, "provenance_sha256": artifact.ProvenanceSHA256, "inventory_json": raw, "inventory_sha256": digest}).Scan(&ref)
	if err != nil || ref != artifact.Ref {
		t.Fatal("persist exact synthetic runtime inventory", err)
	}
	readback, err := scanRoleImageArtifact(repository.pool.QueryRow(ctx, queryRoleImagesGetActiveArtifact, current.organizationID, artifact.Ref))
	if err != nil || readback.ToolInventory == nil || readback.ToolInventorySHA256 != digest {
		t.Fatal("read back exact synthetic runtime inventory", err)
	}
}

// TestAssistantAgentRuntimeConfigurationProjectionExcludesPrivateData checks the eighteen-field
// runtime whitelist preserves configured tool names/binding pins while omitting sentinel config,
// values and secret descriptors, and rejects foreign/zero binding and inventory image drift.
// TestAssistantAgentRuntimeConfigurationProjectionExcludesPrivateData проверяет, что runtime
// список восемнадцати разрешённых полей сохраняет configured tool names/binding pins и исключает
// sentinel config, values и secret descriptors; отклоняет foreign/zero binding и изменение
// inventory image.
func TestAssistantAgentRuntimeConfigurationProjectionExcludesPrivateData(t *testing.T) {
	digest := strings.Repeat("a", 64)
	agent := entity.Agent{Ref: "agt_fixture123", ProjectRef: "prj_fixture123", Version: 9}
	artifact := entity.ImageArtifact{SpecSHA256: digest, ImmutableBuildSHA256: digest, RoleRuntimeContractSHA256: digest, ManifestDigest: "sha256:" + digest, ProvenanceSHA256: digest}
	inventoryRaw, inventoryDigest := imageInventoryFixture(artifact)
	inventory, err := runtimecontract.DecodeImageToolInventory([]byte(inventoryRaw))
	if err != nil {
		t.Fatal(err)
	}
	view := entity.AgentRuntimeConfigurationView{AgentVersion: agent.Version,
		Configuration:       entity.AgentRuntimeConfiguration{Ref: "arcfg_fixture123", AgentRef: agent.Ref, Version: 5, Digest: digest, Provider: "openai-codex", Model: "gpt-6.1-sol"},
		EnvironmentBinding:  entity.AgentRuntimeEnvironmentBinding{Ref: "aenv_fixture123", AgentRef: agent.Ref, EnvironmentRef: "renv_fixture123", VersionRef: "renvv_fixture123", Version: 3, Digest: digest},
		SafeEffectiveConfig: "PRIVATE_SENTINEL",
		Environment: entity.RuntimeEnvironmentSet{Ref: "renv_fixture123", ProjectRef: agent.ProjectRef, Name: "Developer", Version: 8,
			CurrentVersion: entity.RuntimeEnvironmentVersion{Ref: "renvv_fixture123", Revision: 4, Digest: digest, Policy: runtimecontract.DefaultRuntimeEnvironmentPolicy(),
				Image:             entity.RuntimeEnvironmentImage{ArtifactRef: "imgart_fixture123", RecipeRef: "recipe_fixture123", RecipeGeneration: 2, Reference: "registry.test/runtime@sha256:" + digest, Digest: "sha256:" + digest},
				Tools:             []entity.RuntimeEnvironmentTool{{Name: "Настроенный git", Command: "PRIVATE_SENTINEL", Description: "PRIVATE_SENTINEL", UsageHint: "PRIVATE_SENTINEL"}},
				Values:            []entity.RuntimeEnvironmentValue{{Name: "PRIVATE_VALUE", Value: "PRIVATE_SENTINEL"}},
				SecretDescriptors: []entity.RuntimeSecretDescriptor{{Name: "PRIVATE_SECRET", SecretRef: "PRIVATE_SENTINEL"}}}}}
	raw, err := assistantAgentRuntimeConfigurationJSON(agent, view, &inventory, inventoryDigest)
	if err != nil || strings.Contains(string(raw), "PRIVATE_SENTINEL") || strings.Contains(string(raw), "PRIVATE_VALUE") || strings.Contains(string(raw), "PRIVATE_SECRET") {
		t.Fatal("private runtime fields exposed", err)
	}
	var snapshot map[string]any
	if json.Unmarshal(raw, &snapshot) != nil || len(snapshot) != 18 || snapshot["configured_tools"].([]any)[0] != "Настроенный git" || snapshot["published_version_ref"] != view.EnvironmentBinding.VersionRef {
		t.Fatal("binding pins or configured tool names lost")
	}
	for _, mutate := range []func(*entity.AgentRuntimeConfigurationView){
		func(v *entity.AgentRuntimeConfigurationView) { v.EnvironmentBinding.AgentRef = "agt_foreign123" },
		func(v *entity.AgentRuntimeConfigurationView) { v.EnvironmentBinding.EnvironmentRef = "renv_foreign123" },
		func(v *entity.AgentRuntimeConfigurationView) { v.EnvironmentBinding.VersionRef = "renvv_foreign123" },
		func(v *entity.AgentRuntimeConfigurationView) { v.EnvironmentBinding.Version = 0 },
		func(v *entity.AgentRuntimeConfigurationView) {
			v.Environment.CurrentVersion.Image.Digest = "sha256:" + strings.Repeat("b", 64)
		},
	} {
		bad := view
		mutate(&bad)
		if _, err := assistantAgentRuntimeConfigurationJSON(agent, bad, &inventory, inventoryDigest); !errors.Is(err, errs.ErrUnavailable) {
			t.Fatal("foreign binding or inventory accepted")
		}
	}
}

// TestAssistantAgentRuntimeConfigurationCanonicalAdmissionInventory checks typed,
// newline-terminated and sorted-key admission inventories against exact source/provenance binding
// and one shared contract golden. It verifies projected canonical inventory hashes are distinct
// from source receipts when bytes differ and rejects malformed source digests without live
// admission.
// TestAssistantAgentRuntimeConfigurationCanonicalAdmissionInventory проверяет typed, завершённый
// newline и sorted-key admission inventory по точной source/provenance binding и общему contract
// golden. Подтверждает различие projected canonical inventory hashes и source receipts при разных
// bytes и отклоняет malformed source digests без живого admission.
func TestAssistantAgentRuntimeConfigurationCanonicalAdmissionInventory(t *testing.T) {
	digest := strings.Repeat("a", 64)
	artifact := entity.ImageArtifact{SpecSHA256: digest, ImmutableBuildSHA256: digest, RoleRuntimeContractSHA256: digest, ManifestDigest: "sha256:" + digest, ProvenanceSHA256: digest, Platforms: []entity.RoleImagePlatform{{OS: "linux", Architecture: "amd64"}}}
	typedRaw, _ := imageInventoryFixture(artifact)
	var sorted map[string]any
	if json.Unmarshal([]byte(typedRaw), &sorted) != nil {
		t.Fatal("decode fixture")
	}
	sortedRaw, err := json.Marshal(sorted)
	if err != nil || string(sortedRaw) == typedRaw {
		t.Fatal("fixture must reproduce distinct sorted admission order", err)
	}
	agent := entity.Agent{Ref: "agt_recipient123", ProjectRef: "prj_context123", Version: 9}
	view := entity.AgentRuntimeConfigurationView{AgentVersion: agent.Version,
		Configuration:      entity.AgentRuntimeConfiguration{Ref: "arcfg_fixture123", AgentRef: agent.Ref, Version: 5, Digest: digest, Provider: "openai-codex", Model: "gpt-6.1-sol"},
		EnvironmentBinding: entity.AgentRuntimeEnvironmentBinding{Ref: "aenv_binding123", AgentRef: agent.Ref, EnvironmentRef: "renv_fixture123", VersionRef: "renvv_fixture123", Version: 7, Digest: digest},
		Environment: entity.RuntimeEnvironmentSet{Ref: "renv_fixture123", ProjectRef: agent.ProjectRef, Name: "Окружение Developer", Version: 12,
			CurrentVersion: entity.RuntimeEnvironmentVersion{Ref: "renvv_fixture123", Revision: 4, Digest: digest, Policy: runtimecontract.DefaultRuntimeEnvironmentPolicy(),
				Image: entity.RuntimeEnvironmentImage{ArtifactRef: "imgart_fixture123", RecipeRef: "recipe_fixture123", RecipeGeneration: 2, Reference: "registry.test/runtime@sha256:" + digest, Digest: "sha256:" + digest},
				Tools: []entity.RuntimeEnvironmentTool{{Name: "Configured tool"}}}}}
	golden, err := os.ReadFile("../../../../../../../contracts/proto/testdata/assistant_agent_runtime_configuration.json")
	if err != nil {
		t.Fatal("read shared RC contract fixture", err)
	}
	// jq -sc в image-admission.sh сохраняет конечный LF; admission bridge
	// хеширует exact file, тогда как json.Marshal struct не добавляет LF.
	for name, admissionRaw := range map[string][]byte{
		"typed_order":       []byte(typedRaw),
		"admission_file_LF": []byte(typedRaw + "\n"),
		"sorted_keys":       sortedRaw,
	} {
		t.Run(name, func(t *testing.T) {
			current := artifact
			current.ToolInventorySHA256 = runtimecontract.ImageInventorySHA256(admissionRaw)
			if err := hydrateArtifactToolInventory(&current, string(admissionRaw)); err != nil {
				t.Fatal("valid exact admission source rejected", err)
			}
			bad := current
			bad.ToolInventorySHA256 = strings.Repeat("b", 64)
			if hydrateArtifactToolInventory(&bad, string(admissionRaw)) == nil {
				t.Fatal("source raw hash drift accepted")
			}
			bad = current
			bad.ProvenanceSHA256 = strings.Repeat("b", 64)
			if hydrateArtifactToolInventory(&bad, string(admissionRaw)) == nil {
				t.Fatal("source artifact provenance drift accepted")
			}
			raw, err := assistantAgentRuntimeConfigurationJSON(agent, view, current.ToolInventory, current.ToolInventorySHA256)
			if err != nil {
				t.Fatal("hydrated exact admission inventory projection rejected", err)
			}
			var snapshot map[string]any
			if json.Unmarshal(raw, &snapshot) != nil {
				t.Fatal("decode projection")
			}
			projectedInventory, err := json.Marshal(snapshot["verified_tool_inventory"])
			if err != nil || runtimecontract.ImageInventorySHA256(projectedInventory) != snapshot["verified_tool_inventory_sha256"] {
				t.Fatal("projected inventory hash does not bind exact delivered bytes", err)
			}
			if name == "admission_file_LF" && snapshot["verified_tool_inventory_sha256"] == current.ToolInventorySHA256 {
				t.Fatal("projected SHA became original file receipt SHA")
			}
			if !bytes.Equal(raw, bytes.TrimSuffix(golden, []byte("\n"))) {
				t.Fatal("producer projection differs from shared RC contract fixture")
			}
			if _, err := assistantAgentRuntimeConfigurationJSON(agent, view, current.ToolInventory, "invalid"); !errors.Is(err, errs.ErrUnavailable) {
				t.Fatal("malformed trusted source receipt accepted")
			}
		})
	}
}
