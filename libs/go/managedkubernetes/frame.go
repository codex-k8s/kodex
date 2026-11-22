package managedkubernetes

const (
	// MaximumFrameBytes is the protocol ceiling; each owner stream may impose a lower bound.
	// MaximumFrameBytes — protocol ceiling; каждый owner stream может установить меньший предел.
	MaximumFrameBytes = 32 << 10
	// MaximumStreamBytes bounds total protocol credit independently of per-frame limits.
	// MaximumStreamBytes ограничивает total protocol credit независимо от per-frame limits.
	MaximumStreamBytes = 256 << 20
	// MaximumOutputDeliveries bounds fresh-authorized deliveries of the same retained output frame.
	// INPUT never uses this replay credit; its uncertain attempt remains non-replayable.
	// MaximumOutputDeliveries ограничивает fresh-authorized доставки того же retained output frame.
	// INPUT не использует этот replay credit; uncertain attempt не повторяется.
	MaximumOutputDeliveries = 4
)

// ValidChannel recognizes only registered names; the immutable stream grant narrows them further.
// ValidChannel распознаёт только registered names; immutable stream grant дополнительно сужает их.
func ValidChannel(channel string) bool {
	switch channel {
	case "CONTROL", "STDIN", "STDOUT", "STDERR", "TERMINAL_RESIZE", "PORT_FORWARD", "FILE_CONTENT":
		return true
	default:
		return false
	}
}

// MaximumFrameSequence is the largest sequence representable by the owner's PostgreSQL bigint.
// It is a storage representation boundary, not a lifetime count budget; byte/deadline quotas remain authoritative.
// MaximumFrameSequence — наибольшая sequence, представимая PostgreSQL bigint владельца.
// Это граница представления storage, не lifetime count budget; действуют byte/deadline quotas.
const MaximumFrameSequence uint64 = 1<<63 - 1

// ValidFrameSequence prevents zero and unsigned-to-signed overflow before owner or native admission.
// ValidFrameSequence отвергает zero и unsigned-to-signed overflow до owner либо native admission.
func ValidFrameSequence(sequence uint64) bool {
	return sequence > 0 && sequence <= MaximumFrameSequence
}
