package managedkubernetes

const (
	// InputContractRevision identifies the implemented one-attempt INPUT owner/stage/claim contract.
	// A field reservation or absent storage/schema is never positive readiness evidence.
	// InputContractRevision обозначает implemented one-attempt INPUT owner/stage/claim contract.
	// Reservation поля либо отсутствующие storage/schema не являются positive readiness evidence.
	InputContractRevision uint32 = 1
	// FileInputContractRevision identifies original-catalog FILE normalization and prepared-source INPUT binding.
	// It requires the actual bounded immutable range reader and purpose source lifecycle, not a DTO alone.
	// FileInputContractRevision обозначает original-catalog FILE normalization и prepared-source INPUT binding.
	// Требует actual bounded immutable range reader и purpose source lifecycle, не один DTO.
	FileInputContractRevision uint32 = 1
)
