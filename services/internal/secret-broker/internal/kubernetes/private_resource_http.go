package kubernetes

import "io"

// NewPrivateResourceRequestBody transfers exclusive ownership of an already bounded provider
// request buffer to the existing synchronized private-body primitive. The caller must not read,
// clear or reuse the slice and must defer Close even when HTTP Do fails. Closing joins concurrent
// body reads and clears owned bytes; it neither erases immutable copies nor proves remote failure.
// NewPrivateResourceRequestBody передаёт exclusive ownership заранее ограниченного provider
// request buffer существующему synchronized private-body primitive. Caller не читает, не очищает
// и не переиспользует slice и обязан defer Close даже при ошибке HTTP Do. Close синхронизируется
// с body reads и очищает owned bytes, но не стирает immutable copies и не доказывает remote failure.
func NewPrivateResourceRequestBody(owned []byte) io.ReadCloser {
	return newPrivateIdentityRequestBody(owned)
}
