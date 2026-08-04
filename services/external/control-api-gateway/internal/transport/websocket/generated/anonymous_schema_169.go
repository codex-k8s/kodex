package generated

type AnonymousSchema_169 struct {
	ArtifactKind       string               `json:"artifactKind" binding:"required"`
	Direction          string               `json:"direction" binding:"required"`
	SizeBytes          int                  `json:"sizeBytes" binding:"required"`
	MediaType          string               `json:"mediaType" binding:"required"`
	Sha256             string               `json:"sha256" binding:"required"`
	ScanStatus         *AnonymousSchema_175 `json:"scanStatus" binding:"required"`
	ScanPolicyRevision int                  `json:"scanPolicyRevision" binding:"required"`
	ScanEvidenceSha256 string               `json:"scanEvidenceSha256,omitempty"`
	ScannedAt          string               `json:"scannedAt,omitempty"`
}
