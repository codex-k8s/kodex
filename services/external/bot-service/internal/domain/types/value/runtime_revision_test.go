package value

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestRuntimeRevisionCanonicalizationAndInfluencingFields(t *testing.T) {
	base := RuntimeRevisionInput{
		RoleID: 17, RoleName: "developer", RoleType: "worker",
		RoleUpdatedAt: time.Date(2026, 7, 21, 8, 0, 0, 123, time.UTC),
		Instruction:   "реализуй задачу", AdvancedSettings: `{"mode":"bounded"}`,
		AccountAlias: "codex-main", AuthorizationRevision: "auth-7",
		CodexAuthSecretRef: "codex-auth-main", GitHubSecretRef: "github-agent",
		RunnerImage:   "registry.invalid/runner@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		BotServiceURL: "http://bot-service.runtime.svc/", SandboxMode: "workspace-write",
		ConfigOverlay: "model = \"gpt-5\"",
		Repository:    RuntimeRepositoryManifest{Provider: "github", Owner: "codex-k8s", Name: "matter-codex", DefaultBranch: "main"},
		Environment: []RuntimeEnvironmentReference{
			{Name: "ZETA", SecretName: "runtime-env", SecretKey: "zeta"},
			{Name: "ALPHA", SecretName: "runtime-env", SecretKey: "alpha"},
		},
		KubernetesAccess: "read-only", ServiceAccountName: "matter-codex-agent-runner",
	}
	canonical, err := BuildRuntimeRevision(base)
	if err != nil {
		t.Fatalf("BuildRuntimeRevision() error = %v", err)
	}
	reordered := base
	reordered.Environment = []RuntimeEnvironmentReference{base.Environment[1], base.Environment[0], base.Environment[1]}
	same, err := BuildRuntimeRevision(reordered)
	if err != nil || same.Digest != canonical.Digest || same.ManifestJSON != canonical.ManifestJSON {
		t.Fatalf("порядок/дубликаты изменили каноническую ревизию: digest=%q error=%v", same.Digest, err)
	}
	conflictingOrder := base
	conflictingOrder.Environment = []RuntimeEnvironmentReference{
		{Name: "DUPLICATE", SecretName: "z-secret", SecretKey: "value"},
		{Name: "DUPLICATE", SecretName: "a-secret", SecretKey: "value"},
	}
	reversedConflictingOrder := conflictingOrder
	reversedConflictingOrder.Environment = []RuntimeEnvironmentReference{conflictingOrder.Environment[1], conflictingOrder.Environment[0]}
	firstConflict, err := BuildRuntimeRevision(conflictingOrder)
	if err != nil {
		t.Fatal(err)
	}
	secondConflict, err := BuildRuntimeRevision(reversedConflictingOrder)
	if err != nil || firstConflict.Digest != secondConflict.Digest || firstConflict.Manifest.Environment[0].SecretName != "a-secret" {
		t.Fatalf("конфликтующие имена env не нормализованы детерминированно: first=%#v second=%#v error=%v", firstConflict.Manifest.Environment, secondConflict.Manifest.Environment, err)
	}

	mutations := []struct {
		name   string
		mutate func(*RuntimeRevisionInput)
	}{
		{"role", func(value *RuntimeRevisionInput) { value.RoleName = "reviewer" }},
		{"role revision", func(value *RuntimeRevisionInput) { value.RoleUpdatedAt = value.RoleUpdatedAt.Add(time.Second) }},
		{"instruction", func(value *RuntimeRevisionInput) { value.Instruction += "!" }},
		{"account", func(value *RuntimeRevisionInput) { value.AccountAlias = "codex-secondary" }},
		{"authorization", func(value *RuntimeRevisionInput) { value.AuthorizationRevision = "auth-8" }},
		{"codex secret ref", func(value *RuntimeRevisionInput) { value.CodexAuthSecretRef = "codex-auth-next" }},
		{"github secret ref", func(value *RuntimeRevisionInput) { value.GitHubSecretRef = "github-next" }},
		{"image", func(value *RuntimeRevisionInput) { value.RunnerImage += "-next" }},
		{"endpoint", func(value *RuntimeRevisionInput) { value.BotServiceURL = "http://bot-service-next.runtime.svc" }},
		{"sandbox", func(value *RuntimeRevisionInput) { value.SandboxMode = "danger-full-access" }},
		{"overlay", func(value *RuntimeRevisionInput) { value.ConfigOverlay += "\nmodel_reasoning_effort = \"high\"" }},
		{"repository", func(value *RuntimeRevisionInput) { value.Repository.DefaultBranch = "release" }},
		{"environment", func(value *RuntimeRevisionInput) { value.Environment[0].SecretKey = "next" }},
		{"kubernetes access", func(value *RuntimeRevisionInput) { value.KubernetesAccess = "cluster-admin" }},
		{"service account", func(value *RuntimeRevisionInput) { value.ServiceAccountName = "cluster-admin-runner" }},
	}
	for _, mutation := range mutations {
		t.Run(mutation.name, func(t *testing.T) {
			changed := base
			changed.Environment = append([]RuntimeEnvironmentReference(nil), base.Environment...)
			mutation.mutate(&changed)
			revision, err := BuildRuntimeRevision(changed)
			if err != nil {
				t.Fatalf("BuildRuntimeRevision() error = %v", err)
			}
			if revision.Digest == canonical.Digest {
				t.Fatalf("влияющее поле %s не изменило digest", mutation.name)
			}
		})
	}
}

func TestRuntimeRevisionManifestContainsNoSecretValuesOrUntypedPayload(t *testing.T) {
	const syntheticSecret = "synthetic-runtime-secret-value-should-never-be-persisted"
	input := RuntimeRevisionInput{
		RoleID: 1, RoleName: "developer", RoleType: "worker", RoleUpdatedAt: time.Unix(1, 0),
		AccountAlias: "safe-alias", AuthorizationRevision: "revision-1",
		CodexAuthSecretRef: "safe-codex-ref", RunnerImage: "runner@sha256:safe", SandboxMode: "workspace-write",
		ConfigOverlay:    syntheticSecret,
		Environment:      []RuntimeEnvironmentReference{{Name: "SYNTHETIC_TOKEN", SecretName: "safe-runtime-ref", SecretKey: "token"}},
		KubernetesAccess: "read-only", ServiceAccountName: "safe-service-account",
	}
	revision, err := BuildRuntimeRevision(input)
	if err != nil {
		t.Fatalf("BuildRuntimeRevision() error = %v", err)
	}
	if strings.Contains(revision.ManifestJSON, syntheticSecret) {
		t.Fatal("значение синтетического секрета попало в канонический манифест")
	}
	var decoded RuntimeRevisionManifest
	if err := json.Unmarshal([]byte(revision.ManifestJSON), &decoded); err != nil {
		t.Fatalf("канонический манифест не декодируется в типизированный контракт: %v", err)
	}
	if decoded.Environment[0].SecretName != "safe-runtime-ref" || decoded.Sandbox.ConfigOverlaySHA256 == "" {
		t.Fatalf("безопасные ссылки или необратимый hash отсутствуют: %+v", decoded)
	}
}
