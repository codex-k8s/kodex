package platform

import (
	"reflect"
	"testing"
	"time"

	roleimagerepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/roleimage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

func TestRoleImageBuildIntentCorrelation(t *testing.T) {
	lease := roleimagerepo.BuildLeaseInput{
		Principal: value.Principal{ActorID: "actor", AuthorityTenant: "organization", ProjectRef: "project",
			Permission: "permission", CorrelationRef: "first-verified-RPC", CallerWorkload: "role-image-builder",
			CredentialRevision: 3, CredentialAuthenticatedAt: time.Unix(100, 0).UTC(), CredentialACR: "acr", CredentialAMR: []string{"amr"}},
		IdempotencyKey: "same-key", BuildRef: "build", LeaseToken: "lease", ExpectedVersion: 2, ExpectedFence: 3, ExpectedAttempt: 1,
	}
	for _, test := range []struct {
		name   string
		digest func(roleimagerepo.BuildLeaseInput) string
	}{
		{"renew", roleImageBuildLeaseDigest},
		{"report", func(input roleimagerepo.BuildLeaseInput) string {
			return roleImageBuildProgressDigest(roleimagerepo.BuildProgressInput{BuildLeaseInput: input, Stage: "CONTEXT_VALIDATION", ProgressPercent: 15})
		}},
		{"complete", func(input roleimagerepo.BuildLeaseInput) string {
			return roleImageBuildCompletionDigest(roleimagerepo.BuildCompletionInput{BuildLeaseInput: input, StagingReference: "staging", ManifestDigest: "manifest", ProvenanceSHA256: "provenance", ImmutableBuildSHA256: "input"})
		}},
		{"fail", func(input roleimagerepo.BuildLeaseInput) string {
			return roleImageBuildFailureDigest(roleimagerepo.BuildFailureInput{BuildLeaseInput: input, ErrorCode: "SOLVE_FAILED", DiagnosticCode: "BUILD_GRAPH_REJECTED", DiagnosticSummary: "Build solve failed"})
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			before := lease
			before.Principal.CredentialAMR = append([]string(nil), lease.Principal.CredentialAMR...)
			want := test.digest(lease)
			fresh := lease
			fresh.Principal.CorrelationRef = "second-verified-RPC"
			if test.digest(fresh) != want || !reflect.DeepEqual(before, lease) {
				t.Fatal("fresh correlation changed intent or mutated authority input")
			}
			// Каждый текущий и будущий Principal field остаётся частью commitment.
			principalType := reflect.TypeOf(lease.Principal)
			for index := 0; index < principalType.NumField(); index++ {
				name := principalType.Field(index).Name
				if name == "CorrelationRef" {
					continue
				}
				t.Run(name, func(t *testing.T) {
					changed := lease
					field := reflect.ValueOf(&changed.Principal).Elem().Field(index)
					field.Set(reflect.Zero(field.Type()))
					if test.digest(changed) == want {
						t.Fatal("authority field disappeared from intent")
					}
				})
			}
			leaseType := reflect.TypeOf(lease)
			for index := 0; index < leaseType.NumField(); index++ {
				name := leaseType.Field(index).Name
				if name == "Principal" {
					continue
				}
				t.Run(name, func(t *testing.T) {
					changed := lease
					field := reflect.ValueOf(&changed).Elem().Field(index)
					field.Set(reflect.Zero(field.Type()))
					if test.digest(changed) == want {
						t.Fatal("lease or OCC field disappeared from intent")
					}
				})
			}
		})
	}
}

func TestRoleImageBuildIntentPayload(t *testing.T) {
	progress := roleimagerepo.BuildProgressInput{Stage: "MATERIALIZATION", ProgressPercent: 5}
	changedProgress := progress
	changedProgress.Stage = "CONTEXT_VALIDATION"
	if roleImageBuildProgressDigest(progress) == roleImageBuildProgressDigest(changedProgress) {
		t.Fatal("stage is not committed")
	}
	changedProgress = progress
	changedProgress.ProgressPercent++
	if roleImageBuildProgressDigest(progress) == roleImageBuildProgressDigest(changedProgress) {
		t.Fatal("progress is not committed")
	}
	completion := roleimagerepo.BuildCompletionInput{StagingReference: "staging", ManifestDigest: "manifest", ProvenanceSHA256: "provenance", ImmutableBuildSHA256: "input"}
	failure := roleimagerepo.BuildFailureInput{ErrorCode: "SOLVE_FAILED", DiagnosticCode: "BUILD_GRAPH_REJECTED", DiagnosticSummary: "Build solve failed"}
	for _, test := range []struct {
		input  any
		digest func(any) string
	}{
		{completion, func(input any) string {
			return roleImageBuildCompletionDigest(input.(roleimagerepo.BuildCompletionInput))
		}},
		{failure, func(input any) string { return roleImageBuildFailureDigest(input.(roleimagerepo.BuildFailureInput)) }},
	} {
		original := reflect.ValueOf(test.input)
		for index := 1; index < original.NumField(); index++ {
			changed := reflect.New(original.Type()).Elem()
			changed.Set(original)
			changed.Field(index).SetString("changed")
			if test.digest(test.input) == test.digest(changed.Interface()) {
				t.Fatal("terminal payload field is not committed")
			}
		}
	}
	lease := roleimagerepo.BuildLeaseInput{Principal: value.Principal{CorrelationRef: "fresh"}}
	normalized := lease
	normalized.Principal.CorrelationRef = ""
	if roleImageBuildLeaseDigest(lease) != roleImageDigest(normalized) {
		t.Fatal("digest changed fields beyond correlation")
	}
}
