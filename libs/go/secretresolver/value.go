package secretresolver

const redactedSecret = "[secret redacted]"

// SecretValue keeps raw secret bytes out of JSON, text and fmt output.
type SecretValue struct {
	raw []byte
}

// NewSecretValue creates a secret value and copies input bytes.
func NewSecretValue(raw []byte) SecretValue {
	return SecretValue{raw: append([]byte(nil), raw...)}
}

func newSecretValueOwned(raw []byte) SecretValue {
	return SecretValue{raw: raw}
}

// Bytes returns a copy of the secret bytes for one in-memory operation.
func (v SecretValue) Bytes() []byte {
	return append([]byte(nil), v.raw...)
}

// Clear zeroes the internal buffer and releases it from the value.
func (v *SecretValue) Clear() {
	if v == nil {
		return
	}
	clearBytes(v.raw)
	v.raw = nil
}

// Len returns the byte length without exposing the value.
func (v SecretValue) Len() int {
	return len(v.raw)
}

// String redacts secret values in logs and fmt output.
func (v SecretValue) String() string {
	return redactedSecret
}

// GoString redacts secret values for %#v formatting.
func (v SecretValue) GoString() string {
	return "secretresolver.SecretValue(" + redactedSecret + ")"
}

// MarshalJSON blocks accidental JSON serialization.
func (v SecretValue) MarshalJSON() ([]byte, error) {
	return nil, ErrSecretSerialization
}

// MarshalText blocks accidental text serialization.
func (v SecretValue) MarshalText() ([]byte, error) {
	return nil, ErrSecretSerialization
}

func clearBytes(raw []byte) {
	for i := range raw {
		raw[i] = 0
	}
}
