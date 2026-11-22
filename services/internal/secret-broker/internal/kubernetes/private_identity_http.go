package kubernetes

import (
	"io"
	"sync"
)

// privateIdentityRequestBody owns one purpose credential request buffer until either transport or caller closes it; synchronized Read/Close prevents asynchronous HTTP cleanup from racing a caller-side clear.
// privateIdentityRequestBody владеет одним purpose credential request buffer до закрытия transport либо caller; synchronized Read/Close не даёт asynchronous HTTP cleanup гоняться с caller-side clear.
type privateIdentityRequestBody struct {
	mutex  sync.Mutex
	owned  []byte
	offset int
	closed bool
}

// newPrivateIdentityRequestBody takes exclusive ownership without copying; callers must bound input beforehand, must not read or clear the same slice, and must defer Close even if HTTP Do returns an error.
// newPrivateIdentityRequestBody принимает exclusive ownership без копии; caller обязан заранее ограничить input, не читать и не очищать тот же slice и defer Close даже при ошибке HTTP Do.
func newPrivateIdentityRequestBody(owned []byte) *privateIdentityRequestBody {
	return &privateIdentityRequestBody{owned: owned}
}

// Read copies only while the body remains open and never exposes its backing array; after Close all subsequent transport reads receive EOF rather than cleared or reused private bytes.
// Read копирует только пока body открыт и не раскрывает backing array; после Close все subsequent transport reads получают EOF вместо очищенных либо reused private bytes.
func (body *privateIdentityRequestBody) Read(destination []byte) (int, error) {
	body.mutex.Lock()
	defer body.mutex.Unlock()
	if body.closed || body.offset == len(body.owned) {
		return 0, io.EOF
	}
	count := copy(destination, body.owned[body.offset:])
	body.offset += count
	return count, nil
}

// Close is idempotent across caller and transport, clears the complete owned slice under the read lock and drops its reference; it neither retries a request nor proves remote operation completion.
// Close идемпотентен между caller и transport, очищает весь owned slice под read lock и удаляет reference; request retry либо доказательство remote operation completion он не предоставляет.
func (body *privateIdentityRequestBody) Close() error {
	body.mutex.Lock()
	defer body.mutex.Unlock()
	if !body.closed {
		clear(body.owned)
		body.owned = nil
		body.offset = 0
		body.closed = true
	}
	return nil
}

// String prevents request/body diagnostics from formatting private buffer contents.
// String не даёт request/body diagnostics форматировать private buffer contents.
func (*privateIdentityRequestBody) String() string { return "[private identity request body]" }

// GoString applies the same privacy boundary to detailed Go formatting.
// GoString применяет ту же privacy boundary при подробном Go formatting.
func (body *privateIdentityRequestBody) GoString() string { return body.String() }
