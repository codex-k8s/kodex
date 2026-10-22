package codex

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
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

// errWorkingDirectory is the fixed unsafe-workspace sentinel, intentionally excluding raw paths/errors.
// errWorkingDirectory — фиксированный unsafe-workspace sentinel без сырых paths/errors.
var errWorkingDirectory = errors.New("runtime working directory is unsafe")

// workspaceGuideName names the platform-recreated navigation document inside the writable work tree.
// workspaceGuideName задаёт имя заново создаваемого платформой справочника в writable work tree.
const workspaceGuideName = "WORKSPACE.md"

// workspaceGuide explains the canonical runtime tree, editable copies, read-only originals,
// lookup/publication boundaries and ephemeral lifetime; prose does not enforce filesystem permissions.
// workspaceGuide объясняет каноническое runtime tree, изменяемые копии, read-only originals,
// границы поиска/публикации и временный lifecycle; текст не обеспечивает filesystem permissions.
const workspaceGuide = `# Рабочие файлы агента

Текущий каталог: /workspace/work. Осмотри доступное дерево каталогов и найди
нужные материалы по именам файлов; не требуй от пользователя их идентификаторы.
Точные исходные пути, версии и хеши перечислены в FILES и manifest.

## Карта каталогов

    /workspace/
      work/                 рабочая область, можно менять
        WORKSPACE.md        этот справочник
        input/              редактируемые копии входных файлов
      input/                оригиналы и история, только чтение
      context/              закреплённый контекст, только чтение
      knowledge/            знания проекта, только чтение
      .kodex/outbox/        только готовые разрешённые результаты

Каталоги могут быть пустыми: дерево показывает доступные материалы, а не
выдаёт дополнительные права. Не ищи содержимое задания во внутреннем state.

## Как начать

1. Прочитай этот справочник и осмотри дерево рабочей области и read-only данных.
2. Найди названные в задании файлы по именам и текущему manifest; при совпадении
   имён различай источник и версию. Для уже доступного локального файла не
   вызывай remote file tools только ради повторного чтения тех же байтов.
3. Читай оригинал, меняй рабочую копию. Если файла ещё нет локально, используй
   только разрешённый каталог или repository; отсутствие файла не разрешает
   обход отказа доступа и не повод требовать внутренний ID от пользователя.
4. Сохрани готовый результат разрешённым способом до окончания исполнения.

Для исходников сначала используй дерево или индекс, затем читай полные файлы,
которые нужны текущей задаче. Не выгружай весь репозиторий в контекст модели.
Проверка байтов всего пакета и изучение содержимого отдельных файлов — разные
доказательства: checksum/EOF scan не заменяет обязательное чтение. Явное
требование задания прочитать источник целиком сохраняется. Разрешённое
локальное чтение до EOF не требует повторного remote read_file или вывода
каждого байта в ответ инструмента.

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

// materializedWorkspaceGuide appends the issued writable budgets and fixed artifact size limit
// to navigation prose without validating or changing the immutable runtime policy.
// materializedWorkspaceGuide добавляет выданные writable budgets и фиксированный artifact size limit
// к справочнику без проверки или изменения immutable runtime policy.
func materializedWorkspaceGuide(input model.Input) string {
	return workspaceGuide + fmt.Sprintf(`
## Выданные ограничения файловой системы

Текущий общий бюджет writable workspace: %d байт и %d записей
(файлы и каталоги, включая checkout, .git, рабочие копии и кэши).
Обычные файлы и каталоги разрешены; symlink, hardlink, socket и FIFO
не принимаются при завершении. Не обходи эти ограничения и не меняй policy.
Установка зависимостей может создать ссылки или исчерпать бюджет; учитывай
это до установки и сборки. Перед финальным ответом проверь metadata своих
рабочих файлов, не следуя ссылкам и не читая внутренний state. Удалять можно
только собственные временные результаты; входы, историю и чужие файлы сохраняй.

В outbox публикуй только готовые regular files размером от 1 до %d байт.
Не оставляй там пустые логи, checkout, кэши или ссылки. Не создавай служебные
result.md и workspace-write-result.json: их назначает платформа.
Сохрани substantive результат разрешённым способом до очистки своих scratch
файлов; Git commit/push и фактическая публикация — разные подтверждения.
`, input.WorkspacePolicy.MaximumWritableBytes, input.WorkspacePolicy.MaximumFileCount, maximumPublishedArtifactBytes)
}

// workingDirectory joins the supplied root with the canonical working-root basename;
// it computes a path only and does not validate, create or authorize that directory.
// workingDirectory соединяет переданный root с basename канонического working root;
// она лишь вычисляет путь, не проверяя, не создавая и не разрешая доступ к каталогу.
func workingDirectory(input model.Input) string {
	return filepath.Join(input.WorkspaceRoot, filepath.Base(runtimecontract.RuntimeWorkspaceWorkingRoot))
}

// prepareWorkingDirectory uses fixed source UID 10001 to reset the broker's working tree after the
// caller has joined the previous provider. Originals remain read-only sources; copies are not publication grants.
// prepareWorkingDirectory использует фиксированный source UID 10001 для сброса рабочего дерева broker
// после выполненного caller join прежнего provider. Оригиналы остаются read-only; копии не дают publication grant.
func prepareWorkingDirectory(ctx context.Context, input model.Input) error {
	return prepareWorkingDirectoryOwned(ctx, input, 10001)
}

// prepareWorkingDirectoryOwned checks attachment/policy metadata and guide/input preflight quotas,
// locks and opens the expected owned work directory, rebuilds it, releases the lock, then runs the canary.
// sourceUID is a trusted internal/test parameter. Full writable accounting belongs to the canary;
// failures can leave a partially reset tree and do not roll back earlier filesystem effects.
// prepareWorkingDirectoryOwned проверяет attachment/policy metadata и preflight квоты guide/input,
// блокирует и открывает work ожидаемого owner, пересоздаёт его, снимает lock и запускает canary.
// sourceUID — доверенный internal/test parameter. Полный writable accounting принадлежит canary;
// при отказе дерево может быть сброшено частично, без отката прежних filesystem effects.
func prepareWorkingDirectoryOwned(ctx context.Context, input model.Input, sourceUID uint32) error {
	if _, err := runtimecontract.BuildWorkspaceAttachmentManifest(input.AttachmentSets, input.InputArtifacts); err != nil {
		return errWorkingDirectory
	}
	if input.WorkspacePolicy.Validate() != nil {
		return errWorkingDirectory
	}
	copyBytes, copyFiles := int64(len(materializedWorkspaceGuide(input))), int64(1)
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

// prepareWorkingTree requires a held workspace lock and an expected directory descriptor. It compares
// an os.Root with that inode, clears its children, fsyncs a fresh guide and copies only INPUT artifacts.
// Root-relative operations constrain path traversal, not mounts; cancellation checks occur between
// steps and failures are not transactional. Session history/knowledge are never copied as editable input.
// prepareWorkingTree требует workspace lock и descriptor ожидаемого каталога. Она сравнивает
// os.Root с этим inode, удаляет дочерние записи, синхронизирует новый guide и копирует только INPUT artifacts.
// Root-relative operations ограничивают path traversal, не mounts; отмена проверяется между
// шагами, отказ не транзакционен. История сессии и знания не копируются как изменяемый input.
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
	guideText := materializedWorkspaceGuide(input)
	written, writeErr := io.WriteString(guide, guideText)
	syncErr := guide.Sync()
	closeErr := guide.Close()
	if writeErr != nil || written != len(guideText) || syncErr != nil || closeErr != nil {
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

// copyWorkingInput expects a validated regular source opened at offset zero and a trusted INPUT
// descriptor/path. It checks owner/link/mode/size, exclusively creates a root-relative writable copy,
// hashes at most size+1 bytes, rechecks source identity/times and syncs/closes the target. Failure may
// leave a partial target; matching bytes do not create an artifact publication or new capability.
// copyWorkingInput требует проверенный regular source со смещением ноль и доверенные INPUT
// descriptor/path. Она проверяет owner/link/mode/size, исключительным образом создаёт root-relative
// writable copy, хеширует не более size+1 байтов, сверяет source identity/times и sync/close target.
// При отказе target может остаться частичным; совпадение байтов не создаёт публикацию или capability.
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
