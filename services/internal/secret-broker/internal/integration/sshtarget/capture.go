package sshtarget

import "sync"

// outputCapture bounds stdout and stderr together and closes the connection on overflow rather than draining unlimited remote bytes.
// outputCapture ограничивает stdout и stderr совместно и закрывает соединение при переполнении вместо чтения неограниченного удалённого потока.
type outputCapture struct {
	mutex     sync.Mutex
	stdout    []byte
	stderr    []byte
	maximum   int
	truncated bool
	close     func() error
}

// captureWriter selects one stream of the shared bounded capture; it carries no authority or remote command metadata.
// captureWriter выбирает один stream общего bounded capture; он не несёт authority либо remote command metadata.
type captureWriter struct {
	capture *outputCapture
	stderr  bool
}

// Write stores at most the shared budget and terminates the wire on overflow; the post-dispatch caller consequently preserves UNKNOWN.
// Write сохраняет не больше общего бюджета и прекращает wire при overflow; caller после dispatch вследствие этого сохраняет UNKNOWN.
func (writer captureWriter) Write(raw []byte) (int, error) {
	capture := writer.capture
	capture.mutex.Lock()
	defer capture.mutex.Unlock()
	remaining := capture.maximum - len(capture.stdout) - len(capture.stderr)
	accepted := len(raw)
	if accepted > remaining {
		accepted = remaining
	}
	if writer.stderr {
		capture.stderr = append(capture.stderr, raw[:accepted]...)
	} else {
		capture.stdout = append(capture.stdout, raw[:accepted]...)
	}
	if accepted != len(raw) {
		capture.truncated = true
		_ = capture.close()
		return accepted, ErrOutputLimit
	}
	return accepted, nil
}

// result copies capture state after all SSH stream readers have joined; callers never receive a buffer still owned by a writer.
// result копирует capture state после join всех SSH stream readers; callers не получают buffer, которым ещё владеет writer.
func (capture *outputCapture) result() Result {
	capture.mutex.Lock()
	defer capture.mutex.Unlock()
	return Result{Stdout: append([]byte(nil), capture.stdout...), Stderr: append([]byte(nil), capture.stderr...), Truncated: capture.truncated}
}
