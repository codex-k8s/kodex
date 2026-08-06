
package generated

type ImageBuildStage uint

const (
  ImageBuildStageQueued ImageBuildStage = iota
  ImageBuildStageMaterialization
  ImageBuildStageContextValidation
  ImageBuildStageBasePull
  ImageBuildStageSolving
  ImageBuildStageInstallation
  ImageBuildStageTrustedRuntimeFinalization
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

var ImageBuildStageValues = []any{"QUEUED","MATERIALIZATION","CONTEXT_VALIDATION","BASE_PULL","SOLVING","INSTALLATION","TRUSTED_RUNTIME_FINALIZATION","STAGING_PUSH","PROVENANCE","COMPLETED","FAILED","CANCELLED","EXPIRED","DEAD_LETTER"}
var ValuesToImageBuildStage = map[any]ImageBuildStage{
  ImageBuildStageValues[ImageBuildStageQueued]: ImageBuildStageQueued,
  ImageBuildStageValues[ImageBuildStageMaterialization]: ImageBuildStageMaterialization,
  ImageBuildStageValues[ImageBuildStageContextValidation]: ImageBuildStageContextValidation,
  ImageBuildStageValues[ImageBuildStageBasePull]: ImageBuildStageBasePull,
  ImageBuildStageValues[ImageBuildStageSolving]: ImageBuildStageSolving,
  ImageBuildStageValues[ImageBuildStageInstallation]: ImageBuildStageInstallation,
  ImageBuildStageValues[ImageBuildStageTrustedRuntimeFinalization]: ImageBuildStageTrustedRuntimeFinalization,
  ImageBuildStageValues[ImageBuildStageStagingPush]: ImageBuildStageStagingPush,
  ImageBuildStageValues[ImageBuildStageProvenance]: ImageBuildStageProvenance,
  ImageBuildStageValues[ImageBuildStageCompleted]: ImageBuildStageCompleted,
  ImageBuildStageValues[ImageBuildStageFailed]: ImageBuildStageFailed,
  ImageBuildStageValues[ImageBuildStageCancelled]: ImageBuildStageCancelled,
  ImageBuildStageValues[ImageBuildStageExpired]: ImageBuildStageExpired,
  ImageBuildStageValues[ImageBuildStageDeadLetter]: ImageBuildStageDeadLetter,
}
