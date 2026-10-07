package build

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
)

const rejectionPrivateSentinel = "private-value-DO-NOT-PUBLISH"

func rejectionFixture(t *testing.T) (*Executor, *controlplanev1.RoleImageBuildInput) {
	t.Helper()
	digest := strings.Repeat("a", 64)
	repository := "registry.example.test:5000/kodex/inputs"
	base := "registry.example.test:5000/kodex/agent-runner"
	input := &controlplanev1.RoleImageBuildInput{
		ScopeKind:       controlplanev1.RuntimeResourceScopeKind_RUNTIME_RESOURCE_SCOPE_KIND_ORGANIZATION,
		OrganizationRef: "org_fixture01",
		ContextSha256:   digest, SourceSha256: digest, SpecSha256: digest, ImmutableBuildSha256: digest,
		FrontendSha256: digest, BuilderSha256: digest, ToolchainSha256: digest,
		BaseImageReference: base, BaseImageDigest: "sha256:" + digest,
		RoleRuntimeContractRevision: 2, RoleRuntimeContractSha256: digest,
		ContextRef: "oci://" + repository + "@sha256:" + digest,
		Dockerfile: "FROM " + base + "@sha256:" + digest + "\n",
	}
	executor := &Executor{
		config: Config{WorkspaceRoot: t.TempDir(), ExpectedFrontendSHA256: digest, ExpectedBuilderSHA256: digest,
			ExpectedToolchainSHA256: digest, RoleRuntimeContractRevision: 2, RoleRuntimeContractSHA256: digest},
		allowedBases: baseAllowlist{base + "@sha256:" + digest: {}},
		materializer: &Materializer{repository: repository},
	}
	return executor, input
}

func TestPrepareClosedInputRejectionReasons(t *testing.T) {
	t.Parallel()
	executor, _ := rejectionFixture(t)
	if prepared, diagnostic, err := executor.Prepare(t.Context(), nil, nil); prepared != nil ||
		diagnostic != "INPUT_FETCH_REJECTED" || InputRejectionReason(err) != inputReasonOwnerScope {
		t.Fatal("nil input did not preserve the owner-first rejection")
	}
	for _, test := range []struct {
		name, reason string
		change       func(*Executor, *controlplanev1.RoleImageBuildInput)
	}{
		{"owner", inputReasonOwnerScope, func(_ *Executor, i *controlplanev1.RoleImageBuildInput) { i.OrganizationRef = rejectionPrivateSentinel }},
		{"organization_project", inputReasonOwnerScope, func(_ *Executor, i *controlplanev1.RoleImageBuildInput) { i.ProjectRef = "prj_fixture01" }},
		{"sha", inputReasonSHASchema, func(_ *Executor, i *controlplanev1.RoleImageBuildInput) { i.ContextSha256 = rejectionPrivateSentinel }},
		{"frontend", inputReasonFrontendPin, func(_ *Executor, i *controlplanev1.RoleImageBuildInput) { i.FrontendSha256 = strings.Repeat("b", 64) }},
		{"base_digest_schema", inputReasonSHASchema, func(_ *Executor, i *controlplanev1.RoleImageBuildInput) { i.BaseImageDigest = rejectionPrivateSentinel }},
		{"base_allowlist", inputReasonBaseAllowlist, func(_ *Executor, i *controlplanev1.RoleImageBuildInput) {
			i.BaseImageDigest = "sha256:" + strings.Repeat("b", 64)
		}},
		{"builder", inputReasonBuilderPin, func(_ *Executor, i *controlplanev1.RoleImageBuildInput) { i.BuilderSha256 = strings.Repeat("b", 64) }},
		{"toolchain", inputReasonToolchainPin, func(_ *Executor, i *controlplanev1.RoleImageBuildInput) { i.ToolchainSha256 = strings.Repeat("b", 64) }},
		{"runtime_revision", inputReasonRuntimeContract, func(_ *Executor, i *controlplanev1.RoleImageBuildInput) { i.RoleRuntimeContractRevision++ }},
		{"runtime_sha", inputReasonRuntimeContract, func(_ *Executor, i *controlplanev1.RoleImageBuildInput) {
			i.RoleRuntimeContractSha256 = strings.Repeat("b", 64)
		}},
		{"context_scheme", inputReasonContextRef, func(_ *Executor, i *controlplanev1.RoleImageBuildInput) { i.ContextRef = rejectionPrivateSentinel }},
		{"context_repository", inputReasonContextRef, func(_ *Executor, i *controlplanev1.RoleImageBuildInput) {
			i.ContextRef = "oci://foreign.example.test/private@sha256:" + strings.Repeat("a", 64)
		}},
		{"dockerfile", inputReasonOwnerDockerfile, func(_ *Executor, i *controlplanev1.RoleImageBuildInput) { i.Dockerfile = rejectionPrivateSentinel }},
		{"reserved_alias", inputReasonOwnerDockerfile, func(_ *Executor, i *controlplanev1.RoleImageBuildInput) {
			i.Dockerfile = strings.TrimSpace(i.Dockerfile) + " AS trusted-runtime\n"
		}},
		{"installation", inputReasonOwnerDockerfile, func(_ *Executor, i *controlplanev1.RoleImageBuildInput) {
			i.InstallationBlock = rejectionPrivateSentinel + "\r"
		}},
		{"workspace", inputReasonWorkspaceCreate, func(e *Executor, _ *controlplanev1.RoleImageBuildInput) {
			e.config.WorkspaceRoot = filepath.Join(e.config.WorkspaceRoot, rejectionPrivateSentinel, "missing")
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			executor, input := rejectionFixture(t)
			test.change(executor, input)
			prepared, diagnostic, err := executor.Prepare(t.Context(), input, func() error {
				t.Fatal("rejected input reached context report")
				return nil
			})
			if prepared != nil || diagnostic != "INPUT_FETCH_REJECTED" || InputRejectionReason(err) != test.reason {
				t.Fatalf("unexpected closed classification: diagnostic=%s reason=%s", diagnostic, InputRejectionReason(err))
			}
			if !errors.Is(err, ErrInvalidContext) && !errors.Is(err, ErrMaterialization) {
				t.Fatal("existing sentinel was lost")
			}
			if strings.Contains(err.Error(), rejectionPrivateSentinel) || strings.Contains(InputRejectionSummary(err), "registry.") {
				t.Fatal("input value leaked into diagnostics")
			}
		})
	}
}

func TestMaterializerUpfrontClosedReasons(t *testing.T) {
	t.Parallel()
	executor, input := rejectionFixture(t)
	for _, test := range []struct {
		name, root, reason string
		input              *controlplanev1.RoleImageBuildInput
	}{
		{"nil", t.TempDir(), inputReasonOwnerScope, nil},
		{"relative_root", rejectionPrivateSentinel, inputReasonWorkspaceCreate, input},
		{"unclean_root", "/tmp/" + rejectionPrivateSentinel + "/../other", inputReasonWorkspaceCreate, input},
	} {
		t.Run(test.name, func(t *testing.T) {
			diagnostic, err := executor.materializer.Materialize(t.Context(), test.root, test.input, nil)
			if diagnostic != "INPUT_FETCH_REJECTED" || InputRejectionReason(err) != test.reason || !errors.Is(err, ErrMaterialization) {
				t.Fatal("materializer changed rejection contract")
			}
		})
	}
}

type rejectedInputTransport struct{}

func (rejectedInputTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: http.StatusForbidden, Body: io.NopCloser(strings.NewReader(rejectionPrivateSentinel))}, nil
}

func TestValidPinnedInputPreservesHTTPRejectionDiagnostic(t *testing.T) {
	t.Parallel()
	for _, project := range []bool{false, true} {
		executor, input := rejectionFixture(t)
		if project {
			input.ScopeKind = controlplanev1.RuntimeResourceScopeKind_RUNTIME_RESOURCE_SCOPE_KIND_PROJECT
			input.ProjectRef = "prj_fixture01"
		}
		executor.materializer.client = &http.Client{Transport: rejectedInputTransport{}}
		_, diagnostic, err := executor.Prepare(t.Context(), input, nil)
		if diagnostic != "INPUT_DIGEST_MISMATCH" || !errors.Is(err, ErrMaterialization) || InputRejectionReason(err) != "" {
			t.Fatal("valid input or HTTP failure classification changed")
		}
	}
}

func TestInputRejectionPublicSummaryIsClosed(t *testing.T) {
	t.Parallel()
	for _, reason := range []string{inputReasonOwnerScope, inputReasonSHASchema, inputReasonFrontendPin,
		inputReasonBaseAllowlist, inputReasonBuilderPin, inputReasonToolchainPin, inputReasonRuntimeContract,
		inputReasonContextRef, inputReasonOwnerDockerfile, inputReasonWorkspaceCreate} {
		err := fmt.Errorf("private wrapper %s: %w", rejectionPrivateSentinel, rejectInput(reason, errors.New(rejectionPrivateSentinel)))
		if got := InputRejectionSummary(err); got != inputRejectedSummary+" ("+reason+")" {
			t.Fatal("public summary is not the exact static whitelist output")
		}
	}
	for _, err := range []error{nil, errors.New(rejectionPrivateSentinel),
		rejectInput(rejectionPrivateSentinel, errors.New(rejectionPrivateSentinel)),
		errors.Join(errors.New(rejectionPrivateSentinel), ErrInvalidContext)} {
		if InputRejectionReason(err) != "" || InputRejectionSummary(err) != inputRejectedSummary {
			t.Fatal("unknown error or reason was published")
		}
	}
	if got := rejectInput(rejectionPrivateSentinel, errors.New(rejectionPrivateSentinel)).Error(); got != inputRejectedSummary {
		t.Fatal("typed error leaked an unknown reason")
	}
}
