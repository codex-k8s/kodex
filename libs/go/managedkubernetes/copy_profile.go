package managedkubernetes

import (
	"path"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth"
)

// CopyProfile describes an immutable transfer, never artifact ownership or permission to execute.
// Upload FILE contains raw bytes; TAR contains a validated regular-file/directory archive.
// Download always uses native tar; the owner validates and publishes the requested format.
// CopyProfile описывает immutable transfer, не владение artifact либо право исполнения.
// Upload FILE содержит raw bytes; TAR — проверенный архив regular files/directories.
// Download использует native tar; owner проверяет и публикует выбранный формат.
type CopyProfile struct {
	Container      string `json:"container,omitempty"`
	ArtifactRef    string `json:"artifactRef,omitempty"`
	ArtifactSHA256 string `json:"artifactSHA256,omitempty"`
	TransferFormat string `json:"transferFormat"`
	RemotePath     string `json:"remotePath"`
	MaximumBytes   uint64 `json:"maximumBytes"`
	TimeoutSeconds uint32 `json:"timeoutSeconds"`
}

// DecodeCopyProfile rejects unknown options, non-Pod targets and ambiguous paths before a native command is derived.
// No normalization silently changes approved input; download artifact coordinates are always server-assigned.
// DecodeCopyProfile отклоняет unknown options, не-Pod targets и неоднозначные пути до построения native command.
// Нормализация не меняет approved input молча; download artifact coordinates всегда назначает сервер.
func DecodeCopyProfile(operation string, input OperationInput) (CopyProfile, error) {
	var profile CopyProfile
	if operation != "kubernetes.cp.upload" && operation != "kubernetes.cp.download" || input.Group != "" || input.Version != "v1" || input.Resource != "pods" || input.Subresource != "" ||
		internalrpcauth.DecodeStrictJSON(input.Options, &profile) != nil || profile.Validate(operation) != nil {
		return CopyProfile{}, ErrInvalidOperationInput
	}
	return profile, nil
}

// Validate closes format, path and finite byte/time limits without granting access to source bytes.
// Validate закрывает формат, путь и конечные byte/time limits без выдачи доступа к source bytes.
func (profile CopyProfile) Validate(operation string) error {
	if profile.TransferFormat != "FILE" && profile.TransferFormat != "TAR" || profile.MaximumBytes == 0 || profile.MaximumBytes > 16<<20 || profile.TimeoutSeconds == 0 || profile.TimeoutSeconds > 300 ||
		!validCopyPath(profile.RemotePath) || profile.TransferFormat == "FILE" && (profile.RemotePath == "/" || profile.RemotePath == ".") {
		return ErrInvalidOperationInput
	}
	switch operation {
	case "kubernetes.cp.upload":
		if !ValidLocator(profile.ArtifactRef) || len(profile.ArtifactRef) > 96 || !ValidDigest(profile.ArtifactSHA256) {
			return ErrInvalidOperationInput
		}
	case "kubernetes.cp.download":
		if profile.ArtifactRef != "" || profile.ArtifactSHA256 != "" {
			return ErrInvalidOperationInput
		}
	default:
		return ErrInvalidOperationInput
	}
	return nil
}

// NativeCommand builds a fixed argv with paths as distinct arguments, never a shell string.
// FILE upload tar must contain exactly the destination basename; TAR upload extracts into the exact destination directory.
// NativeCommand строит fixed argv с путями в отдельных аргументах, не shell string.
// FILE upload tar содержит ровно destination basename; TAR upload извлекается в exact destination directory.
func (profile CopyProfile) NativeCommand(operation string) ([]string, error) {
	if profile.Validate(operation) != nil {
		return nil, ErrInvalidOperationInput
	}
	if operation == "kubernetes.cp.upload" {
		directory := profile.RemotePath
		if profile.TransferFormat == "FILE" {
			directory = path.Dir(directory)
		}
		return []string{"tar", "-xmf", "-", "-C", directory}, nil
	}
	if profile.RemotePath == "/" || profile.RemotePath == "." {
		return []string{"tar", "-cf", "-", "-C", profile.RemotePath, "."}, nil
	}
	return []string{"tar", "-cf", "-", "-C", path.Dir(profile.RemotePath), "./" + path.Base(profile.RemotePath)}, nil
}

// validCopyPath accepts explicit clean POSIX paths while refusing traversal, controls and silent rewriting.
// Relative paths retain the remote container working-directory semantics; no host path is accessed.
// validCopyPath допускает explicit clean POSIX paths, отвергая traversal, controls и скрытое изменение.
// Relative paths сохраняют семантику working directory контейнера; host path не используется.
func validCopyPath(value string) bool {
	if value == "" || len(value) > 4096 || !utf8.ValidString(value) || path.Clean(value) != value {
		return false
	}
	for _, part := range strings.Split(value, "/") {
		if part == ".." {
			return false
		}
	}
	for _, char := range value {
		if unicode.IsControl(char) {
			return false
		}
	}
	return true
}

// String hides private paths and source artifact references from generic diagnostics.
// String скрывает private paths и source artifact references в общей диагностике.
func (CopyProfile) String() string { return "[managed Kubernetes copy profile]" }

// GoString preserves redaction during detailed Go formatting.
// GoString сохраняет redaction при подробном Go formatting.
func (CopyProfile) GoString() string { return "[managed Kubernetes copy profile]" }
