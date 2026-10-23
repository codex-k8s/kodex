// Package codex executes one bounded provider turn through the isolated broker/protocol and maps its pinned configuration, context, activity and result without owning domain grants.
// Пакет codex выполняет один ограниченный provider turn через изолированный broker/protocol и отображает pinned configuration, context, activity и результат без владения domain grants.
package codex

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"

	"github.com/BurntSushi/toml"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/model"
)

// ErrRuntimeProfile is the shared safe sentinel for invalid pinned runtime selection or profile materialization; it carries no raw config, authentication or provider diagnostic.
// ErrRuntimeProfile — общий безопасный sentinel неверного pinned runtime selection или profile materialization без сырой конфигурации, авторизации и provider diagnostics.
var ErrRuntimeProfile = errors.New("Codex runtime profile is invalid")

// ValidateRuntimeProfile requires the complete runner input contract before checking local model/reasoning/tool selection; it does not replace server-owned admission, capability or grant validation.
// ValidateRuntimeProfile требует полный runner input contract до локальной проверки model/reasoning/tool selection; не заменяет серверные admission, capability и grant checks.
func ValidateRuntimeProfile(input model.Input) error {
	if input.Validate() != nil {
		return ErrRuntimeProfile
	}
	return validateRuntimeSelection(input)
}

// validateRuntimeSelection accepts the pinned openai provider and bounded nonblank model, validates effective reasoning against the overlay and requires each declared tool to resolve to an executable regular file. It does not impose a local model catalog or execute the tools.
// validateRuntimeSelection принимает pinned openai provider и ограниченную непустую модель, сверяет effective reasoning с overlay и требует разрешения каждого tool в исполняемый обычный файл. Не навязывает локальный model catalog и не запускает tools.
func validateRuntimeSelection(input model.Input) error {
	if runtimecontract.ValidateEffectiveReasoningEffort(input.ConfigOverlay, input.EffectiveReasoningEffort, input.ReasoningMode) != nil || input.Provider != "openai" || input.Model == "" || len(input.Model) > 128 || strings.TrimSpace(input.Model) != input.Model {
		return ErrRuntimeProfile
	}
	for _, tool := range input.EnvironmentTools {
		path, err := exec.LookPath(tool.Command)
		if err != nil {
			return ErrRuntimeProfile
		}
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
			return ErrRuntimeProfile
		}
	}
	return nil
}

// runtimeConfig is the typed TOML projection generated from approved runtime input, combining exact model, permission, environment, history and required-MCP settings; it is not an unrestricted caller-supplied CLI config.
// runtimeConfig — типизированная TOML-проекция одобренного runtime input с точными model, permission, environment, history и required-MCP settings, не произвольная CLI-конфигурация клиента.
type runtimeConfig struct {
	WebSearchMode          string                       `toml:"web_search,omitempty"`
	Model                  string                       `toml:"model"`
	ModelReasoningEffort   string                       `toml:"model_reasoning_effort,omitempty"`
	Personality            string                       `toml:"personality,omitempty"`
	AllowLoginShell        *bool                        `toml:"allow_login_shell,omitempty"`
	ApprovalPolicy         string                       `toml:"approval_policy"`
	DefaultPermissions     string                       `toml:"default_permissions"`
	CLIAuthCredentialStore string                       `toml:"cli_auth_credentials_store"`
	History                historyConfig                `toml:"history"`
	ShellEnvironmentPolicy shellEnvironmentPolicy       `toml:"shell_environment_policy"`
	Features               runtimeFeatures              `toml:"features"`
	Memories               runtimeMemories              `toml:"memories"`
	MCPServers             map[string]mcpServerConfig   `toml:"mcp_servers"`
	Permissions            map[string]permissionProfile `toml:"permissions"`
}

// runtimeFeatures serializes the native tool-routing and local-memory feature switches explicitly, including false values, so the generated profile does not silently inherit user feature defaults.
// runtimeFeatures явно сериализует native tool-routing и local-memory switches, включая false, чтобы создаваемый профиль не наследовал user feature defaults незаметно.
type runtimeFeatures struct {
	CodeModeHost bool                  `toml:"code_mode_host"`
	CodeModeOnly bool                  `toml:"code_mode_only"`
	CodeMode     runtimeCodeModeConfig `toml:"code_mode"`
	Memories     bool                  `toml:"memories"`
}

// runtimeMemories carries explicit local generation/use switches, left disabled by this producer because context comes from Kodex-owned records rather than a second provider memory source or writes to the read-only projection.
// runtimeMemories хранит явные local generation/use switches, отключённые этим producer: контекст поступает из записей Kodex, а не второго provider memory source или записи в read-only projection.
type runtimeMemories struct {
	GenerateMemories bool `toml:"generate_memories"`
	UseMemories      bool `toml:"use_memories"`
}

// runtimeCodeModeConfig records the existing disabled code-mode host settings and direct-only namespaces for the pinned provider profile; routing flags do not relax filesystem, approval or callback authority checks.
// runtimeCodeModeConfig фиксирует прежние disabled code-mode host settings и direct-only namespaces pinned provider profile; routing flags не ослабляют filesystem, approval и callback authority checks.
type runtimeCodeModeConfig struct {
	Enabled                  bool     `toml:"enabled"`
	DirectOnlyToolNamespaces []string `toml:"direct_only_tool_namespaces"`
}

// permissionProfile describes the selected CLI permission base plus exact filesystem rules and network projection, without granting resources absent from the materialized runtime policy.
// permissionProfile описывает выбранную CLI permission base, точные filesystem rules и network projection без выдачи ресурсов вне материализованной runtime policy.
type permissionProfile struct {
	Extends    string            `toml:"extends"`
	Filesystem map[string]string `toml:"filesystem"`
	Network    *networkPolicy    `toml:"network,omitempty"`
}

// networkPolicy is the CLI-facing network projection, including upstream-proxy support and domain hints; protocol, port and method enforcement still belongs to the managed network boundary, not this struct alone.
// networkPolicy — CLI-проекция сети с upstream-proxy support и domain hints; контроль protocol, port и method остаётся у managed network boundary, не у одной этой структуры.
type networkPolicy struct {
	Enabled            bool     `toml:"enabled"`
	Mode               string   `toml:"mode,omitempty"`
	AllowedDomains     []string `toml:"allowed_domains,omitempty"`
	AllowUpstreamProxy bool     `toml:"allow_upstream_proxy"`
}

// historyConfig carries only the approved history persistence selection; an empty overlay value is materialized as save-all by the home writer.
// historyConfig хранит лишь одобренный history persistence selection; пустое overlay-значение writer home материализует как save-all.
type historyConfig struct {
	Persistence string `toml:"persistence"`
}

// shellEnvironmentPolicy projects approved environment names into filters and nonsecret values into set, keeping secret projection values out of persisted TOML while preserving the existing CLI filter policy.
// shellEnvironmentPolicy проецирует одобренные environment names в filters и несекретные values в set, не записывая secret projection values в TOML и сохраняя прежнюю CLI filter policy.
type shellEnvironmentPolicy struct {
	Inherit               string            `toml:"inherit"`
	IgnoreDefaultExcludes bool              `toml:"ignore_default_excludes"`
	Filters               map[string]string `toml:"filters"`
	Set                   map[string]string `toml:"set"`
}

// mcpServerConfig declares the required local Kodex MCP endpoint, bearer environment-variable name, startup/tool budgets and tool approval mode; the bearer value and server-owned execution permission are not stored here.
// mcpServerConfig задаёт обязательный local Kodex MCP endpoint, имя bearer environment variable, startup/tool budgets и tool approval mode; bearer value и серверное execution permission здесь не хранятся.
type mcpServerConfig struct {
	URL                      string `toml:"url"`
	Required                 bool   `toml:"required"`
	BearerTokenEnvVar        string `toml:"bearer_token_env_var"`
	StartupTimeoutSeconds    int    `toml:"startup_timeout_sec"`
	ToolTimeoutSeconds       int    `toml:"tool_timeout_sec"`
	DefaultToolsApprovalMode string `toml:"default_tools_approval_mode"`
}

// PrepareHomeWithAuth validates reasoning, volume pins, home containment and the authentication digest before publishing auth.json, then materializes and round-trip checks the typed TOML profile. It persists only approved nonsecret environment values, not projected secret values. Later failures can leave the already published auth snapshot: this is not an atomic whole-home transaction, and callers supply the trusted input/MCP endpoint.
// PrepareHomeWithAuth проверяет reasoning, volume pins, home containment и auth digest до публикации auth.json, затем материализует и сверяет typed TOML profile обратным разбором. Сохраняет лишь approved nonsecret environment values, не secret projection values. Поздний отказ может оставить уже опубликованный auth snapshot: это не атомарная whole-home transaction, trusted input/MCP endpoint задаёт вызывающий.
func PrepareHomeWithAuth(input model.Input, mcpURL string, auth []byte) error {
	if runtimecontract.ValidateEffectiveReasoningEffort(input.ConfigOverlay, input.EffectiveReasoningEffort, input.ReasoningMode) != nil {
		return ErrRuntimeProfile
	}
	volumePermissions, err := runtimeVolumePermissions(input)
	if err != nil {
		return err
	}
	if filepath.Clean(input.CodexHome) != input.CodexHome ||
		!strings.HasPrefix(input.CodexHome, input.WorkspaceRoot+string(os.PathSeparator)) {
		return errors.New("CODEX_HOME path is invalid")
	}
	if err := secureDirectory(input.CodexHome); err != nil {
		return err
	}
	if len(auth) == 0 || len(auth) > 1<<20 || !bytes.HasPrefix(bytes.TrimSpace(auth), []byte("{")) {
		return errors.New("Codex authentication snapshot is invalid; use codex login --device-auth outside the runtime")
	}
	authDigest := sha256.Sum256(auth)
	expectedDigest, err := pinnedProviderDigest(input)
	if err != nil || hex.EncodeToString(authDigest[:]) != expectedDigest {
		return errors.New("Codex authentication snapshot does not match the pinned provider account")
	}
	if err := replacePrivateFile(filepath.Join(input.CodexHome, "auth.json"), auth); err != nil {
		return err
	}
	// permissionProfileName fixes the single generated profile selected for this runtime input.
	// permissionProfileName закрепляет единственный создаваемый профиль данного runtime input.
	const permissionProfileName = "kodex-runtime"
	filesystem := map[string]string{
		filepath.Join(input.CodexHome, "auth.json"): "deny",
		"/run/secrets": "deny",
		"/proc":        "deny",
		filepath.Join(input.WorkspaceRoot, "AGENTS.md"):     "read",
		filepath.Join(input.WorkspaceRoot, "input"):         "read",
		filepath.Join(input.WorkspaceRoot, "knowledge"):     "read",
		runtimecontract.RuntimeContextRoot:                  "read",
		filepath.Join(input.WorkspaceRoot, ".kodex/inbox"):  "read",
		filepath.Join(input.WorkspaceRoot, ".kodex/outbox"): "read",
	}
	for path, access := range volumePermissions {
		filesystem[path] = access
	}
	if input.CodexSandbox == "workspace-write" && !input.IsAssistant() && slices.Contains(input.Capabilities, runtimecontract.ArtifactCapability) {
		filesystem[filepath.Join(input.WorkspaceRoot, ".kodex/outbox")] = "write"
	}
	permissionBase, err := codexPermissionBase(input.CodexSandbox)
	if err != nil {
		return err
	}
	overlay, err := runtimecontract.ParseConfigOverlay(input.ConfigOverlay)
	if err != nil {
		return errors.New("validate published Codex configuration overlay")
	}
	allowLoginShell := false
	if overlay.AllowLoginShell != nil {
		allowLoginShell = *overlay.AllowLoginShell
	}
	historyPersistence := overlay.History.Persistence
	if historyPersistence == "" {
		historyPersistence = "save-all"
	}
	environmentSet := map[string]string{"PATH": "/usr/local/bin:/usr/bin:/bin", "HOME": "/tmp"}
	environmentNames := map[string]struct{}{"PATH": {}, "HOME": {}}
	if _, err := runtimeTransportEnvironment(); err != nil {
		return err
	}
	for _, name := range runtimeTransportEnvironmentNames {
		environmentNames[name] = struct{}{}
	}
	for _, item := range input.EnvironmentValues {
		if !runtimecontract.ValidRuntimeEnvironmentName(item.Name) {
			return errors.New("runtime environment variable is reserved")
		}
		environmentSet[item.Name] = item.Value
		environmentNames[item.Name] = struct{}{}
	}
	for _, item := range input.SecretProjections {
		if !runtimecontract.ValidRuntimeEnvironmentName(item.Name) {
			return errors.New("runtime environment variable is reserved")
		}
		environmentNames[item.Name] = struct{}{}
	}
	filters := make(map[string]string, len(environmentNames))
	for name := range environmentNames {
		filters[name] = "include"
	}
	config := runtimeConfig{Model: input.Model, ModelReasoningEffort: input.EffectiveReasoningEffort,
		WebSearchMode: overlay.WebSearchMode,
		Personality:   overlay.Personality, AllowLoginShell: &allowLoginShell, ApprovalPolicy: input.CodexApprovalPolicy,
		DefaultPermissions: permissionProfileName, CLIAuthCredentialStore: "file",
		History: historyConfig{Persistence: historyPersistence},
		// В Codex rust-v0.160.0 model.tool_mode имеет приоритет над feature flags.
		// DirectModelOnly сохраняет native tools при CodeModeOnly без запуска host;
		// меняется только маршрутизация, прежние permission/authority checks остаются.
		// Источник: codex-rs/core/src/tools/spec_plan.rs, tag rust-v0.160.0.
		Features: runtimeFeatures{CodeModeHost: false, CodeModeOnly: false, CodeMode: runtimeCodeModeConfig{
			Enabled: false, DirectOnlyToolNamespaces: []string{"functions", "web", "mcp__kodex"},
		}},
		Permissions: map[string]permissionProfile{permissionProfileName: {Extends: permissionBase,
			Filesystem: filesystem, Network: codexNetworkPolicy(input.EnvironmentPolicy.Network.WebAccess)}},
		ShellEnvironmentPolicy: shellEnvironmentPolicy{Inherit: "all", IgnoreDefaultExcludes: true,
			Filters: filters, Set: environmentSet},
		MCPServers: map[string]mcpServerConfig{"kodex": {URL: mcpURL,
			BearerTokenEnvVar: "KODEX_MCP_PROXY_TOKEN", DefaultToolsApprovalMode: "approve",
			Required: true, StartupTimeoutSeconds: 15,
			ToolTimeoutSeconds: runtimecontract.MaximumSynchronousMCPToolTimeoutSeconds}}}
	var raw bytes.Buffer
	if err := toml.NewEncoder(&raw).Encode(config); err != nil {
		return errors.New("encode Codex configuration")
	}
	var decoded runtimeConfig
	metadata, err := toml.Decode(raw.String(), &decoded)
	if err != nil || len(metadata.Undecoded()) != 0 || decoded.Model != input.Model || decoded.WebSearchMode != overlay.WebSearchMode ||
		!decoded.MCPServers["kodex"].Required ||
		decoded.MCPServers["kodex"].BearerTokenEnvVar != "KODEX_MCP_PROXY_TOKEN" ||
		decoded.MCPServers["kodex"].DefaultToolsApprovalMode != "approve" ||
		!metadata.IsDefined("features", "code_mode_host") || decoded.Features.CodeModeHost ||
		!metadata.IsDefined("features", "code_mode_only") || decoded.Features.CodeModeOnly ||
		!metadata.IsDefined("features", "code_mode", "enabled") || decoded.Features.CodeMode.Enabled ||
		len(decoded.Features.CodeMode.DirectOnlyToolNamespaces) != 3 ||
		decoded.Features.CodeMode.DirectOnlyToolNamespaces[0] != "functions" ||
		decoded.Features.CodeMode.DirectOnlyToolNamespaces[1] != "web" ||
		decoded.Features.CodeMode.DirectOnlyToolNamespaces[2] != "mcp__kodex" ||
		decoded.DefaultPermissions != permissionProfileName || decoded.Permissions[permissionProfileName].Extends != permissionBase ||
		decoded.ShellEnvironmentPolicy.Inherit != "all" || !sameEnvironmentFilters(decoded.ShellEnvironmentPolicy.Filters, filters) ||
		decoded.Permissions[permissionProfileName].Filesystem[filepath.Join(input.CodexHome, "auth.json")] != "deny" {
		return errors.New("validate Codex configuration")
	}
	return replacePrivateFile(filepath.Join(input.CodexHome, "config.toml"), raw.Bytes())
}

// runtimeVolumePermissions maps only normalized, digest-pinned declared volume mount paths to read or workspace-write access; it grants no parent/state/input paths and accepts an empty volume list without adding permissions.
// runtimeVolumePermissions назначает read или workspace-write лишь нормализованным digest-pinned объявленным mount paths томов; не разрешает parent/state/input paths и принимает пустой список без добавления прав.
func runtimeVolumePermissions(input model.Input) (map[string]string, error) {
	permissions := make(map[string]string, len(input.EnvironmentPolicy.Volumes))
	if len(input.EnvironmentPolicy.Volumes) == 0 {
		return permissions, nil
	}
	policy, err := runtimecontract.NormalizeRuntimeEnvironmentPolicy(input.EnvironmentPolicy)
	if err != nil || input.EnvironmentPolicy.VolumesDigest != policy.VolumesDigest {
		return nil, ErrRuntimeProfile
	}
	access := "read"
	if input.CodexSandbox == "workspace-write" {
		access = "write"
	}
	for _, volume := range policy.Volumes {
		permissions[volume.MountPath] = access
	}
	return permissions, nil
}

// sameEnvironmentFilters compares map size and expected lookup values for the generated nonempty include filters, including nil/empty equality; it is not a general key-presence comparator for arbitrary empty-string map values.
// sameEnvironmentFilters сравнивает размер maps и ожидаемые lookup values создаваемых непустых include filters, включая равенство nil/empty; не является общим key-presence comparator произвольных пустых строк.
func sameEnvironmentFilters(actual, expected map[string]string) bool {
	if len(actual) != len(expected) {
		return false
	}
	for name, value := range expected {
		if actual[name] != value {
			return false
		}
	}
	return true
}

// codexNetworkPolicy projects the existing web-access mode and allowlist domain patterns into CLI network settings with upstream proxy enabled. It assumes validated input and neither resolves DNS nor itself enforces port/protocol/HTTP-method rules.
// codexNetworkPolicy проецирует прежний web-access mode и allowlist domain patterns в CLI network settings с upstream proxy. Предполагает проверенный вход, не разрешает DNS и сам не применяет port/protocol/HTTP-method rules.
func codexNetworkPolicy(access runtimecontract.RuntimeWebAccess) *networkPolicy {
	result := &networkPolicy{Enabled: access.Mode != runtimecontract.RuntimeWebAccessNone, AllowUpstreamProxy: true}
	switch access.Mode {
	case runtimecontract.RuntimeWebAccessAllowlistReadOnly:
		result.Mode = "limited"
	case runtimecontract.RuntimeWebAccessAllowlistFull, runtimecontract.RuntimeWebAccessFullPublic:
		result.Mode = "full"
	}
	if access.Mode == runtimecontract.RuntimeWebAccessAllowlistReadOnly || access.Mode == runtimecontract.RuntimeWebAccessAllowlistFull {
		for _, rule := range access.Rules {
			result.AllowedDomains = append(result.AllowedDomains, rule.DomainPattern)
		}
	}
	return result
}

// readProviderDigest reads a small textual lowercase SHA-256 pin, allowing the existing raw-length range and trimmed whitespace; length is checked after ReadFile, not as a pre-allocation or no-follow file bound.
// readProviderDigest читает текстовый lowercase SHA-256 pin с прежним raw-length range и обрезкой пробелов; размер проверяется после ReadFile, не как pre-allocation или no-follow ограничение.
func readProviderDigest(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil || len(raw) < 64 || len(raw) > 66 {
		return "", errors.New("read provider authentication digest")
	}
	value := strings.TrimSpace(string(raw))
	if len(value) != 64 || strings.Trim(value, "0123456789abcdef") != "" {
		return "", errors.New("provider authentication digest is invalid")
	}
	return value, nil
}

// pinnedProviderDigest requires the mounted authentication digest to equal the immutable input's provider credential digest and returns a fixed mismatch error rather than using the file as a new source of credential authority.
// pinnedProviderDigest требует равенства mounted auth digest и provider credential digest immutable input, возвращая фиксированный mismatch вместо принятия файла за новый источник credential authority.
func pinnedProviderDigest(input model.Input) (string, error) {
	value, err := readProviderDigest(input.ProviderAuthSHA256File)
	if err != nil || value != input.ProviderCredentialSHA256 {
		return "", errors.New("provider authentication revision does not match RuntimeRevision")
	}
	return value, nil
}

// codexPermissionBase recognizes only read-only and workspace-write, mapping them to existing named CLI bases and rejecting dangerous full-access or unknown policies.
// codexPermissionBase распознаёт лишь read-only и workspace-write, переводя в прежние именованные CLI bases и отклоняя dangerous full-access или неизвестные policies.
func codexPermissionBase(sandbox string) (string, error) {
	switch sandbox {
	case "read-only":
		return ":read-only", nil
	case "workspace-write":
		return ":workspace", nil
	default:
		return "", errors.New("Codex sandbox policy is invalid")
	}
}

// secureDirectory creates or inspects the final state directory without accepting a final symlink. Provider UID 10002 must observe the exact existing runner-owned shared boundary; other callers require current-user ownership before the existing numeric chmod request. Parent-path trust remains the caller's responsibility.
// secureDirectory создаёт или проверяет конечный state directory без принятия final symlink. Provider UID 10002 обязан видеть точную существующую runner-owned shared boundary; прочим вызывающим нужно current-user ownership перед прежним numeric chmod request. Доверие parent path остаётся обязанностью вызывающего.
func secureDirectory(path string) error {
	if err := os.MkdirAll(path, 0o770); err != nil {
		return errors.New("create Codex state directory")
	}
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("Codex state directory is unsafe")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return errors.New("Codex state directory metadata is unavailable")
	}
	if os.Geteuid() == 10002 {
		if stat.Uid != 10001 || stat.Gid != 29000 || info.Mode().Perm() != 0o770 || info.Mode()&os.ModeSetgid == 0 {
			return errors.New("Codex state directory is outside the trusted shared boundary")
		}
		return nil
	}
	if stat.Uid != uint32(os.Geteuid()) || os.Chmod(path, 0o2770) != nil {
		return errors.New("protect Codex state directory")
	}
	return nil
}

// replacePrivateFile removes stale staging, exclusively creates a 0600 snapshot, writes/syncs/closes and renames it. Failure staging is not universally removed, parent directories are assumed trusted and directory fsync is absent; rename errors preserve their OS cause while other stages use fixed diagnostics.
// replacePrivateFile удаляет прежний staging, исключительно создаёт snapshot 0600, пишет/синхронизирует/закрывает и переименовывает. Staging не очищается при всех отказах, parent directories предполагаются доверенными и fsync каталога отсутствует; rename errors сохраняют OS cause, прочие этапы дают фиксированную диагностику.
func replacePrivateFile(path string, payload []byte) error {
	temporary := path + ".next"
	if err := os.Remove(temporary); err != nil && !os.IsNotExist(err) {
		return errors.New("remove stale Codex snapshot")
	}
	file, err := os.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return errors.New("create Codex snapshot")
	}
	if _, err := file.Write(payload); err != nil {
		file.Close()
		return errors.New("write Codex snapshot")
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return errors.New("sync Codex snapshot")
	}
	if err := file.Close(); err != nil {
		return errors.New("close Codex snapshot")
	}
	if err := os.Rename(temporary, path); err != nil {
		return fmt.Errorf("commit Codex snapshot: %w", err)
	}
	return nil
}
