package stackinventory

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"text/template"

	"gopkg.in/yaml.v3"
)

// Stack is the typed projection of the repository root services.yaml needed by
// render, bootstrap and install tooling.
type Stack struct {
	APIVersion string   `yaml:"apiVersion"`
	Kind       string   `yaml:"kind"`
	Metadata   Metadata `yaml:"metadata"`
	Spec       Spec     `yaml:"spec"`
}

// Metadata describes the stack inventory document.
type Metadata struct {
	Name   string `yaml:"name"`
	Status string `yaml:"status"`
}

// Spec contains the stack inventory sections consumed by platform tooling.
type Spec struct {
	Versions           map[string]VersionSpec  `yaml:"versions"`
	Images             map[string]ImageSpec    `yaml:"images"`
	DeployableServices []DeployableServiceSpec `yaml:"deployableServices"`
	PlatformDataStores []PlatformDataStoreSpec `yaml:"platformDataStores"`
	ActiveRoots        []string                `yaml:"activeRoots"`
	RepositorySources  map[string]any          `yaml:"repositorySources"`
	SourceOfTruth      []string                `yaml:"sourceOfTruth"`
}

// VersionSpec records a named stack version and the paths that should bump it.
type VersionSpec struct {
	Value  string   `yaml:"value"`
	BumpOn []string `yaml:"bumpOn"`
}

// ImageSpec records how a stack image is resolved for manifests and build
// tooling.
type ImageSpec struct {
	Type             string `yaml:"type"`
	From             string `yaml:"from"`
	Local            string `yaml:"local"`
	Repository       string `yaml:"repository"`
	TagTemplate      string `yaml:"tagTemplate"`
	Dockerfile       string `yaml:"dockerfile"`
	Target           string `yaml:"target"`
	Context          string `yaml:"context"`
	ImageEnv         string `yaml:"imageEnv"`
	MigratesDatabase string `yaml:"migratesDatabase"`
}

// DeployableServiceSpec is the deployment inventory projection used by future
// stack-aware install and deploy tooling. Nested sections stay intentionally
// open so the library does not become a domain-specific deploy schema.
type DeployableServiceSpec struct {
	Name                                 string         `yaml:"name"`
	Status                               string         `yaml:"status"`
	Zone                                 string         `yaml:"zone"`
	Path                                 string         `yaml:"path"`
	Dockerfile                           string         `yaml:"dockerfile"`
	ImageEnv                             string         `yaml:"imageEnv"`
	InternalImageRepositoryEnv           string         `yaml:"internalImageRepositoryEnv"`
	MigrationsImageEnv                   string         `yaml:"migrationsImageEnv"`
	MigrationsInternalImageRepositoryEnv string         `yaml:"migrationsInternalImageRepositoryEnv"`
	OwnerDomain                          string         `yaml:"ownerDomain"`
	Dependencies                         []string       `yaml:"dependencies"`
	APIContracts                         map[string]any `yaml:"apiContracts"`
	Database                             map[string]any `yaml:"database"`
	Implementation                       map[string]any `yaml:"implementation"`
	Deploy                               DeploySpec     `yaml:"deploy"`
}

// DeploySpec is the subset of deploy metadata needed by stack render and
// dry-run tooling.
type DeploySpec struct {
	ServiceManifest    string     `yaml:"serviceManifest"`
	MigrationsManifest string     `yaml:"migrationsManifest"`
	Kustomization      string     `yaml:"kustomization"`
	Health             HealthSpec `yaml:"health"`
}

// HealthSpec records service probe endpoints published by deploy inventory.
type HealthSpec struct {
	Livez   string `yaml:"livez"`
	Readyz  string `yaml:"readyz"`
	Metrics string `yaml:"metrics"`
	OpenAPI string `yaml:"openapi"`
	MCP     string `yaml:"mcp"`
}

// PlatformDataStoreSpec is the platform storage projection needed by deploy
// dry-runs without introducing service-domain deploy policy.
type PlatformDataStoreSpec struct {
	Name               string `yaml:"name"`
	Status             string `yaml:"status"`
	Owner              string `yaml:"owner"`
	DatabaseName       string `yaml:"databaseName"`
	Migrations         string `yaml:"migrations"`
	Dockerfile         string `yaml:"dockerfile"`
	MigrationsImageEnv string `yaml:"migrationsImageEnv"`
	ClientLibrary      string `yaml:"clientLibrary"`
	Purpose            string `yaml:"purpose"`
}

var legacyVersionIndexPattern = regexp.MustCompile(`index\s+\.Versions\s+"([^"]+)"`)

// LoadFile loads the root services.yaml stack inventory.
func LoadFile(path string) (Stack, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Stack{}, fmt.Errorf("read services file %s: %w", path, err)
	}
	stack, err := Parse(data)
	if err != nil {
		return Stack{}, fmt.Errorf("parse services file %s: %w", path, err)
	}
	return stack, nil
}

// LoadOptional loads the stack inventory when a path exists. An empty path or
// absent file returns an empty Stack so low-level render tests can run without
// a repository inventory.
func LoadOptional(path string) (Stack, error) {
	if strings.TrimSpace(path) == "" {
		return Stack{}, nil
	}
	stack, err := LoadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Stack{}, nil
	}
	return stack, err
}

// Parse decodes root services.yaml content.
func Parse(data []byte) (Stack, error) {
	var stack Stack
	if err := yaml.Unmarshal(data, &stack); err != nil {
		return Stack{}, err
	}
	return stack, nil
}

// Version resolves a named stack version.
func (stack Stack) Version(name string) (string, error) {
	value := stack.Spec.Versions[name].Value
	if value == "" {
		return "", fmt.Errorf("version %q is not defined in services.yaml", name)
	}
	return value, nil
}

// ImageOr resolves an image through an explicit env override first.
func (stack Stack) ImageOr(name string, envName string) (string, error) {
	if value := os.Getenv(envName); value != "" {
		return value, nil
	}
	return stack.Image(name)
}

// Image resolves an image from the stack inventory, honoring its imageEnv
// override before rendering the inventory template.
func (stack Stack) Image(name string) (string, error) {
	spec, ok := stack.Spec.Images[name]
	if !ok {
		return "", fmt.Errorf("image %q is not defined in services.yaml", name)
	}
	if spec.ImageEnv != "" {
		if value := os.Getenv(spec.ImageEnv); value != "" {
			return value, nil
		}
	}
	switch {
	case spec.From != "":
		return stack.renderInventoryTemplate(name, "from", spec.From)
	case spec.Repository != "" && spec.TagTemplate != "":
		repository, err := stack.renderInventoryTemplate(name, "repository", spec.Repository)
		if err != nil {
			return "", err
		}
		tag, err := stack.renderInventoryTemplate(name, "tagTemplate", spec.TagTemplate)
		if err != nil {
			return "", err
		}
		return repository + ":" + tag, nil
	default:
		return "", fmt.Errorf("image %q has no supported source in services.yaml", name)
	}
}

// VersionValues returns the legacy template context projection used only to
// validate old index .Versions template expressions while templates migrate to
// the typed version helper.
func (stack Stack) VersionValues() map[string]string {
	values := make(map[string]string, len(stack.Spec.Versions))
	for key, version := range stack.Spec.Versions {
		values[key] = version.Value
	}
	return values
}

// TemplateFuncs returns safe helpers for manifest templates.
func (stack Stack) TemplateFuncs() template.FuncMap {
	return template.FuncMap{
		"envOr":   EnvOr,
		"env":     os.Getenv,
		"image":   stack.Image,
		"imageOr": stack.ImageOr,
		"version": stack.Version,
	}
}

// EnvOr returns an env value or fallback without logging either value.
func EnvOr(name string, fallback string) string {
	value := os.Getenv(name)
	if value == "" {
		return fallback
	}
	return value
}

func (stack Stack) renderInventoryTemplate(imageName, fieldName, source string) (string, error) {
	if err := stack.validateLegacyVersionIndexes(imageName, fieldName, source); err != nil {
		return "", err
	}
	tpl, err := template.New(imageName + "." + fieldName).Funcs(template.FuncMap{
		"envOr":   EnvOr,
		"env":     os.Getenv,
		"version": stack.Version,
	}).Parse(source)
	if err != nil {
		return "", fmt.Errorf("parse services.yaml image %s %s template: %w", imageName, fieldName, err)
	}
	var output bytes.Buffer
	if err := tpl.Execute(&output, struct {
		Versions map[string]string
	}{
		Versions: stack.VersionValues(),
	}); err != nil {
		return "", fmt.Errorf("render services.yaml image %s %s template: %w", imageName, fieldName, err)
	}
	return output.String(), nil
}

func (stack Stack) validateLegacyVersionIndexes(imageName, fieldName, source string) error {
	for _, match := range legacyVersionIndexPattern.FindAllStringSubmatch(source, -1) {
		if len(match) < 2 {
			continue
		}
		if _, err := stack.Version(match[1]); err != nil {
			return fmt.Errorf("validate services.yaml image %s %s template: %w", imageName, fieldName, err)
		}
	}
	return nil
}
