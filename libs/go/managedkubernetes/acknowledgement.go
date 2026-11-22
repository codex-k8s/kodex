package managedkubernetes

import "github.com/google/uuid"

// AcknowledgementRejection retains bounded private evidence of a completed invalid successful ACK.
// It contains no response bytes or usable locator. An optional observed UID is only a witness;
// it never authorizes reading, replacing or deleting an object and cannot repair the rejected ACK.
// AcknowledgementRejection хранит bounded private evidence завершённого неверного успешного ACK.
// Не содержит response bytes либо usable locator. Optional observed UID — лишь witness;
// он не разрешает read/replace/delete объекта и не исправляет rejected ACK.
type AcknowledgementRejection struct {
	ResponseSHA256 string `json:"-"`
	Truncated      bool   `json:"-"`
	ObservedUID    string `json:"-"`
}

// Valid rejects incomplete hashes and noncanonical optional UID witnesses before owner persistence.
// Valid отклоняет incomplete hashes и noncanonical optional UID witnesses до owner persistence.
func (receipt AcknowledgementRejection) Valid() bool {
	if !ValidDigest(receipt.ResponseSHA256) {
		return false
	}
	if receipt.ObservedUID == "" {
		return true
	}
	id, err := uuid.Parse(receipt.ObservedUID)
	return !receipt.Truncated && err == nil && id != uuid.Nil && id.String() == receipt.ObservedUID
}

// String prevents witness hashes or object identities from escaping through diagnostics.
// String предотвращает раскрытие witness hashes либо object identities в диагностике.
func (AcknowledgementRejection) String() string {
	return "[Kubernetes rejected acknowledgement witness]"
}

// GoString preserves redaction for detailed Go formatting of the private witness.
// GoString сохраняет redaction при подробном Go formatting private witness.
func (receipt AcknowledgementRejection) GoString() string { return receipt.String() }
