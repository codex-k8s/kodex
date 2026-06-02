package main

import (
	"testing"
)

func TestDeploymentReady(t *testing.T) {
	replicas := int32(2)
	deployment := deploymentStatus{}
	deployment.Spec.Replicas = &replicas
	deployment.Status.ReadyReplicas = 2
	deployment.Status.AvailableReplicas = 2
	deployment.Status.Conditions = []condition{{Type: "Available", Status: "True"}}

	if !deploymentReady(deployment) {
		t.Fatal("expected deployment to be ready")
	}

	deployment.Status.ReadyReplicas = 1
	if deploymentReady(deployment) {
		t.Fatal("expected deployment with missing ready replica to fail")
	}
}

func TestJobComplete(t *testing.T) {
	job := jobStatus{}
	job.Status.Succeeded = 1
	job.Status.Conditions = []condition{{Type: "Complete", Status: "True"}}

	if !jobComplete(job) {
		t.Fatal("expected completed job")
	}

	job.Status.Succeeded = 0
	job.Status.Failed = 1
	if jobComplete(job) {
		t.Fatal("expected failed job without success to fail")
	}
}

func TestMissingSecretKeys(t *testing.T) {
	secret := secretObject{Data: map[string]string{
		"present": "encoded-value",
		"empty":   "",
	}}

	missing := missingSecretKeys(secret, []string{"present", "empty", "absent"})
	if len(missing) != 2 || missing[0] != "absent" || missing[1] != "empty" {
		t.Fatalf("unexpected missing keys: %v", missing)
	}
}

func TestValidatePublicIngressTargets(t *testing.T) {
	ingress := ingressWithTargets("web-console-public-oauth2-proxy")
	if err := validatePublicIngressTargets(ingress); err != nil {
		t.Fatalf("expected oauth2-proxy-only ingress to pass: %v", err)
	}

	ingress = ingressWithTargets("web-console-public-oauth2-proxy", "web-console")
	if err := validatePublicIngressTargets(ingress); err == nil {
		t.Fatal("expected direct web-console target to fail")
	}
}

func TestValidateProviderWebhookIngressTargets(t *testing.T) {
	ingress := providerWebhookIngressWithPaths([]ingressTestPath{
		{Service: "integration-gateway", Path: providerWebhookPath, PathType: "Exact"},
	})
	if err := validateProviderWebhookIngressTargets(ingress); err != nil {
		t.Fatalf("expected integration-gateway exact webhook ingress to pass: %v", err)
	}

	ingress = providerWebhookIngressWithPaths([]ingressTestPath{
		{Service: "web-console-public-oauth2-proxy", Path: providerWebhookPath, PathType: "Exact"},
	})
	if err := validateProviderWebhookIngressTargets(ingress); err == nil {
		t.Fatal("expected oauth2-proxy target to fail")
	}

	ingress = providerWebhookIngressWithPaths([]ingressTestPath{
		{Service: "integration-gateway", Path: "/v1/provider-webhooks", PathType: "Prefix"},
	})
	if err := validateProviderWebhookIngressTargets(ingress); err == nil {
		t.Fatal("expected non-exact webhook path to fail")
	}
}

func TestValidateGitHubOAuthLocation(t *testing.T) {
	if err := validateGitHubOAuthLocation("https://github.com/login/oauth/authorize?client_id=redacted"); err != nil {
		t.Fatalf("expected GitHub OAuth location to pass: %v", err)
	}

	if err := validateGitHubOAuthLocation("https://example.com/login/oauth/authorize"); err == nil {
		t.Fatal("expected non-GitHub OAuth location to fail")
	}
}

func TestPublicURLWithPathDropsQuery(t *testing.T) {
	got, err := publicURLWithPath("https://platform.kodex.works/oauth2/callback?code=redacted", providerWebhookPath)
	if err != nil {
		t.Fatalf("public URL with path: %v", err)
	}
	want := "https://platform.kodex.works/v1/provider-webhooks/github"
	if got != want {
		t.Fatalf("URL = %s, want %s", got, want)
	}
}

type ingressTestPath struct {
	Service  string
	Path     string
	PathType string
}

func ingressWithTargets(targets ...string) ingressObject {
	paths := make([]ingressTestPath, 0, len(targets))
	for _, target := range targets {
		paths = append(paths, ingressTestPath{Service: target})
	}
	return providerWebhookIngressWithPaths(paths)
}

func providerWebhookIngressWithPaths(paths []ingressTestPath) ingressObject {
	ingress := ingressObject{}
	ingress.Spec.Rules = append(ingress.Spec.Rules, struct {
		HTTP struct {
			Paths []struct {
				Path     string `json:"path"`
				PathType string `json:"pathType"`
				Backend  struct {
					Service struct {
						Name string `json:"name"`
					} `json:"service"`
				} `json:"backend"`
			} `json:"paths"`
		} `json:"http"`
	}{})
	for _, item := range paths {
		path := struct {
			Path     string `json:"path"`
			PathType string `json:"pathType"`
			Backend  struct {
				Service struct {
					Name string `json:"name"`
				} `json:"service"`
			} `json:"backend"`
		}{}
		path.Path = item.Path
		path.PathType = item.PathType
		path.Backend.Service.Name = item.Service
		ingress.Spec.Rules[0].HTTP.Paths = append(ingress.Spec.Rules[0].HTTP.Paths, path)
	}
	return ingress
}
