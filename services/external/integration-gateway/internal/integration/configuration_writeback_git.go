package integration

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	// writeBackGitBinary pins the executable path instead of resolving a caller-controlled command through an inherited PATH.
	// RU: writeBackGitBinary закрепляет путь исполняемого файла вместо поиска управляемой вызывающим команды через унаследованный PATH.
	writeBackGitBinary = "/usr/bin/git"
	// maximumGitOutput bounds captured Git stdout; exceeding it rejects the operation rather than returning a truncated successful result.
	// RU: maximumGitOutput ограничивает захваченный Git stdout; превышение отклоняет операцию вместо усечённого успешного результата.
	maximumGitOutput = 8 << 20
	// writeBackAskPass returns a fixed username and reads the private credential file only when Git requests a password; the token is not an argument or environment value.
	// RU: writeBackAskPass возвращает фиксированное имя и читает приватный credential-файл только по запросу пароля Git; токен не входит в аргументы либо значения env.
	writeBackAskPass = "#!/bin/sh\ncase \"$1\" in\n*Username*) printf '%s' \"$KODEX_GIT_USERNAME\";;\n*Password*) cat -- \"$KODEX_GIT_CREDENTIAL_FILE\";;\n*) exit 1;;\nesac\n"
)

// errWriteBackGit is the safe sentinel for local Git and malformed evidence failures, without including stderr, remote credentials or arbitrary output.
// RU: errWriteBackGit — безопасная sentinel-ошибка локального Git и неверных доказательств без stderr, remote credentials и произвольного вывода.
var errWriteBackGit = errors.New("configuration writeback Git operation failed")

// sourceContentDigest hashes exact configuration bytes with SHA-256 without text, newline or format normalization.
// RU: sourceContentDigest хеширует точные байты конфигурации SHA-256 без нормализации текста, переводов строк либо формата.
func sourceContentDigest(content []byte) string {
	digest := sha256.Sum256(content)
	return hex.EncodeToString(digest[:])
}

// checkWriteBackRuntime checks executable-file availability and temporary-directory creation/removal, not provider connectivity or complete Git operation readiness.
// RU: checkWriteBackRuntime проверяет доступность исполняемого файла и создание/удаление временного каталога, а не связь с провайдером либо полную готовность Git-операций.
func checkWriteBackRuntime() error {
	info, err := os.Stat(writeBackGitBinary)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 {
		return errWriteBackGit
	}
	directory, err := os.MkdirTemp("", "kodex-writeback-readiness-")
	if err != nil {
		return errWriteBackGit
	}
	if os.Remove(directory) != nil {
		return errWriteBackGit
	}
	return nil
}

// writeBackGitWorkspace owns one disposable bare repository, selected HTTPS remote/proxy and explicit subprocess environment with credential-file indirection.
// RU: writeBackGitWorkspace владеет одним временным bare-репозиторием, выбранными HTTPS remote/proxy и явным окружением процесса со ссылкой на credential-файл.
type writeBackGitWorkspace struct {
	directory, remote, proxy string
	environment              []string
}

// boundedGitOutput captures stdout only up to maximumGitOutput; its caller clears captured bytes when a Git operation fails.
// RU: boundedGitOutput захватывает stdout только до maximumGitOutput; вызывающий очищает захваченные байты при ошибке Git-операции.
type boundedGitOutput struct{ bytes.Buffer }

// Write rejects a chunk that would exceed the remaining budget without appending part of it or reporting successful truncation.
// RU: Write отклоняет блок сверх оставшегося бюджета без частичного добавления и без сообщения об успешном усечении.
func (output *boundedGitOutput) Write(value []byte) (int, error) {
	if len(value) > maximumGitOutput-output.Len() {
		return 0, errWriteBackGit
	}
	return output.Buffer.Write(value)
}

// newWriteBackGitWorkspace creates a private directory, credential/askpass files and a bare repository with a fixed author/time and non-inherited Git configuration.
// RU: newWriteBackGitWorkspace создаёт приватный каталог, credential/askpass файлы и bare-репозиторий с заданными author/time и без унаследованной Git-конфигурации.
// Empty or multiline credentials and multiline author fields are rejected; partial startup attempts workspace cleanup, and no remote fetch or push occurs here.
// RU: Пустые либо многострочные credentials и многострочные author-поля отклоняются; частичный startup пытается очистить workspace, remote fetch и push здесь не выполняются.
// Remote and proxy selection belongs to the validated execution caller, not this filesystem constructor.
// RU: Remote и proxy выбирает проверенный вызывающий объект исполнения, а не этот файловый конструктор.
func newWriteBackGitWorkspace(ctx context.Context, remote, proxy string, credential []byte, authorName, authorEmail string, created time.Time) (*writeBackGitWorkspace, error) {
	if len(credential) == 0 || bytes.ContainsAny(credential, "\x00\r\n") || strings.ContainsAny(authorName+authorEmail, "\x00\r\n") {
		return nil, errWriteBackGit
	}
	directory, err := os.MkdirTemp("", "kodex-configuration-writeback-")
	if err != nil {
		return nil, errWriteBackGit
	}
	workspace := &writeBackGitWorkspace{directory: directory, remote: remote, proxy: proxy}
	credentialPath, askpassPath := filepath.Join(directory, "credential"), filepath.Join(directory, "askpass")
	if os.WriteFile(credentialPath, credential, 0600) != nil || os.WriteFile(askpassPath, []byte(writeBackAskPass), 0700) != nil {
		workspace.close()
		return nil, errWriteBackGit
	}
	date := strconv.FormatInt(created.Unix(), 10) + " +0000"
	username := "oauth2"
	if strings.HasPrefix(remote, "https://github.com/") {
		username = "x-access-token"
	}
	workspace.environment = []string{"PATH=/usr/bin:/bin", "LANG=C", "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_TERMINAL_PROMPT=0", "GIT_ASKPASS=" + askpassPath, "KODEX_GIT_CREDENTIAL_FILE=" + credentialPath,
		"KODEX_GIT_USERNAME=" + username, "GIT_AUTHOR_NAME=" + authorName, "GIT_COMMITTER_NAME=" + authorName, "GIT_AUTHOR_EMAIL=" + authorEmail, "GIT_COMMITTER_EMAIL=" + authorEmail, "GIT_AUTHOR_DATE=" + date, "GIT_COMMITTER_DATE=" + date}
	if _, err = workspace.run(ctx, nil, "init", "--bare", "--quiet", "."); err != nil {
		workspace.close()
		return nil, err
	}
	if _, err = workspace.run(ctx, nil, "remote", "add", "origin", remote); err != nil {
		workspace.close()
		return nil, err
	}
	return workspace, nil
}

// close best-effort removes the exact private workspace tree, ignoring removal errors; it does not promise secure erasure or remove caller repositories.
// RU: close пытается удалить точное приватное дерево workspace, игнорируя ошибки; безопасное стирание и удаление репозиториев вызывающего не обещаются.
func (workspace *writeBackGitWorkspace) close() { _ = os.RemoveAll(workspace.directory) }

// run executes fixed Git arguments in the isolated workspace with HTTPS-only verified TLS, no redirects/hooks/credential helpers and the selected proxy.
// RU: run выполняет фиксированные Git-аргументы в изолированном workspace с проверенным HTTPS TLS, без redirects/hooks/credential helpers и с выбранным proxy.
// Stdout is bounded and stderr discarded; cancellation kills the process group with a one-second wait delay, exposing only closed operation-stage diagnostics.
// RU: Stdout ограничен, stderr отбрасывается; отмена убивает process group с секундным wait delay, раскрывая только закрытую диагностику этапа операции.
func (workspace *writeBackGitWorkspace) run(ctx context.Context, input []byte, arguments ...string) ([]byte, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	options := []string{"-c", "credential.helper=", "-c", "core.hooksPath=/dev/null", "-c", "core.attributesFile=/dev/null", "-c", "protocol.allow=never", "-c", "protocol.https.allow=always", "-c", "http.followRedirects=false", "-c", "http.sslVerify=true", "-c", "http.sslVersion=tlsv1.3", "-c", "http.proxy=" + workspace.proxy, "-c", "fetch.fsckObjects=true", "-c", "transfer.fsckObjects=true"}
	process := exec.CommandContext(ctx, writeBackGitBinary, append(options, arguments...)...)
	process.Dir, process.Env = workspace.directory, workspace.environment
	process.Stdin = bytes.NewReader(input)
	process.Stderr = io.Discard
	process.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	process.Cancel = func() error {
		if process.Process == nil {
			return os.ErrProcessDone
		}
		err := syscall.Kill(-process.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	process.WaitDelay = time.Second
	var output boundedGitOutput
	process.Stdout = &output
	if process.Run() != nil {
		clear(output.Bytes())
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		stage := "other"
		if len(arguments) > 0 {
			switch arguments[0] {
			case "init", "remote", "fetch", "ls-remote", "ls-tree", "cat-file", "read-tree", "hash-object", "update-index", "write-tree", "commit-tree", "push":
				stage = arguments[0]
			}
		}
		return nil, fmt.Errorf("%w: %s", errWriteBackGit, stage)
	}
	return output.Bytes(), nil
}

// refs reads only the selected source/proposal heads, rejects unexpected or duplicate refs and requires the source while preserving an absent proposal as empty.
// RU: refs читает только выбранные source/proposal heads, отклоняет неожиданные либо повторные refs и требует source, сохраняя отсутствие proposal пустой строкой.
func (workspace *writeBackGitWorkspace) refs(ctx context.Context, source, proposal string) (string, string, error) {
	raw, err := workspace.run(ctx, nil, "ls-remote", "--heads", "origin", "refs/heads/"+source, "refs/heads/"+proposal)
	if err != nil {
		return "", "", err
	}
	defer clear(raw)
	var sourceSHA, proposalSHA string
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		if line == "" {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) != 2 || !sourceCommitPattern.MatchString(fields[0]) {
			return "", "", errWriteBackGit
		}
		switch fields[1] {
		case "refs/heads/" + source:
			if sourceSHA != "" {
				return "", "", errWriteBackGit
			}
			sourceSHA = fields[0]
		case "refs/heads/" + proposal:
			if proposalSHA != "" {
				return "", "", errWriteBackGit
			}
			proposalSHA = fields[0]
		default:
			return "", "", errWriteBackGit
		}
	}
	if sourceSHA == "" {
		return "", "", errWriteBackGit
	}
	return sourceSHA, proposalSHA, nil
}

// writeBackGitCandidate records exact local object identities of a one-file change, including the original regular-file blob needed for owner evidence.
// RU: writeBackGitCandidate хранит точные локальные object ID изменения одного файла, включая исходный blob обычного файла для owner-доказательств.
type writeBackGitCandidate struct{ Commit, Tree, Blob, BaseBlob string }

// candidate shallow-fetches the pinned base, requires an existing regular-file blob with the expected content digest and replaces only that index entry.
// RU: candidate неглубоко загружает закреплённую базу, требует существующий regular-file blob с ожидаемым хешем и заменяет только эту запись индекса.
// Git plumbing writes the new blob/tree and a single-parent commit using pinned metadata; every returned object ID is checked and no remote ref is changed.
// RU: Git plumbing записывает новые blob/tree и commit с одним родителем и закреплёнными метаданными; каждый object ID проверяется, remote ref не меняется.
func (workspace *writeBackGitWorkspace) candidate(ctx context.Context, base, path, baseDigest, message string, content []byte) (writeBackGitCandidate, error) {
	var result writeBackGitCandidate
	if _, err := workspace.run(ctx, nil, "fetch", "--quiet", "--depth=1", "--filter=blob:none", "--no-tags", "--no-recurse-submodules", "origin", base); err != nil {
		return result, err
	}
	raw, err := workspace.run(ctx, nil, "ls-tree", "-z", base, "--", path)
	if err != nil {
		return result, err
	}
	entry := strings.TrimSuffix(string(raw), "\x00")
	clear(raw)
	parts := strings.SplitN(entry, "\t", 2)
	if len(parts) != 2 || parts[1] != path {
		return result, errWriteBackGit
	}
	metadata := strings.Fields(parts[0])
	if len(metadata) != 3 || (metadata[0] != "100644" && metadata[0] != "100755") || metadata[1] != "blob" || !sourceCommitPattern.MatchString(metadata[2]) {
		return result, errWriteBackGit
	}
	result.BaseBlob = metadata[2]
	raw, err = workspace.run(ctx, nil, "cat-file", "blob", result.BaseBlob)
	if err != nil {
		return result, err
	}
	valid := len(raw) > 0 && len(raw) <= maximumSourceContentBytes && sourceContentDigest(raw) == baseDigest
	clear(raw)
	if !valid {
		return result, errWriteBackGit
	}
	if _, err = workspace.run(ctx, nil, "read-tree", base); err != nil {
		return result, err
	}
	raw, err = workspace.run(ctx, content, "hash-object", "-w", "--stdin")
	if err != nil {
		return result, err
	}
	result.Blob = strings.TrimSpace(string(raw))
	clear(raw)
	if !sourceCommitPattern.MatchString(result.Blob) {
		return result, errWriteBackGit
	}
	if _, err = workspace.run(ctx, nil, "update-index", "--cacheinfo", fmt.Sprintf("%s,%s,%s", metadata[0], result.Blob, path)); err != nil {
		return result, err
	}
	raw, err = workspace.run(ctx, nil, "write-tree")
	if err != nil {
		return result, err
	}
	result.Tree = strings.TrimSpace(string(raw))
	clear(raw)
	if !sourceCommitPattern.MatchString(result.Tree) {
		return result, errWriteBackGit
	}
	raw, err = workspace.run(ctx, []byte(message+"\n"), "commit-tree", result.Tree, "-p", base)
	if err != nil {
		return result, err
	}
	result.Commit = strings.TrimSpace(string(raw))
	clear(raw)
	if !sourceCommitPattern.MatchString(result.Commit) {
		return result, errWriteBackGit
	}
	return result, nil
}

// push targets only the proposal branch with an explicit empty-expect lease, requiring that ref not to exist; the source ref is absent from the refspec.
// RU: push направлен только в proposal-ветку с явным empty-expect lease, требующим отсутствия ref; source ref не входит в refspec.
// The lease is not an atomic check that the source branch still equals the previously read base, and this helper performs no retry.
// RU: Lease не является атомарной проверкой неизменности исходной базы; helper не выполняет повтор.
func (workspace *writeBackGitWorkspace) push(ctx context.Context, branch, commit string) error {
	_, err := workspace.run(ctx, nil, "push", "--porcelain", "--no-verify", "--force-with-lease=refs/heads/"+branch+":", "origin", commit+":refs/heads/"+branch)
	return err
}
