// Package contextfiles materializes and verifies immutable pinned skill/memory snapshots.
// Пакет contextfiles материализует и проверяет неизменяемые закреплённые снимки навыков и памяти.
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

// ErrContextFiles is the fixed failure for invalid snapshot materialization, permissions, layout or integrity without copying file contents into diagnostics.
// ErrContextFiles задаёт фиксированный отказ при неверной материализации снимка, правах, структуре или целостности без копирования содержимого файлов в диагностику.
var ErrContextFiles = errors.New("runtime context files are invalid")

// FileSource writes skill bytes for already validated immutable bundle/file pins; it accepts neither caller URLs nor arbitrary filesystem paths.
// FileSource записывает байты навыка по уже проверенным неизменяемым привязкам пакета и файла, не принимая URL вызывающей стороны или произвольные файловые пути.
type FileSource interface {
	// WriteSkillFile streams one pinned skill file into the supplied bounded writer under the caller context; materialization verifies exact size and hash afterward.
	// WriteSkillFile передаёт один закреплённый файл навыка в ограниченный writer под контекстом вызывающего; материализация затем сверяет точные размер и хеш.
	WriteSkillFile(context.Context, runtimecontract.RunnerInput, runtimecontract.RuntimeSkillBundle, runtimecontract.RuntimeSkillFile, io.Writer) error
}

// manifest binds the serialized context snapshot to session and ordinary revision/turn/attempt metadata; SYSTEM reuse normalization is explicit in manifestBytes.
// manifest связывает сериализованный снимок контекста с сессией и обычными метаданными ревизии, хода и попытки; нормализация повторного использования SYSTEM явно задана в manifestBytes.
type manifest struct {
	RuntimeRevisionRef    string                                 `json:"runtime_revision_ref"`
	RuntimeRevisionDigest string                                 `json:"runtime_revision_digest"`
	SessionRef            string                                 `json:"session_ref"`
	TurnRef               string                                 `json:"turn_ref"`
	Attempt               int32                                  `json:"attempt"`
	Snapshot              runtimecontract.RuntimeContextSnapshot `json:"snapshot"`
}

// Materialize materializes the validated immutable context at the canonical context root using the caller lifecycle context and current time.
// It performs local file writes and clears the previous owned tree; it does not publish owner grants.
// Materialize материализует проверенный неизменяемый контекст в каноническом корне с контекстом жизненного цикла вызывающего и текущим временем.
// Локально записывает файлы и очищает прежнее принадлежащее ему дерево, не публикуя доступы владельца.
func Materialize(ctx context.Context, input runtimecontract.RunnerInput, snapshot runtimecontract.RuntimeContextSnapshot, source FileSource) error {
	return materializeAt(ctx, runtimecontract.RuntimeContextRoot, input, snapshot, source, time.Now())
}

// Verify validates current snapshot eligibility and exact canonical context files, requiring a filesystem read-only flag rather than only restrictive chmod.
// It reads local content and metadata without fetching missing files or repairing an invalid tree.
// Verify проверяет актуальную допустимость снимка и точные канонические файлы контекста, требуя флаг read-only файловой системы, а не только ограничивающий chmod.
// Читает локальные данные и метаданные без загрузки отсутствующих файлов и исправления неверного дерева.
func Verify(input runtimecontract.RunnerInput, snapshot runtimecontract.RuntimeContextSnapshot) error {
	return verifyAt(runtimecontract.RuntimeContextRoot, input, snapshot, time.Now(), true)
}

// materializeAt validates context before fetching, clears only the owned layout and writes bounded pinned skill files plus typed memory representations.
// Each skill must match exact size/hash and mode; the manifest is written last and the complete tree is verified before success.
// Requires a single startup writer; failed work attempts best-effort cleanup and never reports a partial tree as ready.
// materializeAt проверяет контекст до загрузки, очищает только принадлежащую ему структуру и пишет ограниченные закреплённые файлы навыков с типизированными представлениями памяти.
// Каждый навык должен совпасть по точным размеру, хешу и режиму; манифест пишется последним, успех требует проверки полного дерева.
// Требуется единственный writer начальной подготовки; ошибка приводит к попытке очистки без гарантии успеха, частичное дерево не объявляется готовым.
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

// makeDirectories creates each relative component beneath os.Root, opens it as a no-follow directory and restores exact setgid 0750 mode despite process umask.
// It relies on mount group inheritance; it does not grant access to an arbitrary external path.
// makeDirectories создаёт каждый относительный компонент под os.Root, открывает его как каталог без следования ссылкам и восстанавливает точный setgid 0750 независимо от umask процесса.
// Полагается на наследование группы монтирования, не выдавая доступ к произвольному внешнему пути.
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
		// Каждый вложенный каталог должен наследовать fsGroup mount. Иначе
		// bundle под skills/ получит primary GID materializer, а изолированный
		// provider не сможет пройти к опубликованному SKILL.md.
		modeErr := exactMode(directory, os.ModeSetgid|0o750)
		closeErr := directory.Close()
		if modeErr != nil || closeErr != nil {
			return ErrContextFiles
		}
	}
	return nil
}

// exactMode changes an open inode's mode and reads it back, checking permission/setgid bits and returning the fixed context error on either failure.
// exactMode изменяет режим открытого inode и считывает его обратно, проверяя биты доступа и setgid с фиксированной ошибкой контекста при любом отказе.
func exactMode(file *os.File, mode os.FileMode) error {
	if file.Chmod(mode) != nil {
		return ErrContextFiles
	}
	info, err := file.Stat()
	if err != nil || info.Mode()&(os.ModePerm|os.ModeSetgid) != mode&(os.ModePerm|os.ModeSetgid) {
		return ErrContextFiles
	}
	return nil
}

// verifyAt checks snapshot eligibility at the supplied time, opens the requested root under its read-only policy and verifies exact local tree contents.
// verifyAt проверяет допустимость снимка на переданное время, открывает заданный корень с его политикой read-only и сверяет точное локальное дерево.
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

// openRoot requires a clean absolute directory path with no Lstat-observed symlink ancestors, then opens os.Root and checks its filesystem flags.
// Read-only mode requires ST_RDONLY; the preliminary pathname checks alone are not an atomic provenance or ownership proof.
// openRoot требует нормализованный абсолютный путь каталога без обнаруженных Lstat ссылок в родителях, затем открывает os.Root и проверяет флаги файловой системы.
// Режим read-only требует ST_RDONLY; предварительные проверки имён пути сами по себе не доказывают атомарно происхождение или владельца.
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

// clearTree accepts only the three owned top-level names skills, memory and manifest.json, rejects extra entries and removes those paths through os.Root.
// Used for renewal and best-effort failed-write cleanup; it does not traverse a symlink target outside the root.
// clearTree принимает только три собственных имени верхнего уровня skills, memory и manifest.json, отклоняет лишние элементы и удаляет эти пути через os.Root.
// Используется при обновлении и попытке очистки после ошибки записи; цель ссылки вне корня не обходится.
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

// writeFile exclusively creates one relative file, writes its representation, restores exact 0440 mode, syncs and closes before reporting success.
// Callers own failure cleanup; this helper neither renames atomically nor publishes a readiness receipt.
// writeFile создаёт один относительный файл исключительно новым, записывает представление, восстанавливает точный режим 0440, синхронизирует и закрывает до успеха.
// Очистка после ошибки принадлежит вызывающему; атомарное переименование и квитанция готовности здесь не выполняются.
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

// manifestBytes serializes bounded snapshot/session metadata; SYSTEM clears revision, turn and attempt fields for identical pinned-context reuse.
// Ordinary/project manifests remain attempt-bound; fresh execution authority is checked elsewhere, never inferred from manifest reuse.
// manifestBytes сериализует ограниченные метаданные снимка и сессии; SYSTEM очищает поля ревизии, хода и попытки для повторного использования одинакового закреплённого контекста.
// Обычный и проектный манифесты остаются привязаны к попытке; свежие полномочия выполнения проверяются отдельно, а не выводятся из повторного использования.
func manifestBytes(input runtimecontract.RunnerInput, snapshot runtimecontract.RuntimeContextSnapshot) ([]byte, error) {
	// Always-hot assistant сохраняет одно дерево exact pins между turn. Его
	// context compatibility уже проверяет controller; authority каждой attempt
	// остаётся в свежем execution binding. Остальные manifests привязаны к attempt.
	if input.IsSystemAssistant() {
		input.RuntimeRevisionRef, input.RuntimeRevisionDigest, input.TurnRef, input.Attempt = "", "", "", 0
	}
	raw, err := json.Marshal(manifest{RuntimeRevisionRef: input.RuntimeRevisionRef, RuntimeRevisionDigest: input.RuntimeRevisionDigest,
		SessionRef: input.SessionRef, TurnRef: input.TurnRef, Attempt: input.Attempt, Snapshot: snapshot})
	if err != nil || len(raw) > runtimecontract.MaximumRunnerInputBytes {
		return nil, ErrContextFiles
	}
	return raw, nil
}

// MemoryRepresentation renders typed memory metadata as JSON followed by the record summary in a Markdown file, without reading Codex-generated memory state.
// MemoryRepresentation формирует Markdown-файл с типизированными метаданными памяти в JSON и следующим за ними текстом записи без чтения памяти, сгенерированной Codex.
func MemoryRepresentation(memory runtimecontract.RuntimeMemoryRecord) []byte {
	metadata := memory
	metadata.Summary = ""
	raw, _ := json.Marshal(metadata)
	return []byte("# KodexMemoryRecord\n\n```json\n" + string(raw) + "\n```\n\n" + memory.Summary + "\n")
}

// expectedFile holds the exact byte size and sha256-prefixed digest required for one enumerated context file, not a permission or owner grant.
// expectedFile хранит точные размер в байтах и дайджест с префиксом sha256 для одного перечисленного файла контекста, а не разрешение или доступ владельца.
type expectedFile struct {
	size   int64
	digest string
}

// verifyTree enumerates expected manifest/memory/skill bytes and parent directories, rejecting missing, extra, symlinked or wrong-mode entries.
// Opens each regular file no-follow/nonblocking, checks one hardlink and same inode, then verifies bounded exact size/hash and skill frontmatter.
// It assumes the caller validated snapshot eligibility and does not run antivirus or interpret document instructions.
// verifyTree перечисляет ожидаемые байты манифеста, памяти, навыков и родительские каталоги, отклоняя отсутствующие, лишние элементы, ссылки и неверные режимы.
// Открывает обычный файл без следования ссылкам и блокировки, проверяет одну жёсткую ссылку и тот же inode, затем ограниченные точные размер, хеш и frontmatter навыка.
// Предполагает проверку допустимости снимка вызывающим, не запускает антивирус и не интерпретирует инструкции документов.
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
			if !dirs[name] || name != "." && info.Mode()&(os.ModePerm|os.ModeSetgid) != os.ModeSetgid|0o750 {
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

// actualStat extracts syscall.Stat_t only from non-nil file metadata; unsupported metadata returns false instead of a type assertion panic.
// actualStat извлекает syscall.Stat_t только из непустых метаданных файла; неподдерживаемые метаданные дают false вместо паники утверждения типа.
func actualStat(info fs.FileInfo) (*syscall.Stat_t, bool) {
	if info == nil {
		return nil, false
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	return stat, ok
}

// validateSkillManifests reads each SKILL.md with a 256-KiB bound, rejects invalid UTF-8/NUL or missing body, and strictly matches YAML name/description to snapshot metadata.
// This is format/provenance consistency, not semantic instruction safety or a new scan verdict.
// validateSkillManifests читает каждый SKILL.md с пределом 256 КиБ, отклоняет неверный UTF-8, NUL или отсутствие тела, строго сверяет YAML-имя и описание с метаданными снимка.
// Это согласованность формата и происхождения, а не смысловая безопасность инструкций или новый вердикт сканирования.
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

// boundedWriter tracks exact remaining bytes and sticky overflow/short-write failures while forwarding to file/hash writers; it imposes no independent timeout.
// boundedWriter отслеживает точный остаток байтов и сохраняемый признак переполнения либо короткой записи при передаче в файловый и хеширующий writer; отдельный таймаут не задаёт.
type boundedWriter struct {
	writer    io.Writer
	remaining int64
	failed    bool
}

// Write rejects an over-budget chunk before writing, subtracts actual written bytes and marks any short/error write as failed for final validation.
// It preserves the underlying count/error; the caller must inspect the sticky failure flag even if a short writer returned nil.
// Write отклоняет превышающий бюджет блок до записи, вычитает фактически записанные байты и помечает короткую запись либо ошибку для итоговой проверки.
// Сохраняет число байтов и ошибку вложенного writer; вызывающий должен проверить признак ошибки, даже если короткая запись вернула nil.
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
