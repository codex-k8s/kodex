package gitsource

import (
	"strings"
	"testing"
)

func TestGitEnvironmentKeepsExternalHTTPSBehindPlatformGateway(t *testing.T) {
	t.Parallel()

	fetcher := &Fetcher{config: FetcherConfig{
		GitExecutable: "/usr/bin/git", CAFile: "/etc/ssl/certs/ca-certificates.crt",
		HTTPSProxy: "http://egress-gateway.mattercodex-system.svc.cluster.local:8080",
		NoProxy:    "localhost,127.0.0.1,::1,.svc,.svc.cluster.local",
	}}
	environment := strings.Join(fetcher.environment("/tmp/git", "/tmp/askpass", "/tmp/credential"), "\n")
	for _, required := range []string{
		"HTTPS_PROXY=http://egress-gateway.mattercodex-system.svc.cluster.local:8080",
		"NO_PROXY=localhost,127.0.0.1,::1,.svc,.svc.cluster.local",
		"GIT_SSL_CAINFO=/etc/ssl/certs/ca-certificates.crt",
	} {
		if !strings.Contains(environment, required) {
			t.Fatalf("Git environment missed platform egress binding %q", required)
		}
	}
}
