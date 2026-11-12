package runtimecontract

import (
	"slices"
	"strings"
	"testing"
)

// workspaceToolInputFixture derives an inert complete OpenCode input and explicitly grants
// artifact work; no model endpoint, filesystem, process or current owner authority is involved.
// workspaceToolInputFixture получает полный неактивный вход OpenCode и явно добавляет право
// на файлы; endpoint модели, filesystem, процессы и текущие полномочия владельца не используются.
func workspaceToolInputFixture() RunnerInput {
	input := modelProviderRunnerFixture()
	input.Capabilities = append(input.Capabilities, ArtifactCapability)
	input.AssistantScope = AssistantScopeNone
	input.WorkspaceAccess = "workspace-write"
	input.WorkspacePolicy = RuntimeWorkspacePolicyV1()
	input.EnvironmentPolicy = DefaultRuntimeEnvironmentPolicy()
	input.EnvironmentPolicy.ManagedWorkspace = true
	return input
}

// TestWorkspaceToolsKeepDefaultAndAssistantProfilesClosed checks each independent eligibility
// pin and proves the optional local catalog never mutates the existing controller catalog.
// TestWorkspaceToolsKeepDefaultAndAssistantProfilesClosed проверяет каждую независимую
// привязку допуска и неизменность существующего каталога контроллера при локальном opt-in.
func TestWorkspaceToolsKeepDefaultAndAssistantProfilesClosed(t *testing.T) {
	input := workspaceToolInputFixture()
	before := RuntimeMCPToolNames(input)
	if got := RuntimeWorkspaceToolNames(input); !slices.Equal(got, []string{WorkspaceToolList, WorkspaceToolRead, WorkspaceToolWrite}) {
		t.Fatal("explicit artifact profile lost local file tools", got)
	}
	if !slices.Equal(before, RuntimeMCPToolNames(input)) {
		t.Fatal("local workspace tools changed the controller catalog")
	}
	for name, mutate := range map[string]func(*RunnerInput){
		"read-only work":      func(i *RunnerInput) { i.WorkspaceAccess = "read-only" },
		"opted out":           func(i *RunnerInput) { i.EnvironmentPolicy.ManagedWorkspace = false },
		"Codex default":       func(i *RunnerInput) { i.Provider = "openai" },
		"assistant project":   func(i *RunnerInput) { i.AssistantScope = AssistantScopeProject },
		"assistant system":    func(i *RunnerInput) { i.AssistantScope = AssistantScopeSystem },
		"no capability":       func(i *RunnerInput) { i.Capabilities = nil },
		"no project":          func(i *RunnerInput) { i.ProjectRef = "" },
		"no lease":            func(i *RunnerInput) { i.LeaseRef = "" },
		"no fence":            func(i *RunnerInput) { i.LeaseFence = "" },
		"no generation":       func(i *RunnerInput) { i.LeaseGeneration = 0 },
		"no input digest":     func(i *RunnerInput) { i.InputDigest = "" },
		"no revision digest":  func(i *RunnerInput) { i.RuntimeRevisionDigest = "" },
		"no provider config":  func(i *RunnerInput) { i.ModelProvider = nil },
		"invalid provider":    func(i *RunnerInput) { i.ModelProvider.EngineVersion = "unqualified" },
		"invalid root policy": func(i *RunnerInput) { i.WorkspacePolicy.Root = "/" },
		"invalid quota":       func(i *RunnerInput) { i.WorkspacePolicy.MaximumFileCount = 0 },
		"invalid policy hash": func(i *RunnerInput) { i.WorkspacePolicy.Digest = strings.Repeat("0", 64) },
		"not a turn":          func(i *RunnerInput) { i.Mode = "WARM" },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := workspaceToolInputFixture()
			mutate(&candidate)
			if RuntimeWorkspaceToolsAvailable(candidate) || len(RuntimeWorkspaceToolNames(candidate)) != 0 {
				t.Fatal("unqualified profile advertised workspace access")
			}
		})
	}
}

// TestWorkspaceCommandSelectionRejectsShellStringsAndPaths requires exact membership in the
// published executable list; a display name, prefix, pathname or injected shell line never matches.
// TestWorkspaceCommandSelectionRejectsShellStringsAndPaths требует точного совпадения с
// опубликованным executable; имя для UI, prefix, путь либо shell-строка не подходят.
func TestWorkspaceCommandSelectionRejectsShellStringsAndPaths(t *testing.T) {
	input := workspaceToolInputFixture()
	if RuntimeWorkspaceCommandAllowed(input, "go") || slices.Contains(RuntimeWorkspaceToolNames(input), WorkspaceToolExecute) {
		t.Fatal("empty environment implicitly granted commands")
	}
	input.EnvironmentTools = []RuntimeEnvironmentTool{{Name: "Go toolchain", Command: "go", Description: "Build and test local source"}}
	if !RuntimeWorkspaceCommandAllowed(input, "go") || !slices.Contains(RuntimeWorkspaceToolNames(input), WorkspaceToolExecute) {
		t.Fatal("exact configured executable was lost")
	}
	for _, command := range []string{"", "Go toolchain", "golang", "Go", "/usr/bin/go", "../go", "go test", "go;id", "go\nprintenv", "$(id)", "bash"} {
		if RuntimeWorkspaceCommandAllowed(input, command) {
			t.Fatalf("unselected command accepted: %q", command)
		}
	}
	input.Capabilities = nil
	if RuntimeWorkspaceCommandAllowed(input, "go") {
		t.Fatal("environment tools replaced artifact eligibility")
	}
}

// workspaceCheckoutGrants returns three inert read descriptors with one Connection/version/package.
// workspaceCheckoutGrants создаёт три неактивных READ descriptors одной Connection/version/package.
func workspaceCheckoutGrants() []RunnerIntegrationGrant {
	var result []RunnerIntegrationGrant
	for _, operation := range []string{"gitlab.repository.tree.list", "gitlab.repository.file.read", "gitlab.commit.read"} {
		result = append(result, RunnerIntegrationGrant{Ref: "grt_fixture_" + strings.ReplaceAll(operation, ".", "_"),
			ConnectionRef: "int_fixture123", GrantVersion: 1, ConnectionVersion: 4,
			DefinitionKey: "gitlab", DefinitionDigest: strings.Repeat("a", 64),
			Operation: operation, CapabilityKey: operation, Risk: "READ"})
	}
	return result
}

// TestWorkspaceCheckoutCannotCombineForeignOrStaleGrants prevents a complete-looking trio from
// crossing Connection/version/package boundaries or treating an unrelated write as a read grant.
// TestWorkspaceCheckoutCannotCombineForeignOrStaleGrants запрещает собирать внешне полный набор
// из разных Connection/version/package и считать несвязанный WRITE правом чтения.
func TestWorkspaceCheckoutCannotCombineForeignOrStaleGrants(t *testing.T) {
	input := workspaceToolInputFixture()
	input.IntegrationGrants = workspaceCheckoutGrants()
	if !RuntimeWorkspaceCheckoutAvailable(input) || !slices.Contains(RuntimeWorkspaceToolNames(input), WorkspaceToolCheckout) {
		t.Fatal("complete same-connection checkout catalog was lost")
	}
	for name, mutate := range map[string]func(*RunnerInput){
		"missing tree":        func(i *RunnerInput) { i.IntegrationGrants = i.IntegrationGrants[1:] },
		"other connection":    func(i *RunnerInput) { i.IntegrationGrants[0].ConnectionRef = "int_other123" },
		"old version":         func(i *RunnerInput) { i.IntegrationGrants[0].ConnectionVersion-- },
		"old package":         func(i *RunnerInput) { i.IntegrationGrants[0].DefinitionDigest = strings.Repeat("b", 64) },
		"other provider":      func(i *RunnerInput) { i.IntegrationGrants[0].DefinitionKey = "github" },
		"write risk":          func(i *RunnerInput) { i.IntegrationGrants[0].Risk = "WRITE" },
		"wrong operation":     func(i *RunnerInput) { i.IntegrationGrants[0].Operation = "gitlab.issue.read" },
		"zero grant version":  func(i *RunnerInput) { i.IntegrationGrants[0].GrantVersion = 0 },
		"missing grant":       func(i *RunnerInput) { i.IntegrationGrants[0].Ref = "" },
		"duplicate file read": func(i *RunnerInput) { i.IntegrationGrants[0] = i.IntegrationGrants[1] },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := workspaceToolInputFixture()
			candidate.IntegrationGrants = workspaceCheckoutGrants()
			mutate(&candidate)
			if RuntimeWorkspaceCheckoutAvailable(candidate) || slices.Contains(RuntimeWorkspaceToolNames(candidate), WorkspaceToolCheckout) {
				t.Fatal("incomplete or foreign grant trio advertised checkout")
			}
		})
	}
}

// TestManagedWorkspacePathsNeverResolveProviderStateOrPlatformInputs checks canonical selectors,
// protected areas and traversal variants independently from later kernel-level path confinement.
// TestManagedWorkspacePathsNeverResolveProviderStateOrPlatformInputs проверяет канонические
// selectors, защищённые области и traversal независимо от последующей защиты путей ядром.
func TestManagedWorkspacePathsNeverResolveProviderStateOrPlatformInputs(t *testing.T) {
	input := workspaceToolInputFixture()
	for relative, expected := range map[string]string{
		"work":                "/workspace/work",
		"work/cmd/main.go":    "/workspace/work/cmd/main.go",
		"work/.gitlab-ci.yml": "/workspace/work/.gitlab-ci.yml",
		"outbox":              "/workspace/.kodex/outbox",
		"outbox/review.json":  "/workspace/.kodex/outbox/review.json",
		"work/документы/проверка.md": "/workspace/work/документы/проверка.md",
	} {
		if actual, ok := ManagedWorkspacePath(input, relative); !ok || actual != expected {
			t.Fatalf("canonical workspace selector rejected: %q", relative)
		}
	}
	for _, relative := range []string{"", ".", "/workspace/work/a", "../a", "work/../input/a", "work/../../etc/passwd", "work//a", "work/./a", "work/", "work\\a", "work/a\x00b", "work/a\nb", "input/a", "knowledge/a", ".kodex/state/opencode/auth.json", "outbox/nested/result.json", "worker/a", "outboxx/a", "work/" + strings.Repeat("x", 1024)} {
		if _, ok := ManagedWorkspacePath(input, relative); ok {
			t.Fatalf("protected or ambiguous selector accepted: %q", relative)
		}
	}
	input.Capabilities = nil
	if _, ok := ManagedWorkspacePath(input, "work/main.go"); ok {
		t.Fatal("path syntax granted workspace authority")
	}
}
