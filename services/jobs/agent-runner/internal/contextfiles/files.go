// Package contextfiles материализует только immutable Skills/Memory snapshot.
package contextfiles

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"go.yaml.in/yaml/v2"
	"golang.org/x/sys/unix"
)

var ErrContextFiles = errors.New("runtime context files are invalid")

// FileSource получает уже проверенные pins, не URL либо произвольный путь.
type FileSource interface {
	WriteSkillFile(context.Context, runtimecontract.RunnerInput, runtimecontract.RuntimeSkillBundle, runtimecontract.RuntimeSkillFile, io.Writer) error
}

type manifest struct {
	RuntimeRevisionRef    string                                 `json:"runtime_revision_ref"`
	RuntimeRevisionDigest string                                 `json:"runtime_revision_digest"`
	SessionRef            string                                 `json:"session_ref"`
	TurnRef               string                                 `json:"turn_ref"`
	Attempt               int32                                  `json:"attempt"`
	Snapshot              runtimecontract.RuntimeContextSnapshot `json:"snapshot"`
}

func Materialize(ctx context.Context, input runtimecontract.RunnerInput, snapshot runtimecontract.RuntimeContextSnapshot, source FileSource) error {
	return materializeAt(ctx, runtimecontract.RuntimeContextRoot, input, snapshot, source, time.Now())
}

// Verify требует реального read-only mount, а не только chmod владельца файла.
func Verify(input runtimecontract.RunnerInput, snapshot runtimecontract.RuntimeContextSnapshot) error {
	return verifyAt(runtimecontract.RuntimeContextRoot, input, snapshot, time.Now(), true)
}

func materializeAt(ctx context.Context, directory string, input runtimecontract.RunnerInput, snapshot runtimecontract.RuntimeContextSnapshot, source FileSource, now time.Time) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if snapshot.ValidateFor(input, now) != nil || (len(snapshot.Skills) > 0 && source == nil) {
		return ErrContextFiles
	}
	root, err := openRoot(directory, false)
	if err != nil {
		return err
	}
	defer root.Close()
	// Единственный writer работает до startup barrier. Manifest фиксируется последним;
	// частичное дерево после сбоя никогда не считается готовым.
	if err := clearTree(root); err != nil {
		return err
	}
	complete := false
	defer func() {
		if !complete {
			_ = clearTree(root)
		}
	}()
	for _, dir := range []string{"skills", "memory"} {
		if makeDirectories(root, dir) != nil {
			return ErrContextFiles
		}
	}
	for _, skill := range snapshot.Skills {
		for _, pin := range skill.Files {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			relative := path.Join("skills", skill.BundleRef, pin.Path)
			if makeDirectories(root, path.Dir(relative)) != nil {
				return ErrContextFiles
			}
			file, err := root.OpenFile(relative, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o440)
			if err != nil {
				return ErrContextFiles
			}
			hash := sha256.New()
			bounded := &boundedWriter{writer: io.MultiWriter(file, hash), remaining: pin.SizeBytes}
			writeErr := source.WriteSkillFile(ctx, input, skill, pin, bounded)
			modeErr := exactMode(file, 0o440)
			syncErr, closeErr := file.Sync(), file.Close()
			if writeErr != nil || modeErr != nil || syncErr != nil || closeErr != nil || bounded.failed || bounded.remaining != 0 ||
				"sha256:"+hex.EncodeToString(hash.Sum(nil)) != pin.Digest {
				return ErrContextFiles
			}
		}
	}
	for _, memory := range snapshot.Memories {
		if err := writeFile(root, path.Join("memory", memory.RecordRef+".md"), MemoryRepresentation(memory)); err != nil {
			return err
		}
	}
	if err := validateSkillManifests(root, snapshot); err != nil {
		return err
	}
	raw, err := manifestBytes(input, snapshot)
	if err != nil {
		return err
	}
	if err := writeFile(root, "manifest.json", raw); err != nil {
		return err
	}
	if err := verifyTree(root, input, snapshot); err != nil {
		return err
	}
	complete = true
	return nil
}

// Режим создания сужается umask. Точные права назначаются открытому inode;
// каждый вложенный каталог проверяется отдельно без следования symlink.
func makeDirectories(root *os.Root, relative string) error {
	current := ""
	for _, component := range strings.Split(relative, "/") {
		current = path.Join(current, component)
		if err := root.Mkdir(current, 0o750); err != nil && !errors.Is(err, os.ErrExist) {
			return ErrContextFiles
		}
		directory, err := root.OpenFile(current, os.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW, 0)
		if err != nil {
			return ErrContextFiles
		}
		modeErr := exactMode(directory, 0o750)
		closeErr := directory.Close()
		if modeErr != nil || closeErr != nil {
			return ErrContextFiles
		}
	}
	return nil
}

func exactMode(file *os.File, mode os.FileMode) error {
	if file.Chmod(mode) != nil {
		return ErrContextFiles
	}
	info, err := file.Stat()
	if err != nil || info.Mode().Perm() != mode {
		return ErrContextFiles
	}
	return nil
}

func verifyAt(directory string, input runtimecontract.RunnerInput, snapshot runtimecontract.RuntimeContextSnapshot, now time.Time, readOnly bool) error {
	if snapshot.ValidateFor(input, now) != nil {
		return ErrContextFiles
	}
	root, err := openRoot(directory, readOnly)
	if err != nil {
		return err
	}
	defer root.Close()
	return verifyTree(root, input, snapshot)
}

func openRoot(directory string, readOnly bool) (*os.Root, error) {
	if !filepath.IsAbs(directory) || filepath.Clean(directory) != directory {
		return nil, ErrContextFiles
	}
	// Ни один компонент фиксированного mount path не может быть symlink.
	for current := directory; current != "/"; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return nil, ErrContextFiles
		}
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return nil, ErrContextFiles
	}
	file, err := root.Open(".")
	if err != nil {
		root.Close()
		return nil, ErrContextFiles
	}
	defer file.Close()
	var stat unix.Statfs_t
	if unix.Fstatfs(int(file.Fd()), &stat) != nil || readOnly && stat.Flags&unix.ST_RDONLY == 0 {
		root.Close()
		return nil, ErrContextFiles
	}
	return root, nil
}

func clearTree(root *os.Root) error {
	file, err := root.Open(".")
	if err != nil {
		return ErrContextFiles
	}
	defer file.Close()
	entries, err := file.ReadDir(3)
	if err != nil && !errors.Is(err, io.EOF) {
		return ErrContextFiles
	}
	for _, entry := range entries {
		if entry.Name() != "skills" && entry.Name() != "memory" && entry.Name() != "manifest.json" {
			return ErrContextFiles
		}
	}
	if more, _ := file.ReadDir(1); len(more) != 0 {
		return ErrContextFiles
	}
	for _, name := range []string{"manifest.json", "skills", "memory"} {
		if root.RemoveAll(name) != nil {
			return ErrContextFiles
		}
	}
	return nil
}

func writeFile(root *os.Root, relative string, raw []byte) error {
	file, err := root.OpenFile(relative, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o440)
	if err != nil {
		return ErrContextFiles
	}
	_, writeErr := file.Write(raw)
	modeErr := exactMode(file, 0o440)
	syncErr, closeErr := file.Sync(), file.Close()
	if writeErr != nil || modeErr != nil || syncErr != nil || closeErr != nil {
		return ErrContextFiles
	}
	return nil
}

func manifestBytes(input runtimecontract.RunnerInput, snapshot runtimecontract.RuntimeContextSnapshot) ([]byte, error) {
	// Always-hot assistant сохраняет одно дерево exact pins между turn. Его
	// context compatibility уже проверяет controller; authority каждой attempt
	// остаётся в свежем execution binding. Остальные manifests привязаны к attempt.
	if input.SystemAssistant {
		input.RuntimeRevisionRef, input.RuntimeRevisionDigest, input.TurnRef, input.Attempt = "", "", "", 0
	}
	raw, err := json.Marshal(manifest{RuntimeRevisionRef: input.RuntimeRevisionRef, RuntimeRevisionDigest: input.RuntimeRevisionDigest,
		SessionRef: input.SessionRef, TurnRef: input.TurnRef, Attempt: input.Attempt, Snapshot: snapshot})
	if err != nil || len(raw) > runtimecontract.MaximumRunnerInputBytes {
		return nil, ErrContextFiles
	}
	return raw, nil
}

// MemoryRepresentation не использует generated state ~/.codex/memories.
func MemoryRepresentation(memory runtimecontract.RuntimeMemoryRecord) []byte {
	metadata := memory
	metadata.Summary = ""
	raw, _ := json.Marshal(metadata)
	return []byte("# KodexMemoryRecord\n\n```json\n" + string(raw) + "\n```\n\n" + memory.Summary + "\n")
}

type expectedFile struct {
	size   int64
	digest string
}

func verifyTree(root *os.Root, input runtimecontract.RunnerInput, snapshot runtimecontract.RuntimeContextSnapshot) error {
	files := map[string]expectedFile{}
	add := func(name string, raw []byte) {
		digest := sha256.Sum256(raw)
		files[name] = expectedFile{int64(len(raw)), "sha256:" + hex.EncodeToString(digest[:])}
	}
	raw, err := manifestBytes(input, snapshot)
	if err != nil {
		return err
	}
	add("manifest.json", raw)
	for _, memory := range snapshot.Memories {
		add("memory/"+memory.RecordRef+".md", MemoryRepresentation(memory))
	}
	for _, skill := range snapshot.Skills {
		for _, pin := range skill.Files {
			files[path.Join("skills", skill.BundleRef, pin.Path)] = expectedFile{pin.SizeBytes, pin.Digest}
		}
	}
	dirs := map[string]bool{".": true, "skills": true, "memory": true}
	for name := range files {
		for parent := path.Dir(name); parent != "."; parent = path.Dir(parent) {
			dirs[parent] = true
		}
	}
	count := 0
	err = fs.WalkDir(root.FS(), ".", func(name string, entry fs.DirEntry, walkErr error) error {
		count++
		if walkErr != nil || count > len(files)+len(dirs) || entry.Type()&os.ModeSymlink != 0 {
			return ErrContextFiles
		}
		info, err := entry.Info()
		if err != nil {
			return ErrContextFiles
		}
		if entry.IsDir() {
			if !dirs[name] || name != "." && info.Mode().Perm() != 0o750 {
				return ErrContextFiles
			}
			return nil
		}
		pin, ok := files[name]
		if !ok || !info.Mode().IsRegular() || info.Mode().Perm() != 0o440 || info.Size() != pin.size {
			return ErrContextFiles
		}
		file, err := root.OpenFile(name, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
		if err != nil {
			return ErrContextFiles
		}
		defer file.Close()
		actual, err := file.Stat()
		stat, validStat := actualStat(actual)
		if err != nil || !validStat || stat.Nlink != 1 || !os.SameFile(info, actual) {
			return ErrContextFiles
		}
		hash := sha256.New()
		size, err := io.Copy(hash, io.LimitReader(file, pin.size+1))
		if err != nil || size != pin.size || "sha256:"+hex.EncodeToString(hash.Sum(nil)) != pin.digest {
			return ErrContextFiles
		}
		return nil
	})
	if err != nil || count != len(files)+len(dirs) {
		return ErrContextFiles
	}
	return validateSkillManifests(root, snapshot)
}

func actualStat(info fs.FileInfo) (*syscall.Stat_t, bool) {
	if info == nil {
		return nil, false
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	return stat, ok
}

func validateSkillManifests(root *os.Root, snapshot runtimecontract.RuntimeContextSnapshot) error {
	for _, skill := range snapshot.Skills {
		file, err := root.Open(path.Join("skills", skill.BundleRef, "SKILL.md"))
		if err != nil {
			return ErrContextFiles
		}
		raw, readErr := io.ReadAll(io.LimitReader(file, 256<<10+1))
		closeErr := file.Close()
		if readErr != nil || closeErr != nil || len(raw) > 256<<10 || !utf8.Valid(raw) || bytes.IndexByte(raw, 0) >= 0 {
			return ErrContextFiles
		}
		text := strings.ReplaceAll(string(raw), "\r\n", "\n")
		if !strings.HasPrefix(text, "---\n") {
			return ErrContextFiles
		}
		end := strings.Index(text[4:], "\n---\n")
		if end < 0 || strings.TrimSpace(text[4+end+5:]) == "" {
			return ErrContextFiles
		}
		var header struct {
			Name        string `yaml:"name"`
			Description string `yaml:"description"`
		}
		if yaml.UnmarshalStrict([]byte(text[4:4+end]), &header) != nil || header.Name != skill.Name || header.Description != skill.Description {
			return ErrContextFiles
		}
	}
	return nil
}

type boundedWriter struct {
	writer    io.Writer
	remaining int64
	failed    bool
}

func (writer *boundedWriter) Write(raw []byte) (int, error) {
	if int64(len(raw)) > writer.remaining {
		writer.failed = true
		return 0, ErrContextFiles
	}
	n, err := writer.writer.Write(raw)
	writer.remaining -= int64(n)
	if err != nil || n != len(raw) {
		writer.failed = true
	}
	return n, err
}
