// Package imageowner предоставляет отдельные admission и promotion adapters.
package imageowner

import (
	"context"
	"errors"
	"time"

	controlplanev1 "github.com/codex-k8s/matter-codex/libs/go/controlplaneapi/gen/controlplane/v1"
	sharedclient "github.com/codex-k8s/matter-codex/libs/go/controlplaneclient"
)

type Config struct {
	Target, TLSServerName, CAFile, ClientCertificateFile, ClientPrivateKeyFile string
	ApplicationGrantFile                                                       string
	ExpectedIssuerUID, ExpectedIssuerGID                                       uint32
	DialTimeout, RPCDeadline                                                   time.Duration
	Promotion                                                                  bool
}

type Claim struct {
	ArtifactID                  string    `json:"artifactId"`
	Version                     uint64    `json:"version"`
	Fence                       uint64    `json:"fence"`
	ClaimToken                  string    `json:"claimToken"`
	ExpiresAt                   time.Time `json:"expiresAt"`
	RecipeID                    string    `json:"recipeId"`
	RecipeVersion               uint64    `json:"recipeVersion"`
	RecipeGeneration            uint64    `json:"recipeGeneration"`
	SpecSHA256                  string    `json:"specSHA256"`
	BuildID                     string    `json:"buildId"`
	BuildVersion                uint64    `json:"buildVersion"`
	BuildAttempt                uint32    `json:"buildAttempt"`
	StagingReference            string    `json:"stagingReference"`
	ManifestDigest              string    `json:"manifestDigest"`
	ImmutableBuildSHA256        string    `json:"immutableBuildSHA256"`
	ProvenanceSHA256            string    `json:"provenanceSHA256"`
	BaseImageDigest             string    `json:"baseImageDigest"`
	SourceSHA256                string    `json:"sourceSHA256"`
	ContextSHA256               string    `json:"contextSHA256"`
	BuilderSHA256               string    `json:"builderSHA256"`
	FrontendSHA256              string    `json:"frontendSHA256"`
	ToolchainSHA256             string    `json:"toolchainSHA256"`
	RoleRuntimeContractRevision uint64    `json:"roleRuntimeContractRevision"`
	RoleRuntimeContractSHA256   string    `json:"roleRuntimeContractSHA256"`
	Platforms                   []string  `json:"platforms"`
	PolicyRevision              uint64    `json:"policyRevision"`
	PolicySHA256                string    `json:"policySHA256"`
}

type AdmissionEvidence struct {
	SBOMSHA256, VulnerabilityEvidenceSHA256, SignatureIdentity, SignatureSHA256 string
	AdmissionReceiptSHA256                                                      string
	AdmissionReceiptOCIManifestDigest                                           string
	Accepted                                                                    bool
}

type Promotion struct {
	ArtifactID                        string    `json:"artifactId"`
	Version                           uint64    `json:"version"`
	Claim                             string    `json:"claim"`
	Fence                             uint64    `json:"fence"`
	ExpiresAt                         time.Time `json:"expiresAt"`
	StagingReference                  string    `json:"stagingReference"`
	ManifestDigest                    string    `json:"manifestDigest"`
	AdmissionRevision                 uint64    `json:"admissionRevision"`
	AdmissionReceiptSHA256            string    `json:"admissionReceiptSHA256"`
	AdmissionReceiptOCIManifestDigest string    `json:"admissionReceiptOCIManifestDigest"`
	PromotedReference                 string    `json:"promotedReference,omitempty"`
	ReadbackSHA256                    string    `json:"readbackSHA256,omitempty"`
	AuthorizationToken                string    `json:"authorizationToken,omitempty"`
	AuthorizationExpiresAt            time.Time `json:"authorizationExpiresAt,omitempty"`
}

type Client struct {
	shared      *sharedclient.Client
	rpcDeadline time.Duration
}

func Dial(ctx context.Context, config Config) (*Client, error) {
	operations := sharedclient.ImageAdmissionOperations()
	if config.Promotion {
		operations = sharedclient.ImagePromotionOperations()
	}
	client, err := sharedclient.Dial(ctx, sharedclient.Config{
		Target: config.Target, TLSServerName: config.TLSServerName, CAFile: config.CAFile,
		ClientCertificateFile: config.ClientCertificateFile, ClientPrivateKeyFile: config.ClientPrivateKeyFile,
		ApplicationGrantFile: config.ApplicationGrantFile, ExpectedIssuerUID: config.ExpectedIssuerUID,
		ExpectedIssuerGID: config.ExpectedIssuerGID, DialTimeout: config.DialTimeout, Operations: operations,
	})
	if err != nil {
		return nil, err
	}
	return &Client{shared: client, rpcDeadline: config.RPCDeadline}, nil
}

func (client *Client) Check(ctx context.Context) error {
	callCtx, cancel := context.WithTimeout(ctx, client.rpcDeadline)
	defer cancel()
	return client.shared.Check(callCtx)
}

func (client *Client) Claim(ctx context.Context, key string) (Claim, error) {
	callCtx, cancel := context.WithTimeout(ctx, client.rpcDeadline)
	defer cancel()
	response, err := client.shared.ControlPlane.ClaimImageAdmission(callCtx,
		&controlplanev1.ClaimImageAdmissionRequest{IdempotencyKey: key})
	if err != nil {
		return Claim{}, err
	}
	resource := response.GetImageArtifact()
	spec := resource.GetSpec().GetImageArtifact()
	if resource == nil || spec == nil || resource.GetVersion() == 0 || response.GetClaimToken() == "" ||
		response.GetFence() == 0 || response.GetClaimExpiresAt() == nil || len(spec.GetPlatforms()) == 0 {
		return Claim{}, errors.New("image admission claim is incomplete")
	}
	platforms := make([]string, 0, len(spec.GetPlatforms()))
	for _, platform := range spec.GetPlatforms() {
		value := platform.GetOs() + "/" + platform.GetArchitecture()
		if platform.GetVariant() != "" {
			value += "/" + platform.GetVariant()
		}
		platforms = append(platforms, value)
	}
	return Claim{ArtifactID: resource.GetId(), Version: resource.GetVersion(), Fence: response.GetFence(),
		ClaimToken: response.GetClaimToken(), ExpiresAt: response.GetClaimExpiresAt().AsTime(),
		RecipeID: spec.GetRecipeId(), RecipeVersion: spec.GetRecipeVersion(), RecipeGeneration: spec.GetRecipeGeneration(),
		SpecSHA256: spec.GetSpecSha256(), BuildID: spec.GetBuildId(), BuildVersion: spec.GetBuildVersion(),
		BuildAttempt: spec.GetBuildAttempt(), StagingReference: spec.GetStagingReference(),
		ManifestDigest: spec.GetManifestDigest(), ImmutableBuildSHA256: spec.GetImmutableBuildSha256(),
		ProvenanceSHA256: spec.GetProvenanceSha256(), PolicyRevision: spec.GetPolicyRevision(),
		PolicySHA256: spec.GetPolicySha256(), BaseImageDigest: spec.GetBaseImageDigest(),
		SourceSHA256: spec.GetSourceSha256(), ContextSHA256: spec.GetContextSha256(),
		BuilderSHA256: spec.GetBuilderSha256(), FrontendSHA256: spec.GetFrontendSha256(),
		ToolchainSHA256: spec.GetToolchainSha256(), Platforms: platforms,
		RoleRuntimeContractRevision: spec.GetRoleRuntimeContractRevision(),
		RoleRuntimeContractSHA256:   spec.GetRoleRuntimeContractSha256()}, nil
}

func (client *Client) Record(ctx context.Context, key string, claim Claim, evidence AdmissionEvidence) error {
	verdict := controlplanev1.ImageAdmissionVerdict_IMAGE_ADMISSION_VERDICT_REJECTED
	if evidence.Accepted {
		verdict = controlplanev1.ImageAdmissionVerdict_IMAGE_ADMISSION_VERDICT_ACCEPTED
	}
	callCtx, cancel := context.WithTimeout(ctx, client.rpcDeadline)
	defer cancel()
	response, err := client.shared.ControlPlane.RecordImageAdmission(callCtx, &controlplanev1.RecordImageAdmissionRequest{
		IdempotencyKey: key, ImageArtifactId: claim.ArtifactID, ExpectedVersion: claim.Version,
		ExpectedFence: claim.Fence, ClaimToken: claim.ClaimToken, ManifestDigest: claim.ManifestDigest,
		ImmutableBuildSha256: claim.ImmutableBuildSHA256, ProvenanceSha256: claim.ProvenanceSHA256,
		SbomSha256: evidence.SBOMSHA256, VulnerabilityEvidenceSha256: evidence.VulnerabilityEvidenceSHA256,
		PolicyRevision: claim.PolicyRevision, PolicySha256: claim.PolicySHA256, Verdict: verdict,
		SignatureIdentity: evidence.SignatureIdentity, SignatureSha256: evidence.SignatureSHA256,
		AdmissionReceiptSha256:            evidence.AdmissionReceiptSHA256,
		AdmissionReceiptOciManifestDigest: evidence.AdmissionReceiptOCIManifestDigest,
	})
	if err != nil {
		return err
	}
	artifact := response.GetImageArtifact()
	if artifact == nil || artifact.GetVersion() <= claim.Version {
		return errors.New("recorded image admission response is incomplete")
	}
	return nil
}

func (client *Client) ClaimPromotion(ctx context.Context, key string) (Promotion, error) {
	callCtx, cancel := context.WithTimeout(ctx, client.rpcDeadline)
	defer cancel()
	response, err := client.shared.ControlPlane.ClaimImagePromotion(callCtx,
		&controlplanev1.ClaimImagePromotionRequest{IdempotencyKey: key})
	if err != nil {
		return Promotion{}, err
	}
	artifact := response.GetImageArtifact()
	spec := artifact.GetSpec().GetImageArtifact()
	if artifact == nil || spec == nil || artifact.GetId() == "" || artifact.GetVersion() == 0 || response.GetPromotionClaim() == "" ||
		response.GetFence() == 0 || response.GetAuthorityGeneration() == 0 || response.GetClaimExpiresAt() == nil ||
		spec.GetStagingReference() == "" || spec.GetManifestDigest() == "" || spec.GetAdmissionRevision() == 0 ||
		spec.GetAdmissionReceiptSha256() == "" || spec.GetAdmissionReceiptOciManifestDigest() == "" {
		return Promotion{}, errors.New("image promotion claim is incomplete")
	}
	return Promotion{ArtifactID: artifact.GetId(), Version: artifact.GetVersion(),
		Claim: response.GetPromotionClaim(), Fence: response.GetFence(),
		ExpiresAt: response.GetClaimExpiresAt().AsTime(), StagingReference: spec.GetStagingReference(),
		ManifestDigest: spec.GetManifestDigest(), AdmissionRevision: spec.GetAdmissionRevision(),
		AdmissionReceiptSHA256:            spec.GetAdmissionReceiptSha256(),
		AdmissionReceiptOCIManifestDigest: spec.GetAdmissionReceiptOciManifestDigest()}, nil
}

func (client *Client) Complete(ctx context.Context, key string, promotion Promotion) error {
	callCtx, cancel := context.WithTimeout(ctx, client.rpcDeadline)
	defer cancel()
	response, err := client.shared.ControlPlane.CompleteImagePromotion(callCtx, &controlplanev1.CompleteImagePromotionRequest{
		IdempotencyKey: key, ImageArtifactId: promotion.ArtifactID, ExpectedVersion: promotion.Version,
		AuthorizationToken: promotion.AuthorizationToken, PromotedReference: promotion.PromotedReference,
		ManifestDigest: promotion.ManifestDigest, PromotionReadbackSha256: promotion.ReadbackSHA256,
	})
	if err != nil {
		return err
	}
	if response.GetImageArtifact() == nil {
		return errors.New("completed image promotion response is incomplete")
	}
	return nil
}

func (client *Client) AuthorizePromotion(ctx context.Context, key string, promotion *Promotion) error {
	callCtx, cancel := context.WithTimeout(ctx, client.rpcDeadline)
	defer cancel()
	response, err := client.shared.ControlPlane.AuthorizeImagePromotion(callCtx,
		&controlplanev1.AuthorizeImagePromotionRequest{IdempotencyKey: key,
			ImageArtifactId: promotion.ArtifactID, ExpectedVersion: promotion.Version,
			PromotionClaim: promotion.Claim, ManifestDigest: promotion.ManifestDigest})
	if err != nil {
		return err
	}
	artifact := response.GetImageArtifact()
	if artifact == nil || artifact.GetVersion() <= promotion.Version || response.GetAuthorizationToken() == "" ||
		response.GetAuthorizationExpiresAt() == nil {
		return errors.New("image promotion authorization response is incomplete")
	}
	promotion.Version = artifact.GetVersion()
	promotion.AuthorizationToken = response.GetAuthorizationToken()
	promotion.AuthorizationExpiresAt = response.GetAuthorizationExpiresAt().AsTime()
	promotion.Claim = ""
	return nil
}

func (client *Client) Close() error { return client.shared.Close() }
