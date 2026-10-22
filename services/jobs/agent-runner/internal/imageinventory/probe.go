// Package imageinventory observes a closed tool registry within the build wrapper's final-rootfs boundary.
// Пакет imageinventory наблюдает закрытый реестр инструментов в границе финального rootfs build wrapper.
package imageinventory

import (
	"bytes"
	"context"
	"debug/buildinfo"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"runtime/debug"
	"strings"
	"syscall"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

// Mode selects the privileged image-inventory entrypoint used by the build wrapper; it is distinct from per-tool sandbox execution.
// Mode выбирает привилегированную точку входа инвентаризации образа для build wrapper отдельно от изолированного выполнения отдельной пробы.
const Mode = "image-tool-inventory"

// outputPath fixes the externally collected inventory output path, created exclusively outside the observed /image tree.
// outputPath задаёт фиксированный путь собираемого извне результата инвентаризации, исключительно создаваемого вне наблюдаемого дерева /image.
const outputPath = "/tmp/kodex-tool-inventory.json"

// invalid is the fixed public probe failure, omitting image contents, arguments and raw tool output.
// invalid задаёт фиксированный публичный отказ пробы без содержимого образа, аргументов и исходного вывода инструмента.
var invalid = errors.New("image tool inventory probe failed")

// versionPattern extracts a bounded numeric version token with an optional prefix/suffix; matching alone does not establish executable readiness or provenance.
// versionPattern извлекает ограниченный числовой токен версии с необязательными префиксом и суффиксом; совпадение само не доказывает готовность исполняемого файла или происхождение.
var versionPattern = regexp.MustCompile(`(?:^|[^0-9])v?([0-9]+\.[0-9]+(?:\.[0-9]+){0,2}(?:[-+][A-Za-z0-9.-]{1,48})?)`)

// Run requires root, the inventory mode and five wrapper arguments, validates manifest pins, then observes the closed probe registry within 150 seconds.
// Reads /image and writes a new external inventory file; wrapper mount/network isolation and supplied pin authority are established elsewhere.
// It reads no runtime input or provider credentials and does not admit, promote or launch an agent image.
// Run требует root, режим инвентаризации и пять аргументов wrapper, проверяет привязки манифеста, затем наблюдает закрытый реестр проб в пределах 150 секунд.
// Читает /image и записывает новый внешний файл инвентаря; изоляция монтирования и сети wrapper, полномочия переданных привязок устанавливаются отдельно.
// Не читает вход выполнения и учётные данные провайдера, не допускает, не публикует и не запускает образ агента.
func Run(ctx context.Context, args []string) error {
	if len(args) != 5 || os.Geteuid() != 0 || args[1] != Mode {
		return invalid
	}
	manifest := runtimecontract.ImageToolManifest{Schema: runtimecontract.ImageInventorySchema,
		SpecSHA256: args[2], ImmutableBuildSHA256: args[3], RuntimeContractSHA256: args[4],
		Platform: runtime.GOOS + "/" + runtime.GOARCH, Tools: []runtimecontract.ImageToolObservation{}}
	for _, probe := range runtimecontract.ImageToolProbes() {
		manifest.Tools = append(manifest.Tools, runtimecontract.ImageToolObservation{Name: probe.Name, Status: "MISSING", Required: probe.Required})
	}
	if manifest.Validate() != nil {
		return invalid
	}
	manifest.Tools = nil
	ctx, cancel := context.WithTimeout(ctx, 150*time.Second)
	defer cancel()
	root, err := os.OpenRoot("/image")
	if err != nil {
		return invalid
	}
	defer root.Close()
	// Hash и наблюдение читают только финализированный rootfs; запись идёт снаружи.
	for _, probe := range runtimecontract.ImageToolProbes() {
		if ctx.Err() != nil {
			return invalid
		}
		item := observe(ctx, root, probe)
		manifest.Tools = append(manifest.Tools, item)
	}
	if manifest.Validate() != nil {
		return invalid
	}
	raw, err := json.Marshal(manifest)
	if err != nil {
		return invalid
	}
	file, err := os.OpenFile(outputPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o444)
	if err != nil {
		return invalid
	}
	_, writeErr := file.Write(raw)
	if closeErr := file.Close(); writeErr != nil || closeErr != nil {
		return invalid
	}
	return nil
}

// observe tries registered paths for a readable regular executable through 256 MiB, hashes its observed bytes and runs the bounded version probe.
// The first eligible path returns VERIFIED or PROBE_FAILED; absent/unreadable candidates remain MISSING, and only two tools allow exact build-info fallback.
// Execution-path identity relies on the wrapper filesystem layout; this observation is not admission or signature verification.
// observe перебирает зарегистрированные пути до доступного обычного исполняемого файла размером до 256 МиБ, хеширует наблюдённые байты и запускает ограниченную пробу версии.
// Первый подходящий путь возвращает VERIFIED либо PROBE_FAILED; отсутствующие и нечитаемые кандидаты остаются MISSING, только два инструмента допускают точный резервный путь через build-info.
// Идентичность пути выполнения зависит от файловой структуры wrapper; наблюдение не является допуском или проверкой подписи.
func observe(ctx context.Context, root *os.Root, probe runtimecontract.ImageToolProbe) runtimecontract.ImageToolObservation {
	item := runtimecontract.ImageToolObservation{Name: probe.Name, Status: "MISSING", Required: probe.Required}
	for _, path := range probe.Paths {
		file, err := root.Open(strings.TrimPrefix(path, "/"))
		if err != nil {
			continue
		}
		info, err := file.Stat()
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 || info.Size() < 1 || info.Size() > 256<<20 {
			_ = file.Close()
			continue
		}
		raw, err := io.ReadAll(io.LimitReader(file, (256<<20)+1))
		_ = file.Close()
		if err != nil || len(raw) > 256<<20 {
			continue
		}
		item.Path, item.SHA256, item.Status = path, runtimecontract.ImageInventorySHA256(raw), "PROBE_FAILED"
		clear(raw)
		if version, ok := runProbe(ctx, path, probe.Args); ok {
			item.Version, item.Status = version, "VERIFIED"
		} else if probe.Name == "goimports" || probe.Name == "grpcurl" {
			file, err := root.Open(strings.TrimPrefix(path, "/"))
			if err == nil {
				info, readErr := buildinfo.Read(file)
				_ = file.Close()
				readinessOutput, ready := runProbeReadiness(ctx, path, probe.Args)
				clear(readinessOutput)
				if readErr == nil {
					if version, ok := buildInfoProbeVersion(probe.Name, info, ready); ok {
						item.Version, item.Status = version, "VERIFIED"
					}
				}
			}
		}
		return item
	}
	return item
}

// buildInfoProbeVersion accepts only goimports/grpcurl metadata matching their exact main module/command, a canonical version and a successful independent readiness probe.
// Replacement modules, development banners and metadata without successful execution never fabricate a version.
// buildInfoProbeVersion принимает только метаданные goimports и grpcurl с точным основным модулем и командой, канонической версией и успешной отдельной пробой готовности.
// Подменённые модули, баннеры разработки и метаданные без успешного выполнения не создают выдуманную версию.
func buildInfoProbeVersion(name string, info *debug.BuildInfo, ready bool) (string, bool) {
	if !ready || info == nil || info.Main.Replace != nil {
		return "", false
	}
	module, command := "", ""
	switch name {
	case "goimports":
		module, command = "golang.org/x/tools", "golang.org/x/tools/cmd/goimports"
	case "grpcurl":
		module, command = "github.com/fullstorydev/grpcurl", "github.com/fullstorydev/grpcurl/cmd/grpcurl"
	default:
		return "", false
	}
	if info.Main.Path != module || info.Path != command || !strings.HasPrefix(info.Main.Version, "v") {
		return "", false
	}
	match := versionPattern.FindStringSubmatch(" " + info.Main.Version)
	if len(match) != 2 || match[0] != " "+info.Main.Version || len(match[1]) > 80 {
		return "", false
	}
	return match[1], true
}

// boundedOutput captures at most 4096 combined output bytes and remembers overflow without backpressuring the subprocess; it is not a log sink.
// boundedOutput сохраняет не более 4096 байт объединённого вывода и запоминает переполнение без блокирования дочернего процесса; хранилищем логов не является.
type boundedOutput struct {
	bytes.Buffer
	overflow bool
}

// Write keeps only the remaining capture budget while returning the original byte count and nil; callers must reject the sticky overflow flag.
// Write сохраняет только оставшийся бюджет записи, возвращая исходное число байтов и nil; вызывающий обязан отклонить сохранённый признак переполнения.
func (output *boundedOutput) Write(raw []byte) (int, error) {
	count := len(raw)
	remaining := 4096 - output.Len()
	if len(raw) > remaining {
		output.overflow = true
		raw = raw[:remaining]
	}
	_, _ = output.Buffer.Write(raw)
	return count, nil
}

// runProbe requires successful bounded readiness output before extracting one numeric version; clears the captured bytes on return.
// runProbe требует успешный ограниченный вывод пробы готовности до извлечения одного числового токена версии и очищает записанные байты при возврате.
func runProbe(ctx context.Context, path string, args []string) (string, bool) {
	raw, ok := runProbeReadiness(ctx, path, args)
	defer clear(raw)
	if !ok {
		return "", false
	}
	match := versionPattern.FindSubmatch(raw)
	if len(match) != 2 {
		return "", false
	}
	return string(match[1]), true
}

// runProbeReadiness reexecutes the current runner in closed probe mode as UID/GID 10001 with a fixed environment and a two-second child deadline.
// Captures bounded stdout/stderr, kills the process group after completion/cancellation and rejects execution or output overflow failures.
// Returned bytes belong to the caller for projection and clearing; actual sandbox setup is performed by the child entrypoint.
// runProbeReadiness повторно запускает текущий runner в закрытом режиме пробы с UID и GID 10001, фиксированным окружением и двухсекундным сроком дочернего процесса.
// Ограниченно записывает stdout и stderr, завершает группу процессов после выполнения либо отмены, отклоняет ошибку выполнения и переполнение вывода.
// Возвращаемые байты принадлежат вызывающему для проекции и очистки; саму изоляцию устанавливает дочерняя точка входа.
func runProbeReadiness(ctx context.Context, path string, args []string) ([]byte, bool) {
	call, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	runner, err := os.Executable()
	if err != nil {
		return nil, false
	}
	command := exec.CommandContext(call, runner, append([]string{SandboxExecMode, path}, args...)...)
	command.Dir = "/"
	command.Env = []string{"PATH=/usr/local/go/bin:/usr/local/bin:/usr/bin:/bin", "HOME=/nonexistent", "LANG=C", "LC_ALL=C", "NO_COLOR=1", "COREPACK_ENABLE_NETWORK=0", "GOTOOLCHAIN=local", "GOROOT=/usr/local/go", "GOENV=off"}
	command.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: 10001, Gid: 10001}, Setpgid: true}
	command.Cancel = func() error { return syscall.Kill(-command.Process.Pid, syscall.SIGKILL) }
	command.WaitDelay = 100 * time.Millisecond
	var output boundedOutput
	command.Stdout = &output
	command.Stderr = &output
	err = command.Run()
	if command.Process != nil {
		_ = syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
	}
	if err != nil || output.overflow {
		clear(output.Bytes())
		return nil, false
	}
	return output.Bytes(), true
}
