package runtimecontract

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"regexp"
)

const ImageInventorySchema = "kodex.dev/image-tool-inventory/v1"
const ImageInventoryBindingSchema = "kodex.dev/image-tool-inventory-binding/v1"
const ImageInventoryPath = "/usr/share/kodex/tool-inventory.json"
const MaximumImageInventoryBytes = 128 << 10

var errImageInventory = errors.New("image tool inventory is invalid")
var lowerHexDigestPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)
var inventoryVersionPattern = regexp.MustCompile(`^v?[0-9]+(?:\.[0-9]+){0,3}(?:[-+][A-Za-z0-9.-]{1,48})?$`)

// Реестр определяет наблюдение, но никогда не выдаёт capability.
type ImageToolProbe struct {
	Name     string
	Paths    []string
	Args     []string
	Required bool
}

func ImageToolProbes() []ImageToolProbe {
	probe := func(name, command string, args ...string) ImageToolProbe {
		return ImageToolProbe{Name: name, Paths: []string{"/usr/local/bin/" + command, "/usr/bin/" + command}, Args: args, Required: true}
	}
	optional := func(value ImageToolProbe) ImageToolProbe { value.Required = false; return value }
	return []ImageToolProbe{
		{Name: "bash", Paths: []string{"/usr/local/bin/bash", "/usr/bin/bash", "/bin/bash"}, Args: []string{"--version"}, Required: true},
		probe("curl", "curl", "--version"), probe("git", "git", "--version"), probe("gh", "gh", "--version"),
		probe("jq", "jq", "--version"), probe("yq", "yq", "--version"), probe("ripgrep", "rg", "--version"),
		probe("make", "make", "--version"), probe("just", "just", "--version"),
		{Name: "go", Paths: []string{"/usr/local/go/bin/go", "/usr/local/bin/go", "/usr/bin/go"}, Args: []string{"version"}, Required: true},
		probe("goimports", "goimports", "-h"), probe("gofumpt", "gofumpt", "--version"),
		probe("golangci-lint", "golangci-lint", "--version"), probe("staticcheck", "staticcheck", "-version"),
		probe("goose", "goose", "--version"), probe("sqlc", "sqlc", "version"), probe("buf", "buf", "--version"),
		probe("protoc", "protoc", "--version"), probe("protoc-gen-go", "protoc-gen-go", "--version"),
		probe("protoc-gen-go-grpc", "protoc-gen-go-grpc", "--version"), probe("grpcurl", "grpcurl", "-version"),
		probe("mockgen", "mockgen", "-version"), probe("oapi-codegen", "oapi-codegen", "-version"),
		probe("node", "node", "--version"), probe("npm", "npm", "--version"), probe("pnpm", "pnpm", "--version"), probe("yarn", "yarn", "--version"),
		probe("typescript", "tsc", "--version"), probe("eslint", "eslint", "--version"), probe("prettier", "prettier", "--version"),
		probe("vite", "vite", "--version"), probe("vue-tsc", "vue-tsc", "--version"), probe("vitest", "vitest", "--version"),
		probe("playwright", "playwright", "--version"),
		{Name: "chromium", Paths: []string{"/usr/local/bin/chromium", "/usr/local/bin/chromium-browser", "/usr/local/bin/google-chrome", "/usr/bin/chromium", "/usr/bin/chromium-browser", "/usr/bin/google-chrome"}, Args: []string{"--version"}, Required: true},
		{Name: "playwright-mcp", Paths: []string{"/usr/local/bin/playwright-mcp", "/usr/local/bin/mcp-server-playwright", "/usr/bin/playwright-mcp", "/usr/bin/mcp-server-playwright"}, Args: []string{"--version"}, Required: true},
		probe("wscat", "wscat", "--version"), probe("codex", "codex", "--version"),
		optional(probe("corepack", "corepack", "--version")), optional(probe("python3", "python3", "--version")), optional(probe("pip", "pip", "--version")),
		optional(probe("kubectl", "kubectl", "version", "--client", "--output=json")), optional(probe("kustomize", "kustomize", "version")),
		optional(probe("helm", "helm", "version", "--short")), optional(probe("buildctl", "buildctl", "--version")), optional(probe("docker", "docker", "--version")),
		optional(probe("shellcheck", "shellcheck", "--version")), optional(probe("hadolint", "hadolint", "--version")),
		optional(probe("govulncheck", "govulncheck", "-version")), optional(probe("gitleaks", "gitleaks", "version")),
	}
}

type ImageToolObservation struct {
	Name     string `json:"name"`
	Status   string `json:"status"`
	Path     string `json:"path"`
	Version  string `json:"version"`
	SHA256   string `json:"sha256"`
	Required bool   `json:"required"`
}

type ImageToolManifest struct {
	Schema                string                 `json:"schema"`
	SpecSHA256            string                 `json:"specSHA256"`
	ImmutableBuildSHA256  string                 `json:"immutableBuildSHA256"`
	RuntimeContractSHA256 string                 `json:"runtimeContractSHA256"`
	Platform              string                 `json:"platform"`
	Tools                 []ImageToolObservation `json:"tools"`
}

type ImagePlatformInventory struct {
	PlatformDigest string            `json:"platformDigest"`
	ManifestSHA256 string            `json:"manifestSHA256"`
	Manifest       ImageToolManifest `json:"manifest"`
}

type ImageToolInventory struct {
	Schema           string                   `json:"schema"`
	ImageDigest      string                   `json:"imageDigest"`
	ProvenanceSHA256 string                   `json:"provenanceSHA256"`
	Platforms        []ImagePlatformInventory `json:"platforms"`
}

func (manifest ImageToolManifest) Validate() error {
	if manifest.Schema != ImageInventorySchema || !lowerHexDigestPattern.MatchString(manifest.SpecSHA256) ||
		!lowerHexDigestPattern.MatchString(manifest.ImmutableBuildSHA256) || !lowerHexDigestPattern.MatchString(manifest.RuntimeContractSHA256) ||
		(manifest.Platform != "linux/amd64" && manifest.Platform != "linux/arm64") || len(manifest.Tools) != len(ImageToolProbes()) {
		return errImageInventory
	}
	for index, probe := range ImageToolProbes() {
		item := manifest.Tools[index]
		if item.Name != probe.Name || item.Required != probe.Required {
			return errImageInventory
		}
		if item.Status == "MISSING" {
			if item.Path != "" || item.Version != "" || item.SHA256 != "" {
				return errImageInventory
			}
			continue
		}
		allowedPath := false
		for _, path := range probe.Paths {
			allowedPath = allowedPath || item.Path == path
		}
		if !allowedPath || !lowerHexDigestPattern.MatchString(item.SHA256) {
			return errImageInventory
		}
		if item.Status == "VERIFIED" {
			if !inventoryVersionPattern.MatchString(item.Version) || len(item.Version) > 80 {
				return errImageInventory
			}
		} else if item.Status != "PROBE_FAILED" || item.Version != "" {
			return errImageInventory
		}
	}
	return nil
}

func (inventory ImageToolInventory) Validate() error {
	if inventory.Schema != ImageInventoryBindingSchema || !imageDigestPattern.MatchString(inventory.ImageDigest) ||
		!lowerHexDigestPattern.MatchString(inventory.ProvenanceSHA256) || len(inventory.Platforms) < 1 || len(inventory.Platforms) > 2 {
		return errImageInventory
	}
	seen := map[string]bool{}
	for _, platform := range inventory.Platforms {
		if platform.Manifest.Validate() != nil || !imageDigestPattern.MatchString(platform.PlatformDigest) || seen[platform.Manifest.Platform] {
			return errImageInventory
		}
		seen[platform.Manifest.Platform] = true
		raw, _ := json.Marshal(platform.Manifest)
		if ImageInventorySHA256(raw) != platform.ManifestSHA256 {
			return errImageInventory
		}
	}
	return nil
}

func ImageInventorySHA256(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func DecodeImageToolInventory(raw []byte) (ImageToolInventory, error) {
	var value ImageToolInventory
	if decodeImageInventory(raw, &value) != nil || value.Validate() != nil {
		return ImageToolInventory{}, errImageInventory
	}
	return value, nil
}

func DecodeImageToolManifest(raw []byte) (ImageToolManifest, error) {
	var value ImageToolManifest
	if decodeImageInventory(raw, &value) != nil || value.Validate() != nil {
		return ImageToolManifest{}, errImageInventory
	}
	// Единственная каноническая форма позволяет проверить digest при восстановлении.
	canonical, _ := json.Marshal(value)
	if !bytes.Equal(raw, canonical) {
		return ImageToolManifest{}, errImageInventory
	}
	return value, nil
}

func decodeImageInventory(raw []byte, value any) error {
	if len(raw) == 0 || len(raw) > MaximumImageInventoryBytes || promptJSONUnique(json.NewDecoder(bytes.NewReader(raw)), 0) != nil {
		return errImageInventory
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(value) != nil || decoder.Decode(new(any)) != io.EOF {
		return errImageInventory
	}
	return nil
}
