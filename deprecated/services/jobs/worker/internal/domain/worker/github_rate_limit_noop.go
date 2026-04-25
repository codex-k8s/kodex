package worker

import "context"

type noopGitHubRateLimitWaitProcessor struct{}

func (noopGitHubRateLimitWaitProcessor) ProcessNextGitHubRateLimitWait(context.Context, string) (GitHubRateLimitProcessResult, bool, error) {
	return GitHubRateLimitProcessResult{}, false, nil
}
