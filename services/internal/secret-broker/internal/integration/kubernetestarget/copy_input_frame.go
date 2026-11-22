package kubernetestarget

import "github.com/codex-k8s/kodex/libs/go/managedkubernetes"

// sendFileInput writes one owner-admitted bounded FILE body followed by its optional v5 stdin close.
// The write lock joins both actions; partial body or uncertain close never permits a resend or ACK.
// Ordinary interactive stdin retains its existing separate-empty-EOF grammar.
// sendFileInput пишет один owner-admitted bounded FILE body и optional v5 stdin close.
// Write lock объединяет оба действия; partial body либо uncertain close не допускают resend или ACK.
// Обычный interactive stdin сохраняет separate-empty-EOF grammar.
func (stream *nativeExecWebSocket) sendFileInput(payload []byte, end bool) error {
	stream.writeMu.Lock()
	defer stream.writeMu.Unlock()
	if stream.operation != "kubernetes.cp.upload" || !stream.options.Stdin || stream.inputClosed || len(payload) > managedkubernetes.MaximumFrameBytes || len(payload) == 0 && !end || !stream.spend(len(payload)) {
		return errNativeStreamUnknown
	}
	if len(payload) > 0 {
		if err := stream.writeFrame(append([]byte{0}, payload...)); err != nil {
			return err
		}
	}
	if end {
		stream.inputClosed = true
		return stream.writeFrame([]byte{255, 0})
	}
	return nil
}
