package platform

import (
	"fmt"
	"math"
	"strings"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

// promptStructuredVariables builds input/session/run/workflow/gate/project file descriptors and
// exact runtime image/tool metadata from supplied artifacts. It validates descriptors, scope and
// tool text, derives canonical input paths and exposes workspace guidance without reading files or
// granting capabilities. Workflow files mirror run files only when workflowRef is nonempty.
// promptStructuredVariables собирает файловые дескрипторы input/session/run/workflow/gate/project
// и точные метаданные runtime-образа и инструментов из переданных артефактов. Проверяет
// дескрипторы, область и тексты инструментов, выводит канонические пути input и инструкции
// workspace без чтения файлов и выдачи capabilities. Workflow получает файлы запуска только при
// непустом workflowRef.
func promptStructuredVariables(
	artifacts []map[string]any,
	tools []runtimecontract.RuntimeEnvironmentTool,
	image runtimecontract.RuntimeEnvironmentImage,
	environmentRef, inputAttachmentSetRef, workflowRef string,
) (map[string]any, error) {
	inputFiles := make([]any, 0)
	sessionFiles := make([]any, 0)
	runFiles := make([]any, 0, len(artifacts))
	workflowFiles := make([]any, 0, len(artifacts))
	gateFiles := make([]any, 0)
	projectFiles := make([]any, 0)
	for _, item := range artifacts {
		artifact, descriptor, err := promptArtifactDescriptor(item)
		if err != nil {
			return nil, err
		}
		runFiles = append(runFiles, descriptor)
		if workflowRef != "" {
			workflowFiles = append(workflowFiles, descriptor)
		}
		switch artifact.Scope {
		case runtimecontract.AttachmentScopeInput:
			inputFiles = append(inputFiles, descriptor)
			sessionFiles = append(sessionFiles, descriptor)
			if strings.Contains(strings.ToUpper(artifact.AttachmentPurpose), "GATE") {
				gateFiles = append(gateFiles, descriptor)
			}
		case runtimecontract.AttachmentScopeSession:
			sessionFiles = append(sessionFiles, descriptor)
		case runtimecontract.AttachmentScopeKnowledge:
			projectFiles = append(projectFiles, descriptor)
		default:
			return nil, fmt.Errorf("prompt artifact scope is invalid")
		}
	}
	inputManifestPath := "/workspace/input/manifest.json"
	inputFilesDir := "/workspace/input"
	if inputAttachmentSetRef != "" && len(inputFiles) != 0 {
		firstPath := inputFiles[0].(map[string]any)["path"].(string)
		prefix := "/workspace/input/" + inputAttachmentSetRef
		if !strings.HasPrefix(firstPath, prefix+"/files/") {
			return nil, fmt.Errorf("prompt input workspace path is invalid")
		}
		inputFilesDir = prefix + "/files"
		inputManifestPath = prefix + "/manifest.json"
	}
	toolDescriptors := make([]any, 0, len(tools))
	for _, tool := range tools {
		if strings.TrimSpace(tool.Name) == "" || strings.TrimSpace(tool.Description) == "" {
			return nil, fmt.Errorf("prompt runtime tool descriptor is invalid")
		}
		toolDescriptors = append(toolDescriptors, map[string]any{"name": tool.Name, "description": tool.Description})
	}
	inputScope := promptFileScope(inputFiles, inputFilesDir, inputManifestPath)
	inputScope["workspace"] = map[string]any{
		"working_directory":            runtimecontract.RuntimeWorkspaceWorkingRoot,
		"editable_input_directory":     runtimecontract.RuntimeWorkspaceWorkingRoot + strings.TrimPrefix(inputFilesDir, runtimecontract.RuntimeWorkspaceRoot),
		"original_input_directory":     inputFilesDir,
		"original_input_manifest":      inputManifestPath,
		"readonly_knowledge_directory": runtimecontract.RuntimeWorkspaceRoot + "/knowledge",
		"readonly_context_directory":   runtimecontract.RuntimeContextRoot,
		"guide_path":                   runtimecontract.RuntimeWorkspaceWorkingRoot + "/WORKSPACE.md",
		"guidance":                     promptWorkspaceGuidance + promptWorkspaceSourceTreeGuidance,
	}
	return map[string]any{
		"input":    inputScope,
		"session":  promptFileScope(sessionFiles, "/workspace/input", "/workspace/input/manifest.json"),
		"run":      promptFileScope(runFiles, "/workspace", "/workspace/input/manifest.json"),
		"workflow": promptFileScope(workflowFiles, "/workspace", "/workspace/input/manifest.json"),
		"gate":     promptFileScope(gateFiles, "/workspace/input", "/workspace/input/manifest.json"),
		"project":  promptFileScope(projectFiles, "/workspace/knowledge", "/workspace/input/manifest.json"),
		"runtime": map[string]any{"environment": map[string]any{
			"ref":   environmentRef,
			"image": map[string]any{"reference": image.Reference, "digest": image.Digest},
			"tools": toolDescriptors,
		}},
	}, nil
}

// promptWorkspaceGuidance contains runtime instructions separating verified mounted originals,
// editable INPUT working copies and independently authorized result discovery. It requires exact
// provenance and full mandatory reads, forbids replay after denial and grants no repository,
// credential, publication or external-effect authority. This text is guidance, not enforcement.
// promptWorkspaceGuidance содержит runtime-инструкции, разделяющие проверенные смонтированные
// оригиналы, редактируемые рабочие копии INPUT и независимо разрешённый поиск результатов. Требует
// точное происхождение и полное чтение обязательного содержимого, запрещает повтор после отказа и
// не выдаёт прав на репозиторий, учётные данные, публикацию или внешние действия. Это инструкция,
// а не механизм принудительной проверки.
const promptWorkspaceGuidance = "Use the isolated writable working directory for local edits and scratch files. Read WORKSPACE.md at guide_path and inspect the available directory tree to locate supplied materials by file name; do not require the user to supply file IDs. For already mounted attachments, prefer ordinary local file reads through an available authorized local tool: match the original path, revision, size and digest against the current FILES descriptors and manifest, verify the original bytes, and read required content to EOF. Do not perform remote file discovery or read_file solely to read the same mounted original. Edit only its working_copy_path or your own files in the working directory; never treat a modified working copy as the pinned original. Remote file tools are for authorized files not already materialized locally, including missing predecessor results. This local-first rule is not an alternate transport after a remote denial or failure; never use it to replay or bypass a rejected read. The guide is a navigation aid, not an authority source. Local manifests describe mounted attachments, not the complete RUN_RESULT catalog. A missing handoff tuple or local file alone does not prove a required result unavailable. When own RUN_RESULT tools are available, finish independent authorized discovery using own get_file_manifest and all next_cursor pages with the same purpose; match current root/run/dependency provenance and exact artifact revision/digest, then use the own entry_ref. A filename hit alone does not establish origin. Read matched mandatory content to EOF; actual absence after discovery or remote denial stays blocked, with no replay of the denied read or invented pins. Only INPUT attachments have editable working copies at working_copy_path. Original input files, manifests, SESSION/context attachments and project knowledge remain read-only; their digests describe the originals, not modified working copies. Do not edit internal .kodex state. Local workspace access grants no artifact publication, repository, credential, network or external-effect permission. Use an existing authorized repository checkout in the working directory, or create one there only when the assigned repository and Git operation are already authorized by the current runtime. Do not invent a repository, token or network access. Resolve applicable document IDs and exact pinned paths from current authorized sources before reading mandatory documents."

// promptWorkspaceSourceTreeGuidance adds runtime source-tree navigation rules: inspect an
// authorized index, read relevant exact files fully and preserve explicit full-read requirements.
// It distinguishes integrity/EOF verification from substantive reading and does not require
// dumping unrelated repository contents.
// promptWorkspaceSourceTreeGuidance добавляет runtime-правила навигации по исходникам: изучить
// разрешённый индекс, полностью прочитать точные относящиеся к задаче файлы и сохранить явные
// требования полного чтения. Различает проверку целостности и EOF с содержательным чтением и не
// требует вывода всего постороннего содержимого репозитория.
const promptWorkspaceSourceTreeGuidance = " For a supplied source tree or source bundle, inspect its authorized directory listing or index first, locate the files relevant to the current task, and read those exact full files. Do not dump the entire repository into the model context or invent a mandatory semantic read of every unrelated file. Verify full bundle bytes and provenance separately from studying file contents: a checksum or EOF scan alone does not prove that required content was read and understood. A task explicitly requiring a full source or artifact read still requires that exact read; do not waive its gate. Authorized local reading to EOF does not require calling remote read_file again or printing every byte into a tool response. Keep exact source/file version, digest and read evidence; distinguish integrity verification from substantive reading."

// promptArtifactDescriptor converts a supplied JSON-shaped artifact map into a runner artifact and
// prompt descriptor with a canonical workspace path. Numeric metadata must pass the float64
// integer converter and required refs/digest/media type/revision/version checks; only INPUT
// exposes a derived working-copy path. It performs no content verification, manifest lookup or
// authorization.
// promptArtifactDescriptor преобразует переданную карту артефакта формы JSON в runner-артефакт и
// дескриптор с каноническим путём workspace. Числовые поля проходят преобразование целого float64,
// обязательные ссылки, дайджест, тип, ревизия и версия проверяются; только INPUT получает путь
// рабочей копии. Проверка содержимого, поиск манифеста и авторизация не выполняются.
func promptArtifactDescriptor(item map[string]any) (runtimecontract.RunnerInputArtifact, map[string]any, error) {
	artifact := runtimecontract.RunnerInputArtifact{
		Ref: stringMapValue(item, "ref"), FileName: stringMapValue(item, "fileName"),
		MediaType: stringMapValue(item, "mediaType"), Digest: stringMapValue(item, "digest"),
		Source: stringMapValue(item, "source"), Scope: stringMapValue(item, "scope"),
		AttachmentSetRef:  stringMapValue(item, "attachmentSetRef"),
		AttachmentPurpose: stringMapValue(item, "attachmentPurpose"), Provenance: stringMapValue(item, "provenance"),
	}
	var ok bool
	if artifact.SizeBytes, ok = int64MapValue(item, "sizeBytes"); !ok {
		return runtimecontract.RunnerInputArtifact{}, nil, fmt.Errorf("prompt artifact size is invalid")
	}
	if artifact.Revision, ok = int64MapValue(item, "revision"); !ok {
		return runtimecontract.RunnerInputArtifact{}, nil, fmt.Errorf("prompt artifact revision is invalid")
	}
	if artifact.Version, ok = int64MapValue(item, "version"); !ok {
		return runtimecontract.RunnerInputArtifact{}, nil, fmt.Errorf("prompt artifact version is invalid")
	}
	if artifact.Position, ok = int64MapValue(item, "position"); !ok {
		return runtimecontract.RunnerInputArtifact{}, nil, fmt.Errorf("prompt artifact position is invalid")
	}
	path, err := runtimecontract.ArtifactWorkspacePath(artifact.AttachmentSetRef, artifact)
	if err != nil || artifact.Ref == "" || artifact.Digest == "" || artifact.MediaType == "" || artifact.Revision < 1 || artifact.Version < 1 {
		return runtimecontract.RunnerInputArtifact{}, nil, fmt.Errorf("prompt artifact descriptor is invalid")
	}
	descriptor := map[string]any{
		"artifact_ref": artifact.Ref, "revision_ref": fmt.Sprintf("%s@%d", artifact.Ref, artifact.Revision),
		"name": artifact.FileName, "media_type": artifact.MediaType, "size": artifact.SizeBytes,
		"sha256": artifact.Digest, "path": path, "source": artifact.Source,
		"version": artifact.Version, "purpose": artifact.AttachmentPurpose,
		"original_read_only": true,
	}
	if artifact.Scope == runtimecontract.AttachmentScopeInput {
		descriptor["working_copy_path"] = runtimecontract.RuntimeWorkspaceWorkingRoot + strings.TrimPrefix(path, runtimecontract.RuntimeWorkspaceRoot)
	}
	return artifact, descriptor, nil
}

// promptFileScope packages a supplied file slice, its length, directory and manifest path into a
// new map. File entries remain shared and paths are not validated here.
// promptFileScope упаковывает переданный срез файлов, его длину, каталог и путь манифеста в новую
// карту. Файловые записи остаются общими, пути здесь не проверяются.
func promptFileScope(files []any, directory, manifestPath string) map[string]any {
	return map[string]any{"files": files, "files_count": len(files), "files_dir": directory, "manifest_path": manifestPath}
}

// stringMapValue returns a string-typed map entry, or the empty string for absent or differently
// typed values. It performs no trimming, parsing or validation.
// stringMapValue возвращает строковое значение карты либо пустую строку при отсутствии или другом
// типе. Не удаляет пробелы, не разбирает и не проверяет значение.
func stringMapValue(values map[string]any, key string) string {
	value, _ := values[key].(string)
	return value
}

// int64MapValue accepts only float64 map entries, rejects NaN, infinity, fractional values and
// values outside the float64 comparisons with int64 bounds, then converts to int64. Other Go
// numeric types are rejected; this is a JSON-number conversion, not a general numeric parser.
// int64MapValue принимает только float64 из карты, отклоняет NaN, бесконечность, дробные значения
// и значения вне сравниваемых во float64 границ int64, затем преобразует в int64. Другие числовые
// типы Go отклоняются; это преобразование JSON-числа, а не универсальный числовой разбор.
func int64MapValue(values map[string]any, key string) (int64, bool) {
	value, ok := values[key].(float64)
	if !ok || math.IsNaN(value) || math.IsInf(value, 0) || value != math.Trunc(value) || value < math.MinInt64 || value > math.MaxInt64 {
		return 0, false
	}
	return int64(value), true
}
