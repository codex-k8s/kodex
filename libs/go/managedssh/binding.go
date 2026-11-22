package managedssh

// Binding retains the original owner claim; payload identifiers never mint a lease or select a credential.
// Binding сохраняет исходный owner claim; payload identifiers не создают lease и не выбирают credential.
type Binding struct {
	WorkloadInstance, InvocationRef, ConnectionTestRef, LeaseRef, Fence, InputDigest string
	Generation                                                                       int64
}

// Validate accepts one exact claimed operation or health test, with bounded owner-issued coordinates and a canonical snapshot digest.
// Validate допускает одну claimed operation либо health test с bounded owner-issued coordinates и canonical snapshot digest.
func (binding Binding) Validate() error {
	if !OwnerReference(binding.WorkloadInstance) || !OwnerReference(binding.LeaseRef) || !OwnerReference(binding.Fence) || !Digest(binding.InputDigest) || binding.Generation < 1 || binding.Generation > 9007199254740991 || (binding.InvocationRef == "") == (binding.ConnectionTestRef == "") {
		return ErrProtocol
	}
	if binding.InvocationRef != "" && !OwnerReference(binding.InvocationRef) || binding.ConnectionTestRef != "" && (!OwnerReference(binding.ConnectionTestRef) || binding.Generation != 1) {
		return ErrProtocol
	}
	return nil
}

// String conceals the fence and private snapshot coordinates in ordinary diagnostics.
// String скрывает fence и private snapshot coordinates в ordinary diagnostics.
func (Binding) String() string { return "[SSH owner claim]" }

// GoString preserves the same privacy boundary under detailed Go formatting.
// GoString сохраняет ту же privacy boundary при detailed Go formatting.
func (binding Binding) GoString() string { return binding.String() }
