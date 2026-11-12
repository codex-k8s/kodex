package runtimecontract

import (
	"path"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	// WorkspaceExecutorMode selects the isolated secret-free command container.
	// WorkspaceExecutorMode выбирает isolated command container без секретов.
	WorkspaceExecutorMode = "runtime-workspace-executor"
	// WorkspaceExecutorSocket is shared only between trusted role and executor.
	// WorkspaceExecutorSocket разделяют только trusted role и executor.
	WorkspaceExecutorSocket = "/run/kodex/workspace/authority/executor.sock"
	// WorkspaceCommandChildMode installs kernel filesystem restrictions before exec.
	// WorkspaceCommandChildMode ставит kernel filesystem restrictions до exec.
	WorkspaceCommandChildMode = "runtime-workspace-command"
	// WorkspacePolicyTool is the owner-owned readiness marker and current policy read. Its explicit
	// primary-catalog binding prevents legacy MCP-only runners from silently accepting this profile.
	// WorkspacePolicyTool — серверный marker готовности и чтение актуальной политики. Явная привязка
	// в основном каталоге не позволяет старому MCP-only runner молча принять этот профиль.
	WorkspacePolicyTool = "get_workspace_policy"
	// WorkspaceToolList exposes bounded discovery inside the execution's own mutable tree.
	// WorkspaceToolList открывает ограниченное перечисление собственной рабочей копии исполнения.
	WorkspaceToolList = "list_workspace_files"
	// WorkspaceToolRead reads local source bytes; admitted platform files keep their separate VFS tools.
	// WorkspaceToolRead читает локальный код; допущенные файлы платформы сохраняют отдельные VFS tools.
	WorkspaceToolRead = "read_workspace_file"
	// WorkspaceToolWrite changes local bytes, without publishing an artifact or a repository commit.
	// WorkspaceToolWrite меняет локальные bytes без публикации артефакта либо commit в репозиторий.
	WorkspaceToolWrite = "write_workspace_file"
	// WorkspaceToolExecute selects an owner-published environment command, never a provider shell.
	// WorkspaceToolExecute выбирает опубликованную команду окружения, не shell процесса провайдера.
	WorkspaceToolExecute = "run_workspace_tool"
	// WorkspaceToolCheckout materializes one immutable authorized repository snapshot.
	// WorkspaceToolCheckout материализует одну неизменяемую разрешённую рабочую копию репозитория.
	WorkspaceToolCheckout = "checkout_repository"
)

// RuntimeWorkspaceToolsAvailable derives optional local-tool eligibility from a complete turn's
// immutable coordinates and artifact capability. It is not current owner admission, a file handle,
// a sandbox or permission to publish an external effect; every consumer must retain those checks.
// RuntimeWorkspaceToolsAvailable выводит допуск опциональных локальных tools из координат
// неизменного хода и artifact capability. Это не свежий допуск владельца, файловый handle,
// sandbox либо разрешение внешней записи; каждый consumer обязан сохранить эти проверки.
func RuntimeWorkspaceToolsAvailable(input RunnerInput) bool {
	return input.EnvironmentPolicy.ManagedWorkspace && input.Mode == RunnerModeTurn && input.Provider == "opencode" &&
		input.ModelProvider != nil && input.ModelProvider.Validate() == nil &&
		input.ModelProvider.Engine == "opencode" &&
		input.AssistantScope == AssistantScopeNone && input.WorkspaceAccess == "workspace-write" && input.ProjectRef != "" &&
		input.LeaseRef != "" && input.LeaseFence != "" && input.LeaseGeneration > 0 &&
		sha256Pattern.MatchString(input.InputDigest) && sha256Pattern.MatchString(input.RuntimeRevisionDigest) &&
		slices.Contains(input.Capabilities, ArtifactCapability) && input.WorkspacePolicy.Validate() == nil
}

// RuntimeWorkspaceCommandAllowed matches a single executable identifier from the published
// environment. Arguments are separate data; this predicate does not resolve PATH, execute code,
// inherit environment, validate a command's effects or replace process confinement.
// RuntimeWorkspaceCommandAllowed сопоставляет один executable identifier опубликованного
// окружения. Аргументы передаются отдельно; функция не разрешает PATH, не исполняет код,
// не наследует environment, не проверяет effects команды и не заменяет изоляцию процесса.
func RuntimeWorkspaceCommandAllowed(input RunnerInput, command string) bool {
	if !RuntimeWorkspaceToolsAvailable(input) || !runtimeToolCommandPattern.MatchString(command) {
		return false
	}
	return slices.ContainsFunc(input.EnvironmentTools, func(tool RuntimeEnvironmentTool) bool {
		return tool.Command == command
	})
}

// RuntimeWorkspaceCheckoutAvailable requires a complete tree/file/commit grant trio on the same
// Connection. Immutable catalog membership only advertises the tool: the gateway must recheck
// personal credentials, current grants, repository scope and exact SHA for every actual read.
// RuntimeWorkspaceCheckoutAvailable требует полный набор tree/file/commit grants одного
// Connection. Неизменный каталог лишь показывает tool: gateway повторно проверяет личные
// credentials, текущие grants, область репозитория и точный SHA при каждом настоящем чтении.
func RuntimeWorkspaceCheckoutAvailable(input RunnerInput) bool {
	if !RuntimeWorkspaceToolsAvailable(input) {
		return false
	}
	connections := map[struct {
		ref, definition string
		version         int64
	}]map[string]bool{}
	for _, grant := range input.IntegrationGrants {
		if grant.ConnectionRef == "" || grant.Ref == "" || grant.GrantVersion < 1 || grant.ConnectionVersion < 1 ||
			grant.DefinitionKey != "gitlab" || !sha256Pattern.MatchString(grant.DefinitionDigest) ||
			grant.Risk != "READ" || grant.Operation != grant.CapabilityKey ||
			!slices.Contains([]string{"gitlab.repository.tree.list", "gitlab.repository.file.read", "gitlab.commit.read"}, grant.Operation) {
			continue
		}
		pin := struct {
			ref, definition string
			version         int64
		}{grant.ConnectionRef, grant.DefinitionDigest, grant.ConnectionVersion}
		if connections[pin] == nil {
			connections[pin] = map[string]bool{}
		}
		connections[pin][grant.Operation] = true
	}
	for _, operations := range connections {
		if len(operations) == 3 {
			return true
		}
	}
	return false
}

// RuntimeWorkspaceToolNames describes the separate job-owned local MCP catalog. It deliberately
// does not extend the controller-owned RuntimeMCPToolNames catalog or its binding digest.
// RuntimeWorkspaceToolNames описывает отдельный локальный MCP-каталог, принадлежащий job.
// Функция намеренно не расширяет каталог RuntimeMCPToolNames контроллера и его binding digest.
func RuntimeWorkspaceToolNames(input RunnerInput) []string {
	if !RuntimeWorkspaceToolsAvailable(input) {
		return nil
	}
	names := []string{WorkspaceToolList, WorkspaceToolRead, WorkspaceToolWrite}
	if len(input.EnvironmentTools) != 0 {
		names = append(names, WorkspaceToolExecute)
	}
	if RuntimeWorkspaceCheckoutAvailable(input) {
		names = append(names, WorkspaceToolCheckout)
	}
	return names
}

// ManagedWorkspacePath resolves canonical tool-relative work/outbox names against the existing
// workspace policy. The result is a lexical selector only: consumers must use descriptor-relative
// confined I/O, quotas and writer fencing, and must never turn it into an unchecked pathname open.
// ManagedWorkspacePath разрешает канонические имена work/outbox по существующей workspace policy.
// Результат — лишь лексический selector: consumers обязаны использовать ограниченный I/O через
// descriptors, квоты и writer fencing, а не открывать полученный путь без дополнительных проверок.
func ManagedWorkspacePath(input RunnerInput, relative string) (string, bool) {
	if !RuntimeWorkspaceToolsAvailable(input) || relative == "" || len(relative) > 1024 ||
		!utf8.ValidString(relative) || path.IsAbs(relative) || path.Clean(relative) != relative ||
		strings.ContainsRune(relative, '\\') || strings.ContainsFunc(relative, unicode.IsControl) {
		return "", false
	}
	var resolved string
	switch {
	case relative == "work" || strings.HasPrefix(relative, "work/"):
		resolved = RuntimeWorkspaceRoot + "/" + relative
	case relative == "outbox" || strings.HasPrefix(relative, "outbox/"):
		if strings.Count(relative, "/") > 1 {
			return "", false
		}
		resolved = RuntimeWorkspaceRoot + "/.kodex/" + relative
	default:
		return "", false
	}
	access, _ := input.WorkspacePolicy.AccessForPath(resolved)
	if access != RuntimeWorkspaceWritable {
		return "", false
	}
	return resolved, true
}
