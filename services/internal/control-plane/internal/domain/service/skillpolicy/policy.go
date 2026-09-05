// Package skillpolicy проверяет структуру Skill без подмены malware scan.
package skillpolicy

import (
	"bytes"
	"errors"
	"io"
	"path"
	"strings"
	"unicode/utf8"

	"go.yaml.in/yaml/v3"
)

const MaximumManifestBytes = 256 << 10

var ErrInvalid = errors.New("invalid skill bundle structure")

type Manifest struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
}

func ValidatePaths(paths []string) error {
	if len(paths) == 0 || len(paths) > 128 {
		return ErrInvalid
	}
	seen := make(map[string]bool, len(paths))
	manifest := false
	for _, name := range paths {
		if name == "" || len(name) > 240 || !utf8.ValidString(name) || strings.ContainsAny(name, "\\\x00\n\r:") || path.IsAbs(name) || path.Clean(name) != name || name == "." || name == ".." || strings.HasPrefix(name, "../") {
			return ErrInvalid
		}
		for _, part := range strings.Split(name, "/") {
			if strings.TrimSpace(part) != part || strings.HasPrefix(part, ".") {
				return ErrInvalid
			}
		}
		key := strings.ToLower(name)
		if seen[key] {
			return ErrInvalid
		}
		seen[key] = true
		if key == "skill.md" {
			if name != "SKILL.md" {
				return ErrInvalid
			}
			manifest = true
		} else {
			switch strings.ToLower(path.Ext(name)) {
			case ".md", ".txt", ".json", ".csv", ".png", ".jpg", ".jpeg", ".webp":
			default:
				return ErrInvalid
			}
		}
	}
	if !manifest {
		return ErrInvalid
	}
	return nil
}

func ParseManifest(body []byte) (Manifest, error) {
	if len(body) > MaximumManifestBytes || !utf8.Valid(body) || bytes.IndexByte(body, 0) >= 0 {
		return Manifest{}, ErrInvalid
	}
	text := strings.ReplaceAll(string(body), "\r\n", "\n")
	if !strings.HasPrefix(text, "---\n") {
		return Manifest{}, ErrInvalid
	}
	end := strings.Index(text[4:], "\n---\n")
	if end < 0 {
		return Manifest{}, ErrInvalid
	}
	header := text[4 : 4+end]
	instructions := text[4+end+5:]
	if strings.TrimSpace(instructions) == "" {
		return Manifest{}, ErrInvalid
	}
	decoder := yaml.NewDecoder(strings.NewReader(header))
	decoder.KnownFields(true)
	var manifest Manifest
	if decoder.Decode(&manifest) != nil {
		return Manifest{}, ErrInvalid
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return Manifest{}, ErrInvalid
	}
	if strings.TrimSpace(manifest.Name) == "" || len([]rune(manifest.Name)) > 160 || strings.TrimSpace(manifest.Description) == "" || len([]rune(manifest.Description)) > 2000 {
		return Manifest{}, ErrInvalid
	}
	return manifest, nil
}
