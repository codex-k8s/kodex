package codex

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/model"
	workspacepolicy "github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/workspace"
	"golang.org/x/sys/unix"
)

var errWorkingDirectory = errors.New("runtime working directory is unsafe")

const workspaceGuideName = "WORKSPACE.md"

const workspaceGuide = `# Рабочие файлы агента

Текущий каталог: /workspace/work. Осмотри доступное дерево каталогов и найди
нужные материалы по именам файлов; не требуй от пользователя их идентификаторы.
Точные исходные пути, версии и хеши перечислены в FILES и manifest.

## Можно редактировать

- /workspace/work/input/<набор>/files/ — независимые рабочие копии входных файлов
  текущего задания. Изменяй копию по working_copy_path, не её оригинал.
- /workspace/work/ — собственные файлы, черновики, сборки и scratch-каталоги.
  Уже разрешённый checkout репозитория размещай здесь. Наличие каталога не
  разрешает само по себе clone, Git push, новый credential или сетевой доступ.

## Только для чтения

- /workspace/input/ — оригиналы входных файлов, история SESSION и manifest.json.
- /workspace/context/ — закреплённые инструкции и контекст текущего исполнения.
- /workspace/knowledge/ — предоставленные знания проекта.

История и знания не становятся редактируемыми входами. Хеш исходного файла
не описывает изменённую рабочую копию. Для обязательного чтения проверь exact
version/digest и прочитай требуемый источник до EOF; дерево не доказывает EOF.

## Результат и ограничения

Готовые файлы складывай в /workspace/.kodex/outbox только если публикация
артефактов разрешена текущими capabilities; форматы и ограничения смотри в FILES.
Черновики и checkout не складывай в outbox. Остальное внутреннее .kodex/state
не читай и не изменяй. Этот справочник не выдаёт полномочий или доступ к секретам.

Workspace изолирован и временный: перед следующим исполнением рабочие файлы
сбрасываются. Сохраняй результат через разрешённые артефакты или Git-операции;
локальный файл и финальное сообщение сами по себе не подтверждают публикацию.
`

func workingDirectory(input model.Input) string {
	return filepath.Join(input.WorkspaceRoot, filepath.Base(runtimecontract.RuntimeWorkspaceWorkingRoot))
}

// Broker очищает собственные рабочие файлы после join предыдущего provider.
// Оригиналы и их manifest не изменяются; копии не являются publication authority.
func prepareWorkingDirectory(ctx context.Context, input model.Input) error {
	return prepareWorkingDirectoryOwned(ctx, input, 10001)
}

func prepareWorkingDirectoryOwned(ctx context.Context, input model.Input, sourceUID uint32) error {
	if _, err := runtimecontract.BuildWorkspaceAttachmentManifest(input.AttachmentSets, input.InputArtifacts); err != nil {
		return errWorkingDirectory
	}
	if input.WorkspacePolicy.Validate() != nil {
		return errWorkingDirectory
	}
	copyBytes, copyFiles := int64(len(workspaceGuide)), int64(1)
	if copyBytes > input.WorkspacePolicy.MaximumWritableBytes || copyFiles > input.WorkspacePolicy.MaximumFileCount {
		return errWorkingDirectory
	}
	for _, artifact := range input.InputArtifacts {
		if artifact.Scope != runtimecontract.AttachmentScopeInput {
			continue
		}
		if copyBytes > input.WorkspacePolicy.MaximumWritableBytes-artifact.SizeBytes || copyFiles >= input.WorkspacePolicy.MaximumFileCount {
			return errWorkingDirectory
		}
		copyBytes += artifact.SizeBytes
		copyFiles++
	}
	lock, err := workspacepolicy.Lock(ctx, input.WorkspaceRoot)
	if err != nil {
		return errWorkingDirectory
	}
	defer lock.Close()
	fd, err := unix.Openat(int(lock.Fd()), "work", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return errWorkingDirectory
	}
	directory := os.NewFile(uintptr(fd), "runtime-work")
	defer directory.Close()
	var stat unix.Stat_t
	if unix.Fstat(fd, &stat) != nil || stat.Uid != sourceUID || stat.Mode&unix.S_IFMT != unix.S_IFDIR {
		return errWorkingDirectory
	}
	if err := prepareWorkingTree(ctx, input, directory, sourceUID); err != nil {
		return err
	}
	if lock.Close() != nil {
		return errWorkingDirectory
	}
	return workspacepolicy.RunCanary(ctx, input.WorkspaceRoot, input.WorkspacePolicy)
}

func prepareWorkingTree(ctx context.Context, input model.Input, directory *os.File, sourceUID uint32) error {
	// os.Root привязывает удаление к открытому inode и не выпускает traversal
	// наружу даже при повреждённых остатках предыдущей попытки.
	root, err := os.OpenRoot(workingDirectory(input))
	if err != nil {
		return errWorkingDirectory
	}
	defer root.Close()
	rootInfo, rootErr := root.Stat(".")
	openedInfo, openedErr := directory.Stat()
	if rootErr != nil || openedErr != nil || !os.SameFile(rootInfo, openedInfo) {
		return errWorkingDirectory
	}
	entries, err := directory.ReadDir(-1)
	if err != nil {
		return errWorkingDirectory
	}
	for _, entry := range entries {
		if ctx.Err() != nil || root.RemoveAll(entry.Name()) != nil {
			return errWorkingDirectory
		}
	}
	guide, err := root.OpenFile(workspaceGuideName, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o640)
	if err != nil {
		return errWorkingDirectory
	}
	written, writeErr := io.WriteString(guide, workspaceGuide)
	syncErr := guide.Sync()
	closeErr := guide.Close()
	if writeErr != nil || written != len(workspaceGuide) || syncErr != nil || closeErr != nil {
		return errWorkingDirectory
	}
	for _, artifact := range input.InputArtifacts {
		if artifact.Scope != runtimecontract.AttachmentScopeInput {
			continue
		}
		if ctx.Err() != nil {
			return errWorkingDirectory
		}
		canonical, err := runtimecontract.ArtifactWorkspacePath(input.AttachmentSetRef, artifact)
		if err != nil || !strings.HasPrefix(canonical, "/workspace/input/") {
			return errWorkingDirectory
		}
		relative := strings.TrimPrefix(canonical, "/workspace/")
		file, info, err := openProtectedFile(input.WorkspaceRoot, filepath.Join(input.WorkspaceRoot, filepath.FromSlash(relative)))
		if err != nil {
			return errWorkingDirectory
		}
		copyErr := copyWorkingInput(root, relative, file, info, artifact, sourceUID)
		closeErr := file.Close()
		if copyErr != nil || closeErr != nil {
			return errWorkingDirectory
		}
	}
	return nil
}

func copyWorkingInput(root *os.Root, relative string, source *os.File, before os.FileInfo, artifact runtimecontract.RunnerInputArtifact, sourceUID uint32) error {
	stat, ok := before.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != sourceUID || stat.Nlink != 1 || before.Mode().Perm() != 0o440 || before.Size() != artifact.SizeBytes {
		return errWorkingDirectory
	}
	if root.MkdirAll(filepath.Dir(relative), 0o750) != nil {
		return errWorkingDirectory
	}
	target, err := root.OpenFile(relative, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o640)
	if err != nil {
		return errWorkingDirectory
	}
	defer target.Close()
	digest := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(target, digest), io.LimitReader(source, artifact.SizeBytes+1))
	after, statErr := source.Stat()
	var afterStat *syscall.Stat_t
	if statErr == nil {
		afterStat, _ = after.Sys().(*syscall.Stat_t)
	}
	if copyErr != nil || written != artifact.SizeBytes || statErr != nil || afterStat == nil ||
		!os.SameFile(before, after) || before.Size() != after.Size() || stat.Mtim != afterStat.Mtim || stat.Ctim != afterStat.Ctim ||
		"sha256:"+hex.EncodeToString(digest.Sum(nil)) != artifact.Digest || target.Sync() != nil {
		return errWorkingDirectory
	}
	if target.Close() != nil {
		return errWorkingDirectory
	}
	return nil
}
