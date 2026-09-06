package runtimecontract

import "time"

const (
	MaximumArtifactTransferChunkBytes = 64 << 10
	MaximumArtifactTransferBytes      = MaximumInputArtifactBytes
	MaximumArtifactTransferDuration   = 2 * time.Minute
)
