package managedkubernetes

import (
	"encoding/base64"
	"encoding/binary"
	"math"
)

const (
	// MaximumPortForwardConnections limits simultaneously live connections, never sequential lifetime attempts.
	// MaximumPortForwardConnections ограничивает одновременно live connections, не sequential lifetime attempts.
	MaximumPortForwardConnections = 16
	// PortForwardConnectionFrameHeaderBytes includes the fixed version/action/slot/generation and canonical owner reference.
	// PortForwardConnectionFrameHeaderBytes включает fixed version/action/slot/generation и canonical owner reference.
	PortForwardConnectionFrameHeaderBytes = 51
	// portForwardConnectionRefBytes matches the canonical newRef("kpf") encoding of eighteen random owner bytes.
	// portForwardConnectionRefBytes соответствует canonical newRef("kpf") encoding восемнадцати случайных owner bytes.
	portForwardConnectionRefBytes = 28
	// portForwardConnectionWireVersion rejects silent compatibility with the old slot-only profile.
	// portForwardConnectionWireVersion запрещает скрытую совместимость со старым slot-only profile.
	portForwardConnectionWireVersion = 1
)

// PortForwardConnectionAction is a closed direction-specific transport operation, not permission to create a connection.
// PortForwardConnectionAction — закрытая direction-specific transport operation, не право создать connection.
type PortForwardConnectionAction byte

const (
	// PortForwardConnectionOpen initializes one owner-assigned native DATA/ERROR pair after its staged admission.
	// PortForwardConnectionOpen инициализирует owner-assigned native DATA/ERROR pair после staged admission.
	PortForwardConnectionOpen PortForwardConnectionAction = 1
	// PortForwardConnectionData carries bytes for the exact already-open connection.
	// PortForwardConnectionData несёт bytes для exact already-open connection.
	PortForwardConnectionData PortForwardConnectionAction = 2
	// PortForwardConnectionHalfClose sends local write FIN on INPUT; OUTPUT records a read-end without proving remote success or distinguishing FIN from RST.
	// PortForwardConnectionHalfClose отправляет local write FIN на INPUT; OUTPUT фиксирует read-end без remote success либо различения FIN и RST.
	PortForwardConnectionHalfClose PortForwardConnectionAction = 3
	// PortForwardConnectionClose closes the exact local pair; owner closure and remote termination remain separate facts.
	// PortForwardConnectionClose закрывает exact local pair; owner closure и remote termination остаются отдельными фактами.
	PortForwardConnectionClose PortForwardConnectionAction = 4
)

// PortForwardConnectionFrame is a protected binary envelope with an owner-assigned locator/generation and approved mapping slot.
// Body is borrowed from decoded input, excluded from JSON and never interpreted as target, credential or authority metadata.
// PortForwardConnectionFrame — protected binary envelope с owner-assigned locator/generation и approved mapping slot.
// Body заимствован из decoded input, исключён из JSON и не трактуется как target, credential либо authority metadata.
type PortForwardConnectionFrame struct {
	Action     PortForwardConnectionAction
	Slot       byte
	Generation uint64
	// NativeOrdinal identifies one actual native OUTPUT candidate; INPUT must leave it zero.
	// NativeOrdinal обозначает actual native OUTPUT candidate; INPUT оставляет ноль.
	NativeOrdinal uint64
	ConnectionRef string
	Body          []byte `json:"-"`
}

// ValidPortForwardConnectionRef requires the canonical owner-issued prefix and exact unpadded encoding; caller possession does not prove ownership.
// ValidPortForwardConnectionRef требует canonical owner-issued prefix и exact unpadded encoding; владение строкой не доказывает ownership.
func ValidPortForwardConnectionRef(ref string) bool {
	if len(ref) != portForwardConnectionRefBytes || ref[:4] != "kpf_" {
		return false
	}
	raw, err := base64.RawURLEncoding.DecodeString(ref[4:])
	return err == nil && len(raw) == 18 && base64.RawURLEncoding.EncodeToString(raw) == ref[4:]
}

// Validate enforces finite canonical syntax and the exact approved slot count; durable owner state still decides each action.
// Validate проверяет finite canonical syntax и exact approved slot count; durable owner state отдельно решает допустимость каждого action.
func (frame PortForwardConnectionFrame) Validate(slots int) error {
	if !validPortForwardConnectionCoordinates(frame.ConnectionRef, frame.Generation, frame.NativeOrdinal, frame.Slot, slots) || len(frame.Body) > MaximumFrameBytes-PortForwardConnectionFrameHeaderBytes {
		return ErrInvalidOperationInput
	}
	switch frame.Action {
	case PortForwardConnectionData:
		if len(frame.Body) == 0 {
			return ErrInvalidOperationInput
		}
	case PortForwardConnectionOpen, PortForwardConnectionHalfClose, PortForwardConnectionClose:
		if len(frame.Body) != 0 {
			return ErrInvalidOperationInput
		}
	default:
		return ErrInvalidOperationInput
	}
	return nil
}

// ValidateInput rejects caller-assigned native event witnesses; the owner still resolves connection ownership and lifecycle.
// ValidateInput отклоняет caller-assigned native event witnesses; owner отдельно разрешает connection ownership и lifecycle.
func (frame PortForwardConnectionFrame) ValidateInput(slots int) error {
	if frame.Validate(slots) != nil || frame.NativeOrdinal != 0 {
		return ErrInvalidOperationInput
	}
	return nil
}

// ValidateOutput requires one native event witness and forbids an OUTPUT OPEN that could masquerade as input admission.
// ValidateOutput требует native event witness и запрещает OUTPUT OPEN, который мог бы подменить input admission.
func (frame PortForwardConnectionFrame) ValidateOutput(slots int) error {
	if frame.Validate(slots) != nil || frame.NativeOrdinal == 0 || frame.Action == PortForwardConnectionOpen {
		return ErrInvalidOperationInput
	}
	return nil
}

// PortForwardConnectionWitness binds a payload-free owner admission to the exact canonical native OUTPUT envelope.
// NativeOrdinal is an event identity, not an owner-issued sequence or reusable execution credit.
// PortForwardConnectionWitness связывает payload-free owner admission с exact canonical native OUTPUT envelope.
// NativeOrdinal обозначает event identity, не owner-issued sequence либо reusable execution credit.
type PortForwardConnectionWitness struct {
	ConnectionRef             string
	Generation, NativeOrdinal uint64
	Slot                      byte
	Action                    PortForwardConnectionAction
}

// validPortForwardConnectionCoordinates shares canonical reference, integer and approved-slot bounds across private envelopes and metadata.
// validPortForwardConnectionCoordinates разделяет canonical reference, integer и approved-slot bounds между private envelopes и metadata.
func validPortForwardConnectionCoordinates(ref string, generation, ordinal uint64, slot byte, slots int) bool {
	return slots >= 1 && slots <= MaximumForwardPorts && int(slot) < slots && generation > 0 && generation <= math.MaxInt64 && ordinal <= math.MaxInt64 && ValidPortForwardConnectionRef(ref)
}

// ValidateOutput checks the metadata body's exact byte shape without accepting INPUT selectors or material.
// ValidateOutput проверяет exact byte shape metadata body без принятия INPUT selectors либо material.
func (witness PortForwardConnectionWitness) ValidateOutput(count uint32, slots int) error {
	if !validPortForwardConnectionCoordinates(witness.ConnectionRef, witness.Generation, witness.NativeOrdinal, witness.Slot, slots) || witness.NativeOrdinal == 0 || count > MaximumFrameBytes {
		return ErrInvalidOperationInput
	}
	switch witness.Action {
	case PortForwardConnectionData:
		if count <= PortForwardConnectionFrameHeaderBytes {
			return ErrInvalidOperationInput
		}
	case PortForwardConnectionHalfClose, PortForwardConnectionClose:
		if count != PortForwardConnectionFrameHeaderBytes {
			return ErrInvalidOperationInput
		}
	default:
		return ErrInvalidOperationInput
	}
	return nil
}

// PortForwardOutputWitness derives only native-emitted metadata from one validated protected envelope; no caller target or connection is synthesized.
// PortForwardOutputWitness выводит лишь native-emitted metadata из validated protected envelope; caller target либо connection не синтезируются.
func PortForwardOutputWitness(raw []byte) (PortForwardConnectionWitness, error) {
	frame, err := DecodePortForwardConnectionFrame(raw)
	if err != nil || frame.ValidateOutput(MaximumForwardPorts) != nil {
		return PortForwardConnectionWitness{}, ErrInvalidOperationInput
	}
	return PortForwardConnectionWitness{ConnectionRef: frame.ConnectionRef, Generation: frame.Generation, NativeOrdinal: frame.NativeOrdinal, Slot: frame.Slot, Action: frame.Action}, nil
}

// EncodePortForwardConnectionFrame owns the exact protected bytes covered by the existing staged INPUT/OUTPUT digest.
// It never allocates a locator/generation or rewrites a caller payload after its digest has been fixed.
// EncodePortForwardConnectionFrame владеет exact protected bytes, покрываемыми existing staged INPUT/OUTPUT digest.
// Не назначает locator/generation и не переписывает caller payload после фиксации digest.
func EncodePortForwardConnectionFrame(frame PortForwardConnectionFrame) ([]byte, error) {
	if frame.Validate(MaximumForwardPorts) != nil {
		return nil, ErrInvalidOperationInput
	}
	raw := make([]byte, PortForwardConnectionFrameHeaderBytes+len(frame.Body))
	copy(raw[:3], "KPF")
	raw[3], raw[4], raw[5] = portForwardConnectionWireVersion, byte(frame.Action), frame.Slot
	binary.BigEndian.PutUint64(raw[6:14], frame.Generation)
	raw[14] = portForwardConnectionRefBytes
	copy(raw[15:15+portForwardConnectionRefBytes], frame.ConnectionRef)
	binary.BigEndian.PutUint64(raw[15+portForwardConnectionRefBytes:PortForwardConnectionFrameHeaderBytes], frame.NativeOrdinal)
	copy(raw[PortForwardConnectionFrameHeaderBytes:], frame.Body)
	return raw, nil
}

// DecodePortForwardConnectionFrame rejects truncated, legacy, unknown and noncanonical envelopes before any native effect.
// The returned Body borrows raw and must not outlive its caller's protected payload ownership.
// DecodePortForwardConnectionFrame отклоняет truncated, legacy, unknown и noncanonical envelopes до native effect.
// Возвращённый Body заимствует raw и не должен переживать protected payload ownership caller.
func DecodePortForwardConnectionFrame(raw []byte) (PortForwardConnectionFrame, error) {
	var empty PortForwardConnectionFrame
	if len(raw) < PortForwardConnectionFrameHeaderBytes || len(raw) > MaximumFrameBytes || string(raw[:3]) != "KPF" || raw[3] != portForwardConnectionWireVersion || raw[14] != portForwardConnectionRefBytes {
		return empty, ErrInvalidOperationInput
	}
	frame := PortForwardConnectionFrame{Action: PortForwardConnectionAction(raw[4]), Slot: raw[5], Generation: binary.BigEndian.Uint64(raw[6:14]), ConnectionRef: string(raw[15 : 15+portForwardConnectionRefBytes]), NativeOrdinal: binary.BigEndian.Uint64(raw[15+portForwardConnectionRefBytes : PortForwardConnectionFrameHeaderBytes]), Body: raw[PortForwardConnectionFrameHeaderBytes:]}
	if frame.Validate(MaximumForwardPorts) != nil {
		return empty, ErrInvalidOperationInput
	}
	return frame, nil
}

// String excludes private TCP bytes from ordinary diagnostics, including when enclosed by another value.
// String исключает private TCP bytes из ordinary diagnostics, в том числе внутри другого value.
func (PortForwardConnectionFrame) String() string { return "[managed Kubernetes port-forward frame]" }

// GoString preserves private-body redaction during detailed Go formatting.
// GoString сохраняет private-body redaction при подробном Go formatting.
func (frame PortForwardConnectionFrame) GoString() string { return frame.String() }
