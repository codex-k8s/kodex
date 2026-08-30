package platform

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestPromotionEvidenceGateRejectsIncompleteAdmission(t *testing.T) {
	_, artifact, _ := promotionTestSnapshot()
	if !exactPromotionEvidence(artifact.Artifact) {
		t.Fatal("valid exact promotion evidence was rejected")
	}
	tests := map[string]func(*entity.ImageArtifact){
		"manifest":              func(value *entity.ImageArtifact) { value.ManifestDigest = "latest" },
		"provenance":            func(value *entity.ImageArtifact) { value.ProvenanceSHA256 = "" },
		"immutable build":       func(value *entity.ImageArtifact) { value.ImmutableBuildSHA256 = "" },
		"SBOM":                  func(value *entity.ImageArtifact) { value.SBOMSHA256 = "" },
		"vulnerability":         func(value *entity.ImageArtifact) { value.VulnerabilityEvidenceSHA256 = "" },
		"signature identity":    func(value *entity.ImageArtifact) { value.SignatureIdentity = "invalid identity" },
		"signature":             func(value *entity.ImageArtifact) { value.SignatureSHA256 = "" },
		"admission revision":    func(value *entity.ImageArtifact) { value.AdmissionRevision = 0 },
		"admission receipt":     func(value *entity.ImageArtifact) { value.AdmissionReceiptSHA256 = "" },
		"admission OCI receipt": func(value *entity.ImageArtifact) { value.AdmissionReceiptOCIManifestDigest = "" },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			changed := artifact.Artifact
			mutate(&changed)
			if exactPromotionEvidence(changed) {
				t.Fatalf("incomplete %s evidence was accepted", name)
			}
		})
	}
}

func TestPromotionReceiptDigestBindsImmutableEvidence(t *testing.T) {
	_, artifact, request := promotionTestSnapshot()
	baseline := promotionRequestDigest(request.Ref, request.RequestedBy, artifact)
	if baseline != request.ReceiptSHA256 || !promotionRequestMatches(request, artifact) {
		t.Fatalf("valid promotion request receipt mismatch: digest=%s request=%#v", baseline, request)
	}
	versionChanged := artifact
	versionChanged.Artifact.Version++
	if got := promotionRequestDigest(request.Ref, request.RequestedBy, versionChanged); got != baseline {
		t.Fatalf("mutable lifecycle version changed immutable receipt: before=%s after=%s", baseline, got)
	}
	mutations := map[string]func(*entity.ImageArtifact){
		"provenance":    func(value *entity.ImageArtifact) { value.ProvenanceSHA256 = strings.Repeat("1", 64) },
		"SBOM":          func(value *entity.ImageArtifact) { value.SBOMSHA256 = strings.Repeat("2", 64) },
		"vulnerability": func(value *entity.ImageArtifact) { value.VulnerabilityEvidenceSHA256 = strings.Repeat("3", 64) },
		"signature":     func(value *entity.ImageArtifact) { value.SignatureSHA256 = strings.Repeat("4", 64) },
		"admission":     func(value *entity.ImageArtifact) { value.AdmissionReceiptSHA256 = strings.Repeat("5", 64) },
		"policy":        func(value *entity.ImageArtifact) { value.PolicySHA256 = strings.Repeat("6", 64) },
		"runtime ABI":   func(value *entity.ImageArtifact) { value.RoleRuntimeContractSHA256 = strings.Repeat("7", 64) },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			changed := artifact
			mutate(&changed.Artifact)
			if got := promotionRequestDigest(request.Ref, request.RequestedBy, changed); got == baseline {
				t.Fatalf("%s evidence is not bound to promotion receipt", name)
			}
		})
	}
}

func TestPromotionTokensBindClaimAndAuthorizationSnapshot(t *testing.T) {
	repository := &Repository{roleImages: RoleImageConfig{
		LeaseSigningKey:    []byte("0123456789abcdef0123456789abcdef"),
		PromotedRepository: "registry.internal/promoted/roles",
	}}
	_, artifact, request := promotionTestSnapshot()
	expiresAt := time.Unix(2_000_000_000, 0).UTC()
	baseline := repository.roleImagePromotionToken("image-promotion", artifact.Artifact,
		request.ReceiptSHA256, 4, 7, expiresAt)
	changed := artifact.Artifact
	changed.SBOMSHA256 = strings.Repeat("1", 64)
	checks := map[string]string{
		"artifact version": repository.roleImagePromotionToken("image-promotion", func() entity.ImageArtifact {
			value := artifact.Artifact
			value.Version++
			return value
		}(), request.ReceiptSHA256, 4, 7, expiresAt),
		"evidence": repository.roleImagePromotionToken("image-promotion", changed,
			request.ReceiptSHA256, 4, 7, expiresAt),
		"request receipt": repository.roleImagePromotionToken("image-promotion", artifact.Artifact,
			strings.Repeat("1", 64), 4, 7, expiresAt),
		"fence": repository.roleImagePromotionToken("image-promotion", artifact.Artifact,
			request.ReceiptSHA256, 5, 7, expiresAt),
		"generation": repository.roleImagePromotionToken("image-promotion", artifact.Artifact,
			request.ReceiptSHA256, 4, 8, expiresAt),
		"purpose": repository.roleImagePromotionToken("image-promotion-authorization", artifact.Artifact,
			request.ReceiptSHA256, 4, 7, expiresAt),
	}
	for name, token := range checks {
		if token == baseline {
			t.Fatalf("%s is not bound to promotion token", name)
		}
	}
}

func TestRoleImageTransactionRetryAndCommitClassification(t *testing.T) {
	calls := 0
	result, err := retryRoleImageTransaction(context.Background(), func() (string, error) {
		calls++
		if calls == 1 {
			return "", errors.Join(errRoleImageTransactionRetry, errs.ErrUnavailable)
		}
		return "committed receipt", nil
	})
	if err != nil || result != "committed receipt" || calls != 2 {
		t.Fatalf("bounded idempotent retry mismatch: result=%q calls=%d err=%v", result, calls, err)
	}

	tests := []struct {
		name                 string
		err                  error
		wantConflict         bool
		wantUnavailable      bool
		wantTransactionRetry bool
	}{
		{name: "serialization rollback", err: &pgconn.PgError{Code: "40001"}, wantConflict: true, wantTransactionRetry: true},
		{name: "deadlock rollback", err: &pgconn.PgError{Code: "40P01"}, wantConflict: true, wantTransactionRetry: true},
		{name: "aborted transaction rollback", err: pgx.ErrTxCommitRollback, wantConflict: true, wantTransactionRetry: true},
		{name: "ambiguous connection failure", err: errors.New("connection lost during commit"), wantUnavailable: true, wantTransactionRetry: true},
		{name: "definite unique conflict", err: &pgconn.PgError{Code: "23505"}, wantConflict: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			mapped := roleImageCommitError(test.err)
			if errors.Is(mapped, errs.ErrConflict) != test.wantConflict ||
				errors.Is(mapped, errs.ErrUnavailable) != test.wantUnavailable ||
				errors.Is(mapped, errRoleImageTransactionRetry) != test.wantTransactionRetry {
				t.Fatalf("commit error classification mismatch: mapped=%v", mapped)
			}
		})
	}
}

func TestRoleImagePromotionCancellationIsTerminal(t *testing.T) {
	lower := strings.ToLower(queryRoleImagesCancelOpenPromotions)
	for _, required := range []string{
		"state = 'failed'", "promotion_state = 'rejected'", "promotion_claim_token_sha256 = null",
		"promotion_authorization_token_sha256 = null", "promotion_authority_generation = 0",
	} {
		if !strings.Contains(lower, required) {
			t.Fatalf("promotion cancellation does not enforce %q", required)
		}
	}
}

func TestRoleImagePromotionQueriesKeepOneWinnerAndExactCompletion(t *testing.T) {
	tests := map[string]struct {
		query    string
		required []string
	}{
		"claim candidate": {query: queryRoleImagesClaimPromotionCandidate, required: []string{
			"for update of artifact, request, recipe skip locked", "recipe.version = artifact.recipe_version",
			"artifact.admission_receipt_sha256", "artifact.admission_receipt_oci_manifest_digest",
		}},
		"claim": {query: queryRoleImagesClaimPromotion, required: []string{
			"artifact.version = $3", "artifact.promotion_request_id = $9::uuid", "request.receipt_sha256 = $10",
		}},
		"authorize": {query: queryRoleImagesAuthorizePromotion, required: []string{
			"promotion_authority_generation = $6", "promotion_fence = $7",
			"promotion_claim_token_sha256 = $9", "request.receipt_sha256 = $12",
		}},
		"complete": {query: queryRoleImagesCompletePromotion, required: []string{
			"promotion_authority_generation = $6", "promotion_fence = $7",
			"promotion_authorization_token_sha256 = $9", "request.receipt_sha256 = $14",
		}},
		"activate": {query: queryRoleImagesActivateArtifact, required: []string{
			"version = $4", "artifact.promotion_state = 'promoted'", "request.state = 'promoting'",
		}},
		"immutable revision": {query: queryRoleImagesInsertPromotedRevision, required: []string{
			"artifact.promotion_state = 'promoted'", "recipe.active_image_artifact_id = artifact.id",
			"artifact.specification->>'sourcesha256' = @source_sha256",
			"artifact.promotion_readback_sha256 = @promotion_readback_sha256",
		}},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			query := strings.ToLower(test.query)
			for _, required := range test.required {
				if !strings.Contains(query, required) {
					t.Fatalf("promotion query does not enforce %q", required)
				}
			}
		})
	}
}

func promotionTestSnapshot() (lockedRecipe, lockedArtifact, lockedPromotionRequest) {
	digest := strings.Repeat("a", 64)
	manifest := "sha256:" + strings.Repeat("b", 64)
	recipe := lockedRecipe{ID: "recipe-id", Recipe: entity.RoleImageRecipe{
		Ref: "imgrec_12345678", State: "ACTIVE", Version: 2, Generation: 3,
		SpecSHA256: digest, PolicyRevision: 4, PolicySHA256: digest,
		RoleRuntimeContractRevision: 5, RoleRuntimeContractSHA256: digest,
	}}
	artifact := lockedArtifact{
		ID: "artifact-id", RecipeID: recipe.ID, AdmissionState: "ACCEPTED",
		PromotionState: "PENDING", PromotionRequestID: "promotion-request-id",
		Artifact: entity.ImageArtifact{
			Ref: "imgart_12345678", RecipeRef: recipe.Recipe.Ref, SpecSHA256: digest,
			BuildRef: "imgbld_12345678", StagingReference: "registry.internal/staging@" + manifest,
			ManifestDigest: manifest, ImmutableBuildSHA256: digest, ProvenanceSHA256: digest,
			SourceSHA256: digest, ContextSHA256: digest, BuilderSHA256: digest,
			FrontendSHA256: digest, ToolchainSHA256: digest, PolicySHA256: digest,
			SBOMSHA256: digest, VulnerabilityEvidenceSHA256: digest,
			AdmissionVerdict: "ACCEPTED", SignatureIdentity: "spiffe://kodex.local/image-admission",
			SignatureSHA256: digest, AdmissionReceiptSHA256: digest,
			AdmissionReceiptOCIManifestDigest: manifest, RoleRuntimeContractSHA256: digest,
			Version: 7, RecipeVersion: recipe.Recipe.Version, RecipeGeneration: recipe.Recipe.Generation,
			BuildVersion: 6, PolicyRevision: recipe.Recipe.PolicyRevision, AdmissionRevision: 1,
			RoleRuntimeContractRevision: recipe.Recipe.RoleRuntimeContractRevision, BuildAttempt: 1,
			Platforms: []entity.RoleImagePlatform{{OS: "linux", Architecture: "amd64"}},
			Tools:     []entity.RoleImageTool{{Name: "codex", Version: "1", SourceRef: "oci://tool", SHA256: digest}},
		},
	}
	request := lockedPromotionRequest{
		ID: artifact.PromotionRequestID, Ref: "imgprom_12345678", RequestedBy: "subject-id",
		ExpectedProvenanceSHA256: artifact.Artifact.ProvenanceSHA256,
		ManifestDigest:           artifact.Artifact.ManifestDigest, State: "QUEUED",
	}
	request.ReceiptSHA256 = promotionRequestDigest(request.Ref, request.RequestedBy, artifact)
	return recipe, artifact, request
}
