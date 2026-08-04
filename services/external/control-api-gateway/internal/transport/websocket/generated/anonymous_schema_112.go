package generated

type AnonymousSchema_112 struct {
	SessionId            string `json:"sessionId" binding:"required"`
	Sequence             int    `json:"sequence" binding:"required"`
	SourceRef            string `json:"sourceRef" binding:"required"`
	RuntimeRevisionId    string `json:"runtimeRevisionId" binding:"required"`
	ProcessRunId         string `json:"processRunId,omitempty"`
	Attempt              int    `json:"attempt" binding:"required"`
	ResultArtifactId     string `json:"resultArtifactId,omitempty"`
	EffectiveInputSha256 string `json:"effectiveInputSha256" binding:"required"`
}
