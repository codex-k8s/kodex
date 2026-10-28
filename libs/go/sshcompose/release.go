package sshcompose

import (
	"bytes"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"path"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// File retains the Git blob identity alongside exact regular-file source bytes.
// File сохраняет Git blob identity вместе с точными bytes обычного файла.
type File struct {
	Path    string `json:"path"`
	Mode    string `json:"mode"`
	BlobSHA string `json:"blobSha"`
	Content string `json:"contentBase64"`
}

// Bundle is a canonical complete regular-file snapshot of one immutable Git tree.
// Bundle — канонический полный снимок обычных файлов одного immutable Git tree.
type Bundle struct {
	Version    int    `json:"version"`
	Repository string `json:"repository"`
	MergedSHA  string `json:"mergedSha"`
	TreeSHA    string `json:"treeSha"`
	Files      []File `json:"files"`
}

// Manifest defines the supported single-service release rather than arbitrary Compose.
// Manifest описывает поддерживаемый односервисный релиз вместо произвольного Compose.
type Manifest struct {
	Image         string   `json:"image"`
	Command       []string `json:"command"`
	ContainerPort int      `json:"containerPort"`
	HealthPath    string   `json:"healthPath"`
}

// Validate confines images, argument vectors and health paths to bounded values.
// Validate ограничивает образы, векторы аргументов и health paths.
func (manifest Manifest) Validate() error {
	if !imagePattern.MatchString(manifest.Image) || len(manifest.Command) < 1 || len(manifest.Command) > 16 ||
		manifest.ContainerPort < 1024 || manifest.ContainerPort > 65535 || len(manifest.HealthPath) > 120 ||
		!strings.HasPrefix(manifest.HealthPath, "/") || strings.ContainsAny(manifest.HealthPath, "?#\r\n\\\x00") || strings.Contains(manifest.HealthPath, "..") {
		return ErrorInvalid
	}
	for _, argument := range manifest.Command {
		if len(argument) == 0 || len(argument) > 256 || strings.ContainsAny(argument, "$\r\n\x00") {
			return ErrorInvalid
		}
	}
	return nil
}

// Canonical validates ordered paths and blob commitments before exposing a digest.
// Canonical проверяет порядок paths и blob commitments до выдачи digest.
func (bundle Bundle) Canonical() ([]byte, Manifest, error) {
	if bundle.Version != ProtocolVersion || !repositoryPattern.MatchString(bundle.Repository) || !commitPattern.MatchString(bundle.MergedSHA) ||
		!commitPattern.MatchString(bundle.TreeSHA) || len(bundle.Files) == 0 || len(bundle.Files) > MaximumFiles {
		return nil, Manifest{}, ErrorInvalid
	}
	previous, total := "", 0
	var manifest Manifest
	found := false
	for _, file := range bundle.Files {
		if !ValidPath(file.Path) || file.Path <= previous || !commitPattern.MatchString(file.BlobSHA) || (file.Mode != "100644" && file.Mode != "100755") {
			return nil, Manifest{}, ErrorInvalid
		}
		previous = file.Path
		raw, err := base64.StdEncoding.Strict().DecodeString(file.Content)
		if err != nil || len(raw) > MaximumFileBytes || GitObjectID("blob", raw) != file.BlobSHA {
			return nil, Manifest{}, ErrorInvalid
		}
		total += len(raw)
		if total > MaximumBundleBytes {
			return nil, Manifest{}, ErrorInvalid
		}
		if file.Path == "kodex-deploy.json" {
			if Decode(raw, &manifest, 4096) != nil || manifest.Validate() != nil {
				return nil, Manifest{}, ErrorInvalid
			}
			found = true
		}
	}
	if !found || treeID(bundle.Files, "") != bundle.TreeSHA {
		return nil, Manifest{}, ErrorInvalid
	}
	raw, err := json.Marshal(bundle)
	if err != nil || len(raw) > MaximumBundleBytes {
		return nil, Manifest{}, ErrorInvalid
	}
	return raw, manifest, nil
}

// GitObjectID computes Git's format-specific SHA-1 object identity, not a security signature.
// GitObjectID вычисляет SHA-1 identity формата Git, не криптографическую подпись.
func GitObjectID(kind string, raw []byte) string {
	hash := sha1.New()
	_, _ = hash.Write([]byte(kind + " " + strconv.Itoa(len(raw)) + "\x00"))
	_, _ = hash.Write(raw)
	return hex.EncodeToString(hash.Sum(nil))
}

// treeEntry holds one canonical Git tree child while reconstructing the source root.
// treeEntry хранит одного канонического потомка при восстановлении корня Git tree.
type treeEntry struct {
	name, mode, id string
	directory      bool
}

// treeID reconstructs complete trees so a caller cannot omit or substitute source paths.
// treeID восстанавливает полное дерево, запрещая пропуск либо подмену source paths.
func treeID(files []File, prefix string) string {
	entries := map[string]treeEntry{}
	for _, file := range files {
		if !strings.HasPrefix(file.Path, prefix) {
			continue
		}
		remainder := strings.TrimPrefix(file.Path, prefix)
		name, _, directory := strings.Cut(remainder, "/")
		if directory {
			if existing, ok := entries[name]; ok && !existing.directory {
				return ""
			}
			entries[name] = treeEntry{name, "40000", "", true}
		} else {
			if _, exists := entries[name]; exists {
				return ""
			}
			entries[name] = treeEntry{name, file.Mode, file.BlobSHA, false}
		}
	}
	ordered := make([]treeEntry, 0, len(entries))
	for _, entry := range entries {
		ordered = append(ordered, entry)
	}
	sort.Slice(ordered, func(i, j int) bool {
		left, right := ordered[i].name, ordered[j].name
		if ordered[i].directory {
			left += "/"
		}
		if ordered[j].directory {
			right += "/"
		}
		return left < right
	})
	var raw bytes.Buffer
	for _, entry := range ordered {
		if entry.directory {
			entry.id = treeID(files, prefix+entry.name+"/")
		}
		id, err := hex.DecodeString(entry.id)
		if err != nil || len(id) != sha1.Size {
			return ""
		}
		raw.WriteString(entry.mode + " " + entry.name + "\x00")
		raw.Write(id)
	}
	return GitObjectID("tree", raw.Bytes())
}

// TreeID derives the exact complete tree identity for a newly assembled bundle.
// TreeID получает точную identity полного дерева для нового bundle.
func TreeID(files []File) string { return treeID(SortedFiles(files), "") }

// ValidPath rejects traversal, hidden helper-owned names and ambiguous filesystem paths.
// ValidPath отклоняет traversal, имена helper и неоднозначные filesystem paths.
func ValidPath(value string) bool {
	if value == "" || len(value) > 240 || !utf8.ValidString(value) || path.Clean(value) != value || strings.HasPrefix(value, "/") ||
		strings.ContainsAny(value, "\\\x00\r\n") || strings.HasPrefix(value, ".kodex") {
		return false
	}
	for _, part := range strings.Split(value, "/") {
		if part == "" || part == "." || part == ".." || part == ".git" || strings.HasPrefix(part, ".kodex") {
			return false
		}
	}
	return true
}

// SortedFiles returns a new deterministic file order without mutating caller buffers.
// SortedFiles возвращает новый детерминированный порядок, не меняя caller buffers.
func SortedFiles(values []File) []File {
	result := append([]File(nil), values...)
	sort.Slice(result, func(i, j int) bool { return result[i].Path < result[j].Path })
	return result
}

// Compose renders a fixed unprivileged service with release-only read mounts and a state volume.
// Compose создаёт фиксированный непривилегированный сервис с readonly source и state volume.
func Compose(manifest Manifest, target Target, releaseDirectory string) ([]byte, error) {
	if manifest.Validate() != nil || target.Validate() != nil || manifest.Image != target.RuntimeImage || !strings.HasPrefix(releaseDirectory, "/") || strings.ContainsAny(releaseDirectory, "$:\r\n\x00") {
		return nil, ErrorInvalid
	}
	service := map[string]any{
		"image": manifest.Image, "command": manifest.Command, "working_dir": "/app", "user": "0:0",
		"read_only": true, "cap_drop": []string{"ALL"}, "security_opt": []string{"no-new-privileges:true"},
		"cpus": "0.75", "mem_limit": "384m", "pids_limit": 64, "restart": "no",
		"logging":     map[string]any{"driver": "local", "options": map[string]string{"max-size": "1m", "max-file": "2"}},
		"ports":       []string{"127.0.0.1:" + integer(target.PublishedPort) + ":" + integer(manifest.ContainerPort)},
		"volumes":     []any{map[string]any{"type": "bind", "source": releaseDirectory, "target": "/app", "read_only": true}, map[string]any{"type": "volume", "source": "state", "target": "/state"}},
		"tmpfs":       []string{"/tmp:rw,noexec,nosuid,size=32m,uid=65532,gid=65532"},
		"environment": map[string]string{"PORT": integer(manifest.ContainerPort), "STATE_DIR": "/state", "PYTHONDONTWRITEBYTECODE": "1"},
	}
	return json.Marshal(map[string]any{"services": map[string]any{"app": service}, "volumes": map[string]any{"state": map[string]any{}}})
}

// integer formats a validated port without accepting caller-provided command syntax.
// integer форматирует проверенный порт без caller-provided command syntax.
func integer(value int) string {
	if value == 0 {
		return "0"
	}
	var raw [10]byte
	index := len(raw)
	for value > 0 {
		index--
		raw[index] = byte('0' + value%10)
		value /= 10
	}
	return string(raw[index:])
}
