// Package codex executes one bounded provider turn through the isolated broker/protocol and maps its pinned configuration, context, activity and result without owning domain grants.
// Пакет codex выполняет один ограниченный provider turn через изолированный broker/protocol и отображает pinned configuration, context, activity и результат без владения domain grants.
package codex

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"syscall"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/model"
)

// ErrRuntimeProfile is the shared safe sentinel for invalid pinned runtime selection or profile materialization; it carries no raw config, authentication or provider diagnostic.
// ErrRuntimeProfile — общий безопасный sentinel неверного pinned runtime selection или profile materialization без сырой конфигурации, авторизации и provider diagnostics.
var ErrRuntimeProfile = errors.New("OpenCode runtime profile is invalid")

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
	if runtimecontract.ValidateEffectiveReasoningEffort(input.ConfigOverlay, input.EffectiveReasoningEffort, input.ReasoningMode) != nil || input.Model == "" || len(input.Model) > 128 || strings.TrimSpace(input.Model) != input.Model {
		return ErrRuntimeProfile
	}
	if input.Provider == "opencode" {
		if input.ModelProvider == nil || input.ModelProvider.Validate() != nil || input.ConfigOverlay != "" ||
			(len(input.EnvironmentTools) != 0 && !input.EnvironmentPolicy.ManagedWorkspace) || input.EffectiveReasoningEffort != "" ||
			input.ReasoningMode != runtimecontract.ReasoningUnsupported {
			return ErrRuntimeProfile
		}
		if _, found := input.ModelProvider.Model(input.Model); !found {
			return ErrRuntimeProfile
		}
		return nil
	}
	return ErrRuntimeProfile
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
