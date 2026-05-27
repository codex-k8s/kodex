package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"text/template"

	"github.com/codex-k8s/kodex/libs/go/stackinventory"
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
	stack, err := stackinventory.LoadOptional(options.ServicesFilePath)
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

func renderDir(sourceDir, outputDir string, stack stackinventory.Stack) error {
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

func renderFile(sourcePath, outputPath string, mode os.FileMode, stack stackinventory.Stack) error {
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return fmt.Errorf("create output directory for %s: %w", outputPath, err)
	}
	if !strings.HasSuffix(sourcePath, ".tpl") {
		return copyFile(sourcePath, outputPath, mode)
	}
	tpl, err := template.New(filepath.Base(sourcePath)).Funcs(stack.TemplateFuncs()).ParseFiles(sourcePath)
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
