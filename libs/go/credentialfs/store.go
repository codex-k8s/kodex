// Package credentialfs читает exact credential material из защищённого
// read-only дерева без обхода root через symlink или path traversal.
package credentialfs

import (
	"errors"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/codex-k8s/matter-codex/libs/go/securefile"
)

const maximumCredentialBytes = 1 << 20

var referencePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,96}$`)

type Store struct{ root string }

func New(root string) (*Store, error) {
	if !filepath.IsAbs(root) || filepath.Clean(root) != root {
		return nil, errors.New("credential root is invalid")
	}
	return &Store{root: root}, nil
}

func (store *Store) Read(reference, name string) ([]byte, error) {
	if store == nil || !referencePattern.MatchString(reference) || !safeName(name) {
		return nil, errors.New("credential reference is invalid")
	}
	root, err := filepath.EvalSymlinks(store.root)
	if err != nil {
		return nil, errors.New("credential root is unavailable")
	}
	resolved, err := filepath.EvalSymlinks(filepath.Join(root, reference, name))
	if err != nil {
		return nil, errors.New("credential file is unavailable")
	}
	relative, err := filepath.Rel(root, resolved)
	if err != nil || relative == "." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return nil, errors.New("credential file escapes root")
	}
	value, err := securefile.ReadWithin(root, resolved, maximumCredentialBytes)
	if err != nil {
		return nil, errors.New("credential file is unavailable")
	}
	return value, nil
}

func safeName(name string) bool {
	if name == "" || len(name) > 128 || filepath.Base(name) != name || name == "." || name == ".." {
		return false
	}
	for _, character := range name {
		if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' ||
			character >= '0' && character <= '9' || character == '.' || character == '-' || character == '_' {
			continue
		}
		return false
	}
	return true
}
