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

const maximumRenderBytes = 2 << 20

const rendererPathEnvironment = "PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"

var phases = []string{"claim", "scan", "sign", "admit", "promote"}

type Rendered struct {
	PVC *corev1.PersistentVolumeClaim
	Job *batchv1.Job
}

type Renderer interface {
	Render(context.Context, *corev1.ConfigMap, string, string, string) (Rendered, error)
}

type ScriptRenderer struct {
	path string
}

func NewScriptRenderer(path string) (*ScriptRenderer, error) {
	if !filepathIsCanonicalAbsolute(path) {
		return nil, errors.New("image admission renderer path is invalid")
	}
	return &ScriptRenderer{path: path}, nil
}

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

type boundedBuffer struct {
	bytes.Buffer
	maximum int
}

func (buffer *boundedBuffer) Write(input []byte) (int, error) {
	if len(input) > buffer.maximum-buffer.Len() {
		return 0, errors.New("bounded image admission output exceeded")
	}
	return buffer.Buffer.Write(input)
}
