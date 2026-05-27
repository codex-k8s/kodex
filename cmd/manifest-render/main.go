package main

import (
	"bufio"
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"text/template"

	"gopkg.in/yaml.v3"
)

func main() {
	sourcePath := flag.String("source", "", "source file or directory with *.tpl manifest templates")
	outputPath := flag.String("output", "", "output file or directory")
	envFilePath := flag.String("env-file", "", "optional env file with KEY=value values")
	servicesFilePath := flag.String("services-file", "services.yaml", "optional services.yaml inventory for versions and image resolution")
	flag.Parse()

	if err := runWithOptions(renderOptions{
		SourcePath:       *sourcePath,
		OutputPath:       *outputPath,
		EnvFilePath:      *envFilePath,
		ServicesFilePath: *servicesFilePath,
	}); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "manifest-render: %v\n", err)
		os.Exit(1)
	}
}

func run(sourcePath, outputPath, envFilePath string) error {
	return runWithOptions(renderOptions{
		SourcePath:  sourcePath,
		OutputPath:  outputPath,
		EnvFilePath: envFilePath,
	})
}

type renderOptions struct {
	SourcePath       string
	OutputPath       string
	EnvFilePath      string
	ServicesFilePath string
}

func runWithOptions(options renderOptions) error {
	if strings.TrimSpace(options.SourcePath) == "" {
		return errors.New("--source is required")
	}
	if strings.TrimSpace(options.OutputPath) == "" {
		return errors.New("--output is required")
	}
	if strings.TrimSpace(options.EnvFilePath) != "" {
		if err := loadEnvFile(options.EnvFilePath); err != nil {
			return err
		}
	}
	stack, err := loadServiceStack(options.ServicesFilePath)
	if err != nil {
		return err
	}
	sourceInfo, err := os.Stat(options.SourcePath)
	if err != nil {
		return fmt.Errorf("stat source: %w", err)
	}
	if sourceInfo.IsDir() {
		return renderDir(options.SourcePath, options.OutputPath, stack)
	}
	return renderFile(options.SourcePath, options.OutputPath, sourceInfo.Mode(), stack)
}

func renderDir(sourceDir, outputDir string, stack serviceStack) error {
	return filepath.WalkDir(sourceDir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relPath, err := filepath.Rel(sourceDir, path)
		if err != nil {
			return fmt.Errorf("resolve relative path for %s: %w", path, err)
		}
		targetPath := filepath.Join(outputDir, strings.TrimSuffix(relPath, ".tpl"))
		if entry.IsDir() {
			return os.MkdirAll(targetPath, 0o755)
		}
		info, err := entry.Info()
		if err != nil {
			return fmt.Errorf("stat %s: %w", path, err)
		}
		return renderFile(path, targetPath, info.Mode(), stack)
	})
}

func renderFile(sourcePath, outputPath string, mode os.FileMode, stack serviceStack) error {
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return fmt.Errorf("create output directory for %s: %w", outputPath, err)
	}
	if !strings.HasSuffix(sourcePath, ".tpl") {
		return copyFile(sourcePath, outputPath, mode)
	}
	tpl, err := template.New(filepath.Base(sourcePath)).Funcs(template.FuncMap{
		"envOr":   envOr,
		"env":     os.Getenv,
		"image":   stack.image,
		"imageOr": stack.imageOr,
		"version": stack.version,
	}).ParseFiles(sourcePath)
	if err != nil {
		return fmt.Errorf("parse template %s: %w", sourcePath, err)
	}
	output, err := os.OpenFile(outputPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode.Perm())
	if err != nil {
		return fmt.Errorf("open output %s: %w", outputPath, err)
	}
	defer func() { _ = output.Close() }()
	if err := tpl.Execute(output, nil); err != nil {
		return fmt.Errorf("execute template %s: %w", sourcePath, err)
	}
	return nil
}

func copyFile(sourcePath, outputPath string, mode os.FileMode) error {
	source, err := os.Open(sourcePath)
	if err != nil {
		return fmt.Errorf("open source %s: %w", sourcePath, err)
	}
	defer func() { _ = source.Close() }()
	output, err := os.OpenFile(outputPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode.Perm())
	if err != nil {
		return fmt.Errorf("open output %s: %w", outputPath, err)
	}
	defer func() { _ = output.Close() }()
	if _, err := io.Copy(output, source); err != nil {
		return fmt.Errorf("copy %s to %s: %w", sourcePath, outputPath, err)
	}
	return nil
}

func loadEnvFile(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open env file %s: %w", path, err)
	}
	defer func() { _ = file.Close() }()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(strings.TrimPrefix(key, "export "))
		if key == "" {
			continue
		}
		if err := os.Setenv(key, parseEnvValue(strings.TrimSpace(value))); err != nil {
			return fmt.Errorf("set env %s: %w", key, err)
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read env file %s: %w", path, err)
	}
	return nil
}

func parseEnvValue(value string) string {
	if value == "" {
		return ""
	}
	if strings.HasPrefix(value, "'") && strings.HasSuffix(value, "'") {
		unquoted := strings.TrimSuffix(strings.TrimPrefix(value, "'"), "'")
		return strings.ReplaceAll(unquoted, `'\''`, `'`)
	}
	if strings.HasPrefix(value, `"`) && strings.HasSuffix(value, `"`) {
		if unquoted, err := strconv.Unquote(value); err == nil {
			return unquoted
		}
	}
	return value
}

func envOr(name string, fallback string) string {
	value := os.Getenv(name)
	if value == "" {
		return fallback
	}
	return value
}

type serviceStack struct {
	Versions map[string]string
	Images   map[string]imageSpec
}

type servicesFile struct {
	Spec struct {
		Versions map[string]struct {
			Value string `yaml:"value"`
		} `yaml:"versions"`
		Images map[string]imageSpec `yaml:"images"`
	} `yaml:"spec"`
}

type imageSpec struct {
	From        string `yaml:"from"`
	Local       string `yaml:"local"`
	Repository  string `yaml:"repository"`
	TagTemplate string `yaml:"tagTemplate"`
	ImageEnv    string `yaml:"imageEnv"`
}

var inventoryVersionIndexPattern = regexp.MustCompile(`index\s+\.Versions\s+"([^"]+)"`)

func loadServiceStack(path string) (serviceStack, error) {
	if strings.TrimSpace(path) == "" {
		return serviceStack{}, nil
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return serviceStack{}, nil
	}
	if err != nil {
		return serviceStack{}, fmt.Errorf("read services file %s: %w", path, err)
	}
	var file servicesFile
	if err := yaml.Unmarshal(data, &file); err != nil {
		return serviceStack{}, fmt.Errorf("parse services file %s: %w", path, err)
	}
	stack := serviceStack{
		Versions: make(map[string]string, len(file.Spec.Versions)),
		Images:   file.Spec.Images,
	}
	for key, version := range file.Spec.Versions {
		stack.Versions[key] = version.Value
	}
	return stack, nil
}

func (stack serviceStack) version(name string) (string, error) {
	value := stack.Versions[name]
	if value == "" {
		return "", fmt.Errorf("version %q is not defined in services.yaml", name)
	}
	return value, nil
}

func (stack serviceStack) imageOr(name string, envName string) (string, error) {
	if value := os.Getenv(envName); value != "" {
		return value, nil
	}
	return stack.image(name)
}

func (stack serviceStack) image(name string) (string, error) {
	spec, ok := stack.Images[name]
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

func (stack serviceStack) renderInventoryTemplate(imageName, fieldName, source string) (string, error) {
	if err := stack.validateInventoryTemplateVersions(imageName, fieldName, source); err != nil {
		return "", err
	}
	tpl, err := template.New(imageName + "." + fieldName).Funcs(template.FuncMap{
		"envOr":   envOr,
		"env":     os.Getenv,
		"version": stack.version,
	}).Parse(source)
	if err != nil {
		return "", fmt.Errorf("parse services.yaml image %s %s template: %w", imageName, fieldName, err)
	}
	var output bytes.Buffer
	if err := tpl.Execute(&output, struct {
		Versions map[string]string
	}{
		Versions: stack.Versions,
	}); err != nil {
		return "", fmt.Errorf("render services.yaml image %s %s template: %w", imageName, fieldName, err)
	}
	return output.String(), nil
}

func (stack serviceStack) validateInventoryTemplateVersions(imageName, fieldName, source string) error {
	for _, match := range inventoryVersionIndexPattern.FindAllStringSubmatch(source, -1) {
		if len(match) < 2 {
			continue
		}
		if _, err := stack.version(match[1]); err != nil {
			return fmt.Errorf("validate services.yaml image %s %s template: %w", imageName, fieldName, err)
		}
	}
	return nil
}
