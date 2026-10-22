package stagingcrypto

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"syscall"
)

// ErrMaterialFile gives a fixed administrative-file failure without path, key identity, material or raw
// filesystem diagnostics.
// ErrMaterialFile сообщает фиксированный administrative-file failure без path, key identity, material и
// raw filesystem diagnostics.
var ErrMaterialFile = errors.New("secret draft key material file is invalid or unavailable")

// MaterialSummary exposes only validated manifest revision and digest, not key IDs or material; it is not
// a runtime guard acknowledgement.
// MaterialSummary выдаёт лишь проверенные manifest revision и digest, не key IDs и material; это не
// runtime guard acknowledgement.
type MaterialSummary struct {
	Revision int64  `json:"revision"`
	Digest   string `json:"digest"`
}

// GenerateFile creates an initial random keyring in a new exclusive file and clears its material
// afterward; it neither overwrites an existing file nor activates it in the cluster.
// GenerateFile создаёт исходный случайный keyring в новом exclusive file и затем очищает материал;
// существующий файл не перезаписывает и в кластере не активирует.
func GenerateFile(output string) error {
	document := keyringDocument{Version: 1, Revision: 1}
	if err := appendRandomKey(&document, 1); err != nil {
		return err
	}
	defer clearKeyring(&document)
	return writeMaterial(output, document)
}

// RotateFile requires the exact input revision and a different output path, retains all read keys and adds
// one higher-generation current key; capacity/overflow fail closed without modifying input.
// RotateFile требует точный input revision и другой output path, сохраняет все read keys и добавляет
// current key следующего generation; capacity/overflow закрыто отклоняются без изменения input.
func RotateFile(input, output string, expectedRevision int64) error {
	if expectedRevision < 1 || input == output {
		return ErrMaterialFile
	}
	document, _, err := readMaterial(input)
	defer clearKeyring(&document)
	if err != nil || document.Revision != expectedRevision || document.Revision == math.MaxInt64 || len(document.Keys) >= maximumKeyringKeys {
		return ErrMaterialFile
	}
	var highest int64
	for _, key := range document.Keys {
		if key.Generation > highest {
			highest = key.Generation
		}
	}
	if highest == math.MaxInt64 {
		return ErrMaterialFile
	}
	document.Revision++
	if err := appendRandomKey(&document, highest+1); err != nil {
		return err
	}
	return writeMaterial(output, document)
}

// CheckFile validates the private keyring and returns only its safe summary, clearing decoded material
// without updating a runtime guard.
// CheckFile проверяет private keyring и возвращает лишь безопасный summary, очищая decoded material без
// обновления runtime guard.
func CheckFile(input string) (MaterialSummary, error) {
	document, summary, err := readMaterial(input)
	clearKeyring(&document)
	return summary, err
}

// CopyFile validates a private input, sorts retained entries and writes a new exclusive private keyring,
// clearing decoded material afterward; it does not rotate or activate keys.
// CopyFile проверяет private input, сортирует retained entries и пишет новый exclusive private keyring,
// затем очищает decoded material; ключи не ротирует и не активирует.
func CopyFile(input, output string) error {
	document, _, err := readMaterial(input)
	defer clearKeyring(&document)
	if err != nil {
		return err
	}
	sort.Slice(document.Keys, func(i, j int) bool { return document.Keys[i].Generation < document.Keys[j].Generation })
	return writeMaterial(output, document)
}

// appendRandomKey generates 32 random bytes, rejects an ID collision and appends/selects that key; callers
// enforce generation/capacity rules and clear successful material ownership.
// appendRandomKey создаёт 32 случайных байта, отклоняет ID collision и добавляет/выбирает ключ;
// generation/capacity rules и очистка успешно переданного материала принадлежат caller.
func appendRandomKey(document *keyringDocument, generation int64) error {
	material := make([]byte, 32)
	if _, err := rand.Read(material); err != nil {
		clear(material)
		return ErrMaterialFile
	}
	digest := sha256.Sum256(material)
	id := hex.EncodeToString(digest[:])
	for _, existing := range document.Keys {
		if existing.ID == id {
			clear(material)
			return ErrMaterialFile
		}
	}
	document.Keys = append(document.Keys, keyDocument{ID: id, Generation: generation, Material: material})
	document.Current = id
	return nil
}

// readMaterial anchors a clean absolute path in its private parent, opens with O_NOFOLLOW and requires an
// owned singly-linked regular 0400/0600 file with bounded exact size. It clears raw bytes and returns
// decoded material for caller cleanup.
// readMaterial привязывает clean absolute path к private parent, открывает с O_NOFOLLOW и требует owned
// singly-linked regular file режима 0400/0600 с ограниченным точным размером. Очищает raw bytes, decoded
// material передаёт caller для очистки.
func readMaterial(path string) (keyringDocument, MaterialSummary, error) {
	root, name, err := privateRoot(path)
	if err != nil {
		return keyringDocument{}, MaterialSummary{}, err
	}
	defer root.Close()
	file, err := root.OpenFile(name, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return keyringDocument{}, MaterialSummary{}, ErrMaterialFile
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || (info.Mode().Perm() != 0o400 && info.Mode().Perm() != 0o600) || info.Size() < 1 || info.Size() > maximumKeyringBytes || !ownedFile(info) {
		return keyringDocument{}, MaterialSummary{}, ErrMaterialFile
	}
	raw, err := io.ReadAll(io.LimitReader(file, maximumKeyringBytes+1))
	defer clear(raw)
	if err != nil || int64(len(raw)) != info.Size() {
		return keyringDocument{}, MaterialSummary{}, ErrMaterialFile
	}
	document, manifest, err := decodeKeyring(raw)
	if err != nil {
		clearKeyring(&document)
		return keyringDocument{}, MaterialSummary{}, ErrMaterialFile
	}
	return document, MaterialSummary{Revision: manifest.Revision, Digest: manifest.Digest}, nil
}

// writeMaterial validates serialized keyring bytes, creates an exclusive 0600 file, changes it to 0400 and
// syncs file then directory before success. On failure it attempts removal of its new basename; it is not
// an atomic key activation or guaranteed rollback.
// writeMaterial проверяет serialized keyring bytes, создаёт exclusive 0600 file, меняет режим на 0400 и
// синхронизирует файл, затем каталог до успеха. При отказе пытается удалить свой новый basename; это не
// atomic key activation и не гарантированный rollback.
func writeMaterial(path string, document keyringDocument) error {
	raw, err := json.Marshal(document)
	defer clear(raw)
	if err != nil || len(raw) > maximumKeyringBytes {
		return ErrMaterialFile
	}
	verified, _, err := decodeKeyring(raw)
	clearKeyring(&verified)
	if err != nil {
		return ErrMaterialFile
	}
	root, name, err := privateRoot(path)
	if err != nil {
		return err
	}
	defer root.Close()
	file, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return ErrMaterialFile
	}
	committed := false
	defer func() {
		_ = file.Close()
		if !committed {
			_ = root.Remove(name)
		}
	}()
	if _, err := file.Write(raw); err != nil {
		return ErrMaterialFile
	}
	if err := file.Chmod(0o400); err != nil {
		return ErrMaterialFile
	}
	if err := file.Sync(); err != nil {
		return ErrMaterialFile
	}
	if err := file.Close(); err != nil {
		return ErrMaterialFile
	}
	dir, err := root.Open(".")
	if err != nil {
		return ErrMaterialFile
	}
	err = dir.Sync()
	closeErr := dir.Close()
	if err != nil || closeErr != nil {
		return ErrMaterialFile
	}
	committed = true
	return nil
}

// privateRoot requires a clean absolute path, rejects a pre-open resolved parent alias and opens an owned
// 0700 directory as os.Root; callers close it. Descriptor confinement is not a mount namespace or
// protection against every privileged filesystem race.
// privateRoot требует clean absolute path, отклоняет разрешённый alias родителя до открытия и открывает
// owned 0700 directory как os.Root; закрывает caller. Descriptor confinement не заменяет mount namespace и
// защиту от всех privileged filesystem races.
func privateRoot(path string) (*os.Root, string, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, "", ErrMaterialFile
	}
	parent := filepath.Dir(path)
	resolved, err := filepath.EvalSymlinks(parent)
	if err != nil || resolved != parent {
		return nil, "", ErrMaterialFile
	}
	root, err := os.OpenRoot(parent)
	if err != nil {
		return nil, "", ErrMaterialFile
	}
	info, err := root.Stat(".")
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0o700 || !owned(info) {
		_ = root.Close()
		return nil, "", ErrMaterialFile
	}
	return root, filepath.Base(path), nil
}

// owned requires Unix Stat_t metadata whose UID equals the effective process UID; it does not check file
// type, mode or hard-link count.
// owned требует Unix Stat_t metadata с UID эффективного пользователя процесса; тип файла, режим и
// hard-link count не проверяет.
func owned(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Uid == uint32(os.Geteuid())
}

// ownedFile adds a single-hard-link requirement to effective-UID ownership; callers separately verify
// regular type, mode and size.
// ownedFile добавляет требование одного hard link к effective-UID ownership; regular type, mode и size
// проверяет caller.
func ownedFile(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return owned(info) && ok && stat.Nlink == 1
}
