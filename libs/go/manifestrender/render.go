package manifestrender

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/codex-k8s/kodex/libs/go/stackinventory"
)

// Options describes one manifest render operation.
type Options struct {
	SourcePath       string
	OutputPath       string
	EnvFilePath      string
	ServicesFilePath string
}

// Render renders one file or directory of *.tpl manifest templates.
func Render(options Options) error {
	if strings.TrimSpace(options.SourcePath) == "" {
		return errors.New("--source is required")
	}
	if strings.TrimSpace(options.OutputPath) == "" {
		return errors.New("--output is required")
	}
	if strings.TrimSpace(options.EnvFilePath) != "" {
		if err := stackinventory.LoadEnvFile(options.EnvFilePath); err != nil {
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

// PrepareOutputRoot returns a safe render output root and cleanup function.
// Caller-provided paths are created only when absent and must be empty. The
// function only removes temporary directories it creates itself.
func PrepareOutputRoot(renderDir string, tempPattern string) (string, func(), error) {
	if strings.TrimSpace(tempPattern) == "" {
		tempPattern = "kodex-manifest-render-*"
	}
	if strings.TrimSpace(renderDir) == "" {
		tempDir, err := os.MkdirTemp("", tempPattern)
		if err != nil {
			return "", func() {}, fmt.Errorf("create manifest render dir: %w", err)
		}
		return tempDir, func() { _ = os.RemoveAll(tempDir) }, nil
	}

	if info, err := os.Stat(renderDir); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return "", func() {}, fmt.Errorf("stat render dir: %w", err)
		}
		if err := os.MkdirAll(renderDir, 0o755); err != nil {
			return "", func() {}, fmt.Errorf("create render dir: %w", err)
		}
	} else if !info.IsDir() {
		return "", func() {}, errors.New("render dir must be a directory")
	}

	entries, err := os.ReadDir(renderDir)
	if err != nil {
		return "", func() {}, fmt.Errorf("read render dir: %w", err)
	}
	if len(entries) > 0 {
		return "", func() {}, errors.New("render dir must be empty; manifest render will not remove caller-provided paths")
	}
	return renderDir, func() {}, nil
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
