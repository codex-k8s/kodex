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
	"syscall"
)

var ErrMaterialFile = errors.New("secret draft key material file is invalid or unavailable")

// MaterialSummary не содержит ключи или их идентификаторы.
type MaterialSummary struct {
	Revision int64  `json:"revision"`
	Digest   string `json:"digest"`
}

func GenerateFile(output string) error {
	document := keyringDocument{Version: 1, Revision: 1}
	if err := appendRandomKey(&document, 1); err != nil {
		return err
	}
	defer clearKeyring(&document)
	return writeMaterial(output, document)
}

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

func CheckFile(input string) (MaterialSummary, error) {
	document, summary, err := readMaterial(input)
	clearKeyring(&document)
	return summary, err
}

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

// OpenRoot привязывает все действия к открытому descriptor приватного каталога.
// EvalSymlinks запрещает исходный alias; O_EXCL/O_NOFOLLOW защищают basename.
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
func owned(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Uid == uint32(os.Geteuid())
}
func ownedFile(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return owned(info) && ok && stat.Nlink == 1
}
