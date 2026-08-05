
package generated

type ImageBuildStage uint

const (
  ImageBuildStageQueued ImageBuildStage = iota
  ImageBuildStageContextValidation
  ImageBuildStageBasePull
  ImageBuildStageSolving
  ImageBuildStageStagingPush
  ImageBuildStageProvenance
  ImageBuildStageCompleted
  ImageBuildStageFailed
  ImageBuildStageCancelled
  ImageBuildStageExpired
  ImageBuildStageDeadLetter
)

// Value returns the value of the enum.
func (op ImageBuildStage) Value() any {
	if op >= ImageBuildStage(len(ImageBuildStageValues)) {
		return nil
	}
	return ImageBuildStageValues[op]
}

var ImageBuildStageValues = []any{"QUEUED","CONTEXT_VALIDATION","BASE_PULL","SOLVING","STAGING_PUSH","PROVENANCE","COMPLETED","FAILED","CANCELLED","EXPIRED","DEAD_LETTER"}
var ValuesToImageBuildStage = map[any]ImageBuildStage{
  ImageBuildStageValues[ImageBuildStageQueued]: ImageBuildStageQueued,
  ImageBuildStageValues[ImageBuildStageContextValidation]: ImageBuildStageContextValidation,
  ImageBuildStageValues[ImageBuildStageBasePull]: ImageBuildStageBasePull,
  ImageBuildStageValues[ImageBuildStageSolving]: ImageBuildStageSolving,
  ImageBuildStageValues[ImageBuildStageStagingPush]: ImageBuildStageStagingPush,
  ImageBuildStageValues[ImageBuildStageProvenance]: ImageBuildStageProvenance,
  ImageBuildStageValues[ImageBuildStageCompleted]: ImageBuildStageCompleted,
  ImageBuildStageValues[ImageBuildStageFailed]: ImageBuildStageFailed,
  ImageBuildStageValues[ImageBuildStageCancelled]: ImageBuildStageCancelled,
  ImageBuildStageValues[ImageBuildStageExpired]: ImageBuildStageExpired,
  ImageBuildStageValues[ImageBuildStageDeadLetter]: ImageBuildStageDeadLetter,
}
