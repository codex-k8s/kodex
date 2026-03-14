package githubratelimit

const (
	// ResumePayloadRunPayloadFieldName stores deterministic GitHub rate-limit resume data inside run payload JSON.
	ResumePayloadRunPayloadFieldName = "github_rate_limit_resume_payload"
	// ResumeCorrelationPrefix marks pending runs scheduled specifically for deterministic GitHub rate-limit resume.
	ResumeCorrelationPrefix = "github-rate-limit-resume:"
)
