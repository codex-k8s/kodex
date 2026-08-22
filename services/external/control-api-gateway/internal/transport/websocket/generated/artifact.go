
package generated

type Artifact struct {
  Ref string
  Version int
  ProjectRef string
  RunRef string
  FileName string
  MediaType string
  SizeBytes int
  ScanState *ArtifactScanState
  Source *ArtifactSource
  Revision int
  AgentBindings []string
  PreviewAvailable bool
  CreatedAt string
  NextActions []NextAction
}