package command

import (
	"crypto/sha256"
	"encoding/hex"
	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
)

// ManagedKubernetesOutputFrameLookup names only an original output sequence and durable ordinal.
// It contains neither a storage path nor actor, target, namespace, deadline or artifact selectors.
// ManagedKubernetesOutputFrameLookup указывает лишь original output sequence и durable ordinal.
// Не содержит storage path либо actor/target/namespace/deadline/artifact selectors.
type ManagedKubernetesOutputFrameLookup struct {
	StreamRef       string
	Sequence        uint64
	DeliveryOrdinal uint32
}

// Valid checks bounded syntax without granting access to the stored frame or its bytes.
// Valid проверяет bounded syntax без выдачи доступа к stored frame либо bytes.
func (input ManagedKubernetesOutputFrameLookup) Valid() bool {
	return len(input.StreamRef) <= 96 && managedkubernetes.ValidLocator(input.StreamRef) && managedkubernetes.ValidFrameSequence(input.Sequence) && input.DeliveryOrdinal > 0 && input.DeliveryOrdinal <= managedkubernetes.MaximumOutputDeliveries
}

// ManagedKubernetesAcceptOutputFrame carries one owned binary chunk; the existing frame remains authoritative.
// Storage metadata, materialization attempts and user-visible artifacts are assigned only by the owner.
// ManagedKubernetesAcceptOutputFrame содержит один owned binary chunk; existing frame остаётся authoritative.
// Storage metadata/materialization attempts/user-visible artifacts назначает только owner.
type ManagedKubernetesAcceptOutputFrame struct {
	ManagedKubernetesOutputFrameLookup
	Channel, PayloadDigest string
	ByteCount              uint32
	Payload                []byte
}

// Valid rejects input-only channels, oversized or mismatched bytes before storage admission.
// A correct digest is not authority; the repository must match the exact existing OUTPUT tuple.
// Valid отвергает input-only channels, oversize либо mismatched bytes до storage admission.
// Correct digest не является authority; repository сверяет exact existing OUTPUT tuple.
func (input ManagedKubernetesAcceptOutputFrame) Valid() bool {
	if !input.ManagedKubernetesOutputFrameLookup.Valid() || !managedkubernetes.ValidChannel(input.Channel) || input.Channel == "STDIN" || input.Channel == "TERMINAL_RESIZE" || !managedkubernetes.ValidDigest(input.PayloadDigest) || input.ByteCount == 0 || input.ByteCount > managedkubernetes.MaximumFrameBytes || len(input.Payload) != int(input.ByteCount) {
		return false
	}
	digest := sha256.Sum256(input.Payload)
	return hex.EncodeToString(digest[:]) == input.PayloadDigest
}
