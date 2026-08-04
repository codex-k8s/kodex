package credential

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/codex-k8s/matter-codex/services/external/integration-gateway/internal/domain/types/entity"
)

const maximumCredentialBytes = 64 << 10

type FileSource struct {
	root string
}

func NewFileSource(root string) (*FileSource, error) {
	if !filepath.IsAbs(root) {
		return nil, errors.New("credential root must be absolute")
	}
	return &FileSource{root: filepath.Clean(root)}, nil
}

func (source *FileSource) Resolve(_ context.Context, connection entity.Connection) (map[string]string, error) {
	values := make(map[string]string, len(connection.CredentialBindingRefs))
	for _, binding := range connection.CredentialBindingRefs {
		if binding.Purpose == "" || binding.SecretRef == "" {
			return nil, errors.New("credential binding is invalid")
		}
		clean := filepath.Clean(binding.SecretRef)
		if filepath.IsAbs(clean) || clean == "." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
			return nil, errors.New("credential reference is invalid")
		}
		path := filepath.Join(source.root, clean)
		relative, err := filepath.Rel(source.root, path)
		if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return nil, errors.New("credential reference escapes root")
		}
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maximumCredentialBytes || info.Mode().Perm()&0o007 != 0 {
			return nil, errors.New("credential file is unsafe")
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, errors.New("read credential file")
		}
		value := string(raw)
		if value == "" || len(value) > maximumCredentialBytes ||
			strings.TrimSpace(value) != value || strings.ContainsAny(value, "\r\n") {
			return nil, errors.New("credential value is invalid")
		}
		if _, duplicate := values[binding.Purpose]; duplicate {
			return nil, errors.New("credential purpose is duplicated")
		}
		values[binding.Purpose] = value
	}
	return values, nil
}
