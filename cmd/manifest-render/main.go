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
)

func main() {
	sourcePath := flag.String("source", "", "source file or directory with *.tpl manifest templates")
	outputPath := flag.String("output", "", "output file or directory")
	envFilePath := flag.String("env-file", "", "optional env file with KEY=value values")
	flag.Parse()

	if err := run(*sourcePath, *outputPath, *envFilePath); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "manifest-render: %v\n", err)
		os.Exit(1)
	}
}

func run(sourcePath, outputPath, envFilePath string) error {
	if strings.TrimSpace(sourcePath) == "" {
		return errors.New("--source is required")
	}
	if strings.TrimSpace(outputPath) == "" {
		return errors.New("--output is required")
	}
	if strings.TrimSpace(envFilePath) != "" {
		if err := loadEnvFile(envFilePath); err != nil {
			return err
		}
	}
	sourceInfo, err := os.Stat(sourcePath)
	if err != nil {
		return fmt.Errorf("stat source: %w", err)
	}
	if sourceInfo.IsDir() {
		return renderDir(sourcePath, outputPath)
	}
	return renderFile(sourcePath, outputPath, sourceInfo.Mode())
}

func renderDir(sourceDir, outputDir string) error {
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
		return renderFile(path, targetPath, info.Mode())
	})
}

func renderFile(sourcePath, outputPath string, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return fmt.Errorf("create output directory for %s: %w", outputPath, err)
	}
	if !strings.HasSuffix(sourcePath, ".tpl") {
		return copyFile(sourcePath, outputPath, mode)
	}
	tpl, err := template.New(filepath.Base(sourcePath)).Funcs(template.FuncMap{
		"envOr": envOr,
		"env":   os.Getenv,
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
