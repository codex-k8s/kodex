package runtimecontract

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"reflect"
	"testing"
)

// validWorkspacePolicy returns a fresh canonical default for isolated policy mutations in tests.
// validWorkspacePolicy возвращает новую каноническую политику для изолированных изменений в тестах.
func validWorkspacePolicy() RuntimeWorkspacePolicy {
	return RuntimeWorkspacePolicyV1()
}

// TestRuntimeWorkspacePolicyNormalizesAndAppliesLongestPrefix checks relative paths, protected
// subtrees, writable results, and rejection of absolute or traversal-based workspace escapes.
// TestRuntimeWorkspacePolicyNormalizesAndAppliesLongestPrefix проверяет относительные пути,
// защищённые папки, запись результатов и отказ при абсолютном пути вне workspace или traversal.
func TestRuntimeWorkspacePolicyNormalizesAndAppliesLongestPrefix(t *testing.T) {
	policy := validWorkspacePolicy()
	for _, test := range []struct{ candidate, access, denial string }{
		{RuntimeWorkspaceWorkingRoot + "/repository/main.go", RuntimeWorkspaceWritable, ""},
		{"work/attachment.txt", RuntimeWorkspaceWritable, ""},
		{".kodex/outbox/result.md", RuntimeWorkspaceWritable, ""},
		{"/workspace/input/set/files/a.txt", RuntimeWorkspaceReadOnly, ""},
		{"/workspace/knowledge/memory.md", RuntimeWorkspaceReadOnly, ""},
		{"/workspace/context/skills/skill/SKILL.md", RuntimeWorkspaceReadOnly, ""},
		{"context/memory/records.json", RuntimeWorkspaceReadOnly, ""},
		{"/workspace/.kodex/state/codex-home/auth.json", RuntimeWorkspaceReadOnly, ""},
		{"../other/session", "", RuntimeWorkspacePathOutsideWorkspace},
		{"/workspace/input/../../other", "", RuntimeWorkspacePathOutsideWorkspace},
		{"/other/project", "", RuntimeWorkspacePathOutsideWorkspace},
	} {
		access, denial := policy.AccessForPath(test.candidate)
		if access != test.access || denial != test.denial {
			t.Errorf("AccessForPath(%q) = (%q, %q), want (%q, %q)", test.candidate, access, denial, test.access, test.denial)
		}
	}
}

// TestRuntimeWorkspacePolicyRejectsDigestAndUnsafePath rejects a changed digest and a noncanonical rule path.
// TestRuntimeWorkspacePolicyRejectsDigestAndUnsafePath отклоняет изменённый дайджест и неканонический путь правила.
func TestRuntimeWorkspacePolicyRejectsDigestAndUnsafePath(t *testing.T) {
	p := validWorkspacePolicy()
	if err := p.Validate(); err != nil {
		t.Fatal(err)
	}
	p.Digest = "bad"
	if err := p.Validate(); err == nil {
		t.Fatal("digest mismatch accepted")
	}
	p = validWorkspacePolicy()
	p.Rules[0].Path = "/workspace/../etc"
	raw, _ := json.Marshal(p)
	sum := sha256.Sum256(raw)
	p.Digest = hex.EncodeToString(sum[:])
	if err := p.Validate(); err == nil {
		t.Fatal("unsafe path accepted")
	}
}

// TestRuntimeWorkspacePolicyAcceptsBoundedResourceQuotaWithoutChangingPaths permits lower quotas
// after recomputing the digest while retaining the exact protected path rules.
// TestRuntimeWorkspacePolicyAcceptsBoundedResourceQuotaWithoutChangingPaths допускает меньшие квоты
// после пересчёта дайджеста при сохранении точных правил защищённых путей.
func TestRuntimeWorkspacePolicyAcceptsBoundedResourceQuotaWithoutChangingPaths(t *testing.T) {
	base := RuntimeWorkspacePolicyV1()
	limited := base
	limited.MaximumWritableBytes = 4096
	limited.MaximumFileCount = 10
	limited.Digest = ""
	raw, _ := json.Marshal(limited)
	sum := sha256.Sum256(raw)
	limited.Digest = hex.EncodeToString(sum[:])
	if limited.Validate() != nil || !reflect.DeepEqual(limited.Rules, base.Rules) {
		t.Fatal("bounded quota rejected or protected rules changed")
	}
}

// TestRuntimeWorkspaceLimitsBoundsAndNilPreserveCanonicalPolicy checks nil/default equivalence,
// closed quota bounds, and a new digest for valid narrower limits without path changes.
// TestRuntimeWorkspaceLimitsBoundsAndNilPreserveCanonicalPolicy проверяет соответствие nil и default,
// пределы квот и новый дайджест допустимых меньших лимитов без изменения путей.
func TestRuntimeWorkspaceLimitsBoundsAndNilPreserveCanonicalPolicy(t *testing.T) {
	base := RuntimeWorkspacePolicyV1()
	nilPolicy, err := RuntimeWorkspacePolicyWithLimits(nil)
	if err != nil || !reflect.DeepEqual(base, nilPolicy) {
		t.Fatal("absent limits changed immutable default")
	}
	for _, limits := range []RuntimeWorkspaceLimits{
		{MaxBytes: 0, MaxFiles: 10}, {MaxBytes: 1, MaxFiles: 0},
		{MaxBytes: RuntimeWorkspaceWritableBytes + 1, MaxFiles: 10},
		{MaxBytes: 1, MaxFiles: RuntimeWorkspaceMaximumFiles + 1},
	} {
		if _, err := RuntimeWorkspacePolicyWithLimits(&limits); err == nil {
			t.Fatal("invalid or excessive limits accepted")
		}
	}
	limits := &RuntimeWorkspaceLimits{MaxBytes: 4096, MaxFiles: 10}
	policy, err := RuntimeWorkspacePolicyWithLimits(limits)
	if err != nil || policy.Validate() != nil || policy.Root != base.Root ||
		!reflect.DeepEqual(policy.Rules, base.Rules) || policy.Digest == base.Digest {
		t.Fatal("quota did not preserve protected policy or digest binding")
	}
}

// TestRepositoryWorkspaceFileQuota preserves the default while allowing an owner
// to select a larger bounded metadata quota for a dependency-heavy repository.
// TestRepositoryWorkspaceFileQuota сохраняет default и разрешает владельцу
// выбрать большую ограниченную квоту метаданных для репозитория с зависимостями.
func TestRepositoryWorkspaceFileQuota(t *testing.T) {
	base := RuntimeWorkspacePolicyV1()
	if base.MaximumFileCount != 10_000 {
		t.Fatal("default workspace quota changed")
	}
	selected, err := RuntimeWorkspacePolicyWithLimits(&RuntimeWorkspaceLimits{MaxBytes: base.MaximumWritableBytes, MaxFiles: 50_000})
	if err != nil || selected.Validate() != nil || selected.MaximumFileCount != 50_000 || selected.Digest == base.Digest || !reflect.DeepEqual(selected.Rules, base.Rules) {
		t.Fatal("explicit repository quota lost its protected policy or binding")
	}
}
