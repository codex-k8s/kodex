package value

// ResourceOperationResult contains only bounded provider facts after one registered resource
// request. SafeJSON is sanitized JSON, at most 64 KiB; it never contains a credential document.
// Attempted distinguishes a dispatched wire request from a local refusal, not a proven effect.
// UID/resourceVersion are observed acknowledgement metadata, not authority or a retry permit.
// The owner must freshly admit every returned READ byte, including partial/error results.
// ResourceOperationResult содержит только bounded provider facts после одного registered resource
// request. SafeJSON — очищенный JSON до 64 KiB, никогда не credential document.
// Attempted отличает отправленный wire request от локального отказа, но не доказывает effect.
// UID/resourceVersion — наблюдённые acknowledgement metadata, не authority либо retry permit.
// Owner обязан свежим допуском проверить каждый выдаваемый READ byte, включая partial/error results.
type ResourceOperationResult struct {
	Outcome         string
	SafeJSON        []byte
	ReasonCode      string
	Attempted       bool
	ObservedUID     string
	ResourceVersion string
}
