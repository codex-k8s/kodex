package admissioncontroller

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"slices"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/yaml"
)

// maximumRenderBytes caps accepted renderer stdout; policy JSON and stderr have
// their own smaller limits and are not implicitly covered by this output budget.
// maximumRenderBytes ограничивает принятый stdout renderer; policy JSON и stderr
// имеют отдельные меньшие лимиты и не покрываются этим бюджетом автоматически.
const maximumRenderBytes = 2 << 20

// rendererPathEnvironment is the fixed subprocess executable search path; Render
// supplies it explicitly instead of inheriting host environment or credentials.
// rendererPathEnvironment — фиксированный PATH дочернего процесса; Render задаёт
// его явно вместо наследования окружения либо реквизитов хоста.
const rendererPathEnvironment = "PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"

// phases lists the five renderable supply-chain operations checked before script
// execution; the controller's sequential admission phaseOrder is a separate list.
// phases перечисляет пять разрешённых операций перед запуском скрипта; порядок
// последовательных фаз допуска phaseOrder задаётся отдельным списком контроллера.
var phases = []string{"claim", "scan", "sign", "admit", "promote"}

// Rendered holds decoded Job/PVC definitions, not applied workloads or approval;
// claim needs both resources, while other phases may return only the Job.
// Rendered хранит разобранные определения Job/PVC, не запущенные workloads
// и не approval; claim требует оба ресурса, прочим фазам нужен только Job.
type Rendered struct {
	PVC *corev1.PersistentVolumeClaim
	Job *batchv1.Job
}

// Renderer produces phase definitions from supplied policy/run inputs; the
// controller separately validates managed-resource constraints before creation.
// Renderer создаёт определения фазы из переданных policy/run; контроллер отдельно
// проверяет ограничения managed resources перед созданием.
type Renderer interface {
	// Render materializes one phase under the caller context without implying
	// that its resources were admitted, applied or executed in Kubernetes.
	// Render материализует одну фазу с контекстом вызывающего кода, не утверждая
	// её допуск, применение либо исполнение в Kubernetes.
	Render(context.Context, *corev1.ConfigMap, string, string, string) (Rendered, error)
}

// ScriptRenderer retains the lexical path of a repository-provided executable;
// constructing it does not inspect the file, execute it or load credentials.
// ScriptRenderer хранит лексический путь исполняемого скрипта репозитория;
// создание не проверяет файл, не запускает его и не загружает реквизиты.
type ScriptRenderer struct {
	path string
}

// NewScriptRenderer requires an already-clean absolute path using the same helper
// as Config.Validate; filesystem availability and execution errors remain deferred
// to Render rather than being silently repaired during construction.
// NewScriptRenderer требует уже чистый абсолютный путь тем же helper, что Config.Validate;
// доступность файла и ошибки исполнения остаются проверками Render, а не скрыто
// исправляются при создании.
func NewScriptRenderer(path string) (*ScriptRenderer, error) {
	if !filepathIsCanonicalAbsolute(path) {
		return nil, errors.New("image admission renderer path is invalid")
	}
	return &ScriptRenderer{path: path}, nil
}

// Render checks policy presence/phase, marshals at most 64KiB of policy JSON and
// executes the script with explicit PATH/policy environment and context cancellation.
// Stdout/stderr are bounded at 2MiB/16KiB, and script failures expose only the known
// phase, not private output. Environment/run validation belongs to the script;
// successful decoding still does not constitute managed-resource authorization.
// Render проверяет наличие policy/фазу, кодирует policy JSON до 64KiB и запускает
// скрипт с явными PATH/policy и отменой по контексту. Stdout/stderr ограничены
// 2MiB/16KiB; ошибка скрипта раскрывает только известную фазу, не закрытый вывод.
// Environment/run проверяет скрипт; успешный разбор ещё не разрешает managed resource.
func (renderer *ScriptRenderer) Render(ctx context.Context, policy *corev1.ConfigMap, environment, runID, phase string) (Rendered, error) {
	if policy == nil || !slices.Contains(phases, phase) {
		return Rendered{}, errors.New("image admission render input is invalid")
	}
	rawPolicy, err := json.Marshal(policy)
	if err != nil || len(rawPolicy) == 0 || len(rawPolicy) > 64<<10 {
		return Rendered{}, errors.New("encode bounded image admission policy")
	}
	command := exec.CommandContext(ctx, renderer.path, environment, runID, phase)
	command.Env = []string{rendererPathEnvironment, "IMAGE_ADMISSION_POLICY_JSON=" + string(rawPolicy)}
	stdout, stderr := &boundedBuffer{maximum: maximumRenderBytes}, &boundedBuffer{maximum: 16 << 10}
	command.Stdout, command.Stderr = stdout, stderr
	if err := command.Run(); err != nil {
		return Rendered{}, fmt.Errorf("render image admission phase %s", phase)
	}
	return decodeRendered(stdout.Bytes(), phase)
}

// decodeRendered accepts exactly one batch/v1 Job and, only for claim, one required
// v1 PVC, skipping blank documents and rejecting other kinds/duplicates. Ordinary
// JSON decoding is not strict field validation; input byte bounds and phase validity
// are enforced by Render, while managed-resource eligibility remains a later check.
// decodeRendered принимает ровно один batch/v1 Job и обязательный v1 PVC лишь
// для claim, пропуская пустые документы и отклоняя другие kinds/дубли ресурсов.
// Обычный JSON decoder не является strict field validation; размер и фазу
// проверяет Render, а допустимость managed resource — последующая проверка.
func decodeRendered(raw []byte, phase string) (Rendered, error) {
	decoder := yaml.NewYAMLOrJSONDecoder(bytes.NewReader(raw), 64<<10)
	result := Rendered{}
	for {
		var document json.RawMessage
		if err := decoder.Decode(&document); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			return Rendered{}, errors.New("decode image admission render")
		}
		if len(bytes.TrimSpace(document)) == 0 {
			continue
		}
		var meta metav1.TypeMeta
		if json.Unmarshal(document, &meta) != nil {
			return Rendered{}, errors.New("decode image admission resource kind")
		}
		switch {
		case meta.APIVersion == "v1" && meta.Kind == "PersistentVolumeClaim" && phase == "claim" && result.PVC == nil:
			result.PVC = &corev1.PersistentVolumeClaim{}
			if json.Unmarshal(document, result.PVC) != nil {
				return Rendered{}, errors.New("decode image admission workspace")
			}
		case meta.APIVersion == "batch/v1" && meta.Kind == "Job" && result.Job == nil:
			result.Job = &batchv1.Job{}
			if json.Unmarshal(document, result.Job) != nil {
				return Rendered{}, errors.New("decode image admission job")
			}
		default:
			return Rendered{}, errors.New("unexpected image admission rendered resource")
		}
	}
	if result.Job == nil || phase == "claim" && result.PVC == nil || phase != "claim" && result.PVC != nil {
		return Rendered{}, errors.New("image admission render is incomplete")
	}
	return result, nil
}

// boundedBuffer collects subprocess output up to its explicit maximum without
// retaining an oversized chunk; separate instances isolate stdout and stderr budgets.
// boundedBuffer собирает вывод процесса до явного maximum, не сохраняя превышающий
// chunk; разные экземпляры разделяют бюджеты stdout и stderr.
type boundedBuffer struct {
	bytes.Buffer
	maximum int
}

// Write rejects a chunk exceeding the remaining budget with zero bytes written;
// otherwise it delegates to bytes.Buffer without truncation or partial acceptance.
// Write отклоняет chunk сверх остатка бюджета с нулём записанных байтов; иначе
// передаёт его bytes.Buffer без усечения либо частичного принятия.
func (buffer *boundedBuffer) Write(input []byte) (int, error) {
	if len(input) > buffer.maximum-buffer.Len() {
		return 0, errors.New("bounded image admission output exceeded")
	}
	return buffer.Buffer.Write(input)
}
