package kubernetestarget

import "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/types/value"

// ReadStream transfers native output and explicit exit evidence without promoting a transport EOF.
// Partial bytes remain present alongside errors so the upper stream can retain them before closure.
// ReadStream передаёт native output и explicit exit evidence без превращения transport EOF в success.
// Partial bytes остаются вместе с errors для сохранения верхним stream до closure.
func (stream *nativeExecWebSocket) ReadStream() (value.KubernetesStreamRead, error) {
	channel, payload, exit, err := stream.ReadOutput()
	if stream.operation == "kubernetes.cp.download" && channel == "STDOUT" {
		channel = "FILE_CONTENT"
	}
	return value.KubernetesStreamRead{Channel: channel, Payload: payload, ExitCode: exit}, err
}
