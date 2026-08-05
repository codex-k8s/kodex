package resource

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/errs"
	domainrepo "github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/repository/controlplane"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/enum"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/value"
	"github.com/google/uuid"
)

type RoleImageRecipeAction string

const (
	RoleImageRecipeCreate       RoleImageRecipeAction = "CREATE"
	RoleImageRecipeUpdate       RoleImageRecipeAction = "UPDATE"
	RoleImageRecipeArchive      RoleImageRecipeAction = "ARCHIVE"
	RoleImageRecipeRestore      RoleImageRecipeAction = "RESTORE"
	RoleImageRecipeDelete       RoleImageRecipeAction = "DELETE"
	RoleImageRecipeRequestBuild RoleImageRecipeAction = "REQUEST_BUILD"
)

type ManageRoleImageRecipeInput struct {
	Principal       value.Principal
	IdempotencyKey  string
	Action          RoleImageRecipeAction
	RecipeID        string
	ExpectedVersion uint64
	Name            string
	Input           *entity.RoleImageRecipeInput
}

type ManageRoleImageRecipeResult struct {
	Recipe        entity.Resource
	ImageBuild    entity.Resource
	ImageArtifact entity.Resource
	Reused        bool
}

func (service *Service) ManageRoleImageRecipe(
	ctx context.Context,
	input ManageRoleImageRecipeInput,
) (ManageRoleImageRecipeResult, error) {
	if err := authorize(input.Principal, permissionManageRoleImageRecipe); err != nil {
		return ManageRoleImageRecipeResult{}, err
	}
	if input.Principal.CallerWorkload != controlAPIGatewayWorkload ||
		input.Principal.CallerSPIFFEID != controlAPIGatewaySPIFFEID ||
		value.ValidateIdempotencyKey(input.IdempotencyKey) != nil ||
		!validRoleImageRecipeAction(input.Action) {
		return ManageRoleImageRecipeResult{}, errs.ErrInvalidInput
	}
	if input.Action == RoleImageRecipeCreate {
		if input.RecipeID != "" || input.ExpectedVersion != 0 || value.ValidateName(input.Name) != nil ||
			input.Input == nil || input.Input.Validate() != nil {
			return ManageRoleImageRecipeResult{}, errs.ErrInvalidInput
		}
	} else if value.ValidateID(input.RecipeID) != nil || input.ExpectedVersion == 0 {
		return ManageRoleImageRecipeResult{}, errs.ErrInvalidInput
	}
	if input.Action == RoleImageRecipeUpdate &&
		(value.ValidateName(input.Name) != nil || input.Input == nil || input.Input.Validate() != nil) {
		return ManageRoleImageRecipeResult{}, errs.ErrInvalidInput
	}
	if input.Action != RoleImageRecipeCreate && input.Action != RoleImageRecipeUpdate &&
		(input.Input != nil || input.Name != "") {
		return ManageRoleImageRecipeResult{}, errs.ErrInvalidInput
	}

	normalized := entity.RoleImageRecipeInput{}
	if input.Input != nil {
		normalized = canonicalRoleImageInput(*input.Input)
	}
	requestHash, err := canonicalHash(struct {
		Identity        commandIdentity
		Action          RoleImageRecipeAction
		RecipeID        string
		ExpectedVersion uint64
		Name            string
		Input           entity.RoleImageRecipeInput
	}{identity(input.Principal), input.Action, input.RecipeID, input.ExpectedVersion, input.Name, normalized})
	if err != nil {
		return ManageRoleImageRecipeResult{}, errs.ErrInvalidInput
	}

	recipe, err := service.withResourceReceipt(ctx, input.Principal, input.IdempotencyKey,
		"manage_role_image_recipe_"+strings.ToLower(string(input.Action)), requestHash,
		func(tx domainrepo.Transaction) (entity.Resource, error) {
			imageTx, ok := tx.(domainrepo.ImageTransaction)
			if !ok {
				return entity.Resource{}, errs.ErrInternal
			}
			now, err := tx.CurrentTime(ctx)
			if err != nil {
				return entity.Resource{}, err
			}
			now = now.UTC().Truncate(time.Microsecond)
			if input.Action == RoleImageRecipeCreate {
				spec, specErr := service.newRoleImageRecipeSpec(normalized, 1)
				if specErr != nil {
					return entity.Resource{}, specErr
				}
				created, createErr := entity.New(uuid.NewString(), input.Principal.OrganizationID,
					input.Principal.ProjectID, "", input.Principal.ActorID, enum.KindRoleImageRecipe,
					input.Name, spec, now)
				if createErr != nil {
					return entity.Resource{}, errs.ErrInvalidInput
				}
				if err := tx.Insert(ctx, created); err != nil {
					return entity.Resource{}, err
				}
				if err := service.appendMutationRecords(ctx, tx, input.Principal, "create_role_image_recipe", created); err != nil {
					return entity.Resource{}, err
				}
				if _, reuseErr := imageTx.PromotedImageArtifactBySpec(ctx, created.OrganizationID,
					created.ProjectID, created.OwnerActorID, spec.SpecSHA256, spec.PolicyRevision, spec.PolicySHA256); reuseErr == nil {
					return created, nil
				} else if !errors.Is(reuseErr, errs.ErrNotFound) {
					return entity.Resource{}, reuseErr
				}
				if _, err := service.insertImageBuild(ctx, tx, input.Principal, created, spec, now); err != nil {
					return entity.Resource{}, err
				}
				return created, nil
			}

			current, err := tx.GetForUpdate(ctx, input.Principal.OrganizationID,
				input.Principal.ProjectID, input.RecipeID)
			if err != nil {
				return entity.Resource{}, err
			}
			currentSpec, ok := current.Spec.(entity.RoleImageRecipeSpec)
			if !ok || current.Kind != enum.KindRoleImageRecipe || current.OwnerActorID != input.Principal.ActorID {
				return entity.Resource{}, errs.ErrNotFound
			}
			if current.Version != input.ExpectedVersion {
				return entity.Resource{}, errs.ErrVersionMismatch
			}
			switch input.Action {
			case RoleImageRecipeUpdate:
				if err := service.cancelRecipeWork(ctx, tx, imageTx, input.Principal, current.ID, now); err != nil {
					return entity.Resource{}, err
				}
				nextSpec, specErr := service.newRoleImageRecipeSpec(normalized, currentSpec.Generation+1)
				if specErr != nil {
					return entity.Resource{}, specErr
				}
				updated, updateErr := current.Update(input.Name, nextSpec, now)
				if updateErr != nil {
					return entity.Resource{}, errs.ErrStateConflict
				}
				if err := tx.Update(ctx, updated, current.Version); err != nil {
					return entity.Resource{}, err
				}
				if err := service.appendMutationRecords(ctx, tx, input.Principal, "update_role_image_recipe", updated); err != nil {
					return entity.Resource{}, err
				}
				if _, reuseErr := imageTx.PromotedImageArtifactBySpec(ctx, updated.OrganizationID,
					updated.ProjectID, updated.OwnerActorID, nextSpec.SpecSHA256, nextSpec.PolicyRevision, nextSpec.PolicySHA256); reuseErr == nil {
					return updated, nil
				} else if !errors.Is(reuseErr, errs.ErrNotFound) {
					return entity.Resource{}, reuseErr
				}
				if _, err := service.insertImageBuild(ctx, tx, input.Principal, updated, nextSpec, now); err != nil {
					return entity.Resource{}, err
				}
				return updated, nil
			case RoleImageRecipeRequestBuild:
				if current.State != enum.StateActive {
					return entity.Resource{}, errs.ErrStateConflict
				}
				if _, reuseErr := imageTx.PromotedImageArtifactBySpec(ctx, current.OrganizationID,
					current.ProjectID, current.OwnerActorID, currentSpec.SpecSHA256, currentSpec.PolicyRevision, currentSpec.PolicySHA256); reuseErr == nil {
					return current, nil
				} else if !errors.Is(reuseErr, errs.ErrNotFound) {
					return entity.Resource{}, reuseErr
				}
				if err := service.cancelRecipeWork(ctx, tx, imageTx, input.Principal, current.ID, now); err != nil {
					return entity.Resource{}, err
				}
				nextSpec := currentSpec
				nextSpec.Generation++
				updated, updateErr := current.Update(current.Name, nextSpec, now)
				if updateErr != nil {
					return entity.Resource{}, errs.ErrStateConflict
				}
				if err := tx.Update(ctx, updated, current.Version); err != nil {
					return entity.Resource{}, err
				}
				if err := service.appendMutationRecords(ctx, tx, input.Principal, "request_role_image_build", updated); err != nil {
					return entity.Resource{}, err
				}
				if _, err := service.insertImageBuild(ctx, tx, input.Principal, updated, nextSpec, now); err != nil {
					return entity.Resource{}, err
				}
				return updated, nil
			case RoleImageRecipeRestore:
				nextSpec := currentSpec
				reused := false
				if _, reuseErr := imageTx.PromotedImageArtifactBySpec(ctx, current.OrganizationID,
					current.ProjectID, current.OwnerActorID, currentSpec.SpecSHA256, currentSpec.PolicyRevision, currentSpec.PolicySHA256); reuseErr == nil {
					reused = true
				} else if !errors.Is(reuseErr, errs.ErrNotFound) {
					return entity.Resource{}, reuseErr
				}
				if !reused {
					if nextSpec.Generation == ^uint64(0) {
						return entity.Resource{}, errs.ErrStateConflict
					}
					nextSpec.Generation++
				}
				updated, transitionErr := current.ReplaceAndTransition(nextSpec, enum.StateActive, now)
				if transitionErr != nil {
					return entity.Resource{}, errs.ErrStateConflict
				}
				if err := tx.Update(ctx, updated, current.Version); err != nil {
					return entity.Resource{}, err
				}
				if err := service.appendMutationRecords(ctx, tx, input.Principal, "restore_role_image_recipe", updated); err != nil {
					return entity.Resource{}, err
				}
				if !reused {
					if _, err := service.insertImageBuild(ctx, tx, input.Principal, updated, nextSpec, now); err != nil {
						return entity.Resource{}, err
					}
				}
				return updated, nil
			case RoleImageRecipeArchive, RoleImageRecipeDelete:
				if err := service.cancelRecipeWork(ctx, tx, imageTx, input.Principal, current.ID, now); err != nil {
					return entity.Resource{}, err
				}
				target := enum.StateArchived
				action := "archive_role_image_recipe"
				if input.Action == RoleImageRecipeDelete {
					target, action = enum.StateDeletionPending, "delete_role_image_recipe"
					if current.State == enum.StateDeletionPending {
						target = enum.StateDeleted
					}
				}
				updated, transitionErr := current.Transition(target, now)
				if transitionErr != nil {
					return entity.Resource{}, errs.ErrStateConflict
				}
				if err := tx.Update(ctx, updated, current.Version); err != nil {
					return entity.Resource{}, err
				}
				return updated, service.appendMutationRecords(ctx, tx, input.Principal, action, updated)
			default:
				return entity.Resource{}, errs.ErrInvalidInput
			}
		})
	if err != nil {
		return ManageRoleImageRecipeResult{}, err
	}
	return service.resolveRoleImageRecipeResult(ctx, recipe)
}

func (service *Service) newRoleImageRecipeSpec(
	input entity.RoleImageRecipeInput,
	generation uint64,
) (entity.RoleImageRecipeSpec, error) {
	if input.BaseImageReference != service.trustedRoleBaseRepository || input.BaseImageDigest != service.trustedRoleBaseDigest {
		return entity.RoleImageRecipeSpec{}, errs.ErrInvalidInput
	}
	if !exactRoleImageInputRef(input.ContextRef, service.roleImageInputRepository) {
		return entity.RoleImageRecipeSpec{}, errs.ErrInvalidInput
	}
	for _, item := range input.Packages {
		if !exactRoleImageInputRef(item.SourceRef, service.roleImageInputRepository) {
			return entity.RoleImageRecipeSpec{}, errs.ErrInvalidInput
		}
	}
	for _, item := range input.Tools {
		if !exactRoleImageInputRef(item.SourceRef, service.roleImageInputRepository) {
			return entity.RoleImageRecipeSpec{}, errs.ErrInvalidInput
		}
	}
	specSHA256, err := canonicalHash(struct {
		Input                   entity.RoleImageRecipeInput
		PolicyRevision          uint64
		PolicySHA256            string
		RuntimeContractRevision uint64
		RuntimeContractSHA256   string
	}{input, service.imagePolicyRevision, service.imagePolicySHA256,
		service.roleRuntimeContractRevision, service.roleRuntimeContractSHA256})
	if err != nil {
		return entity.RoleImageRecipeSpec{}, errs.ErrInvalidInput
	}
	spec := entity.RoleImageRecipeSpec{Input: input, Generation: generation,
		SpecSHA256: specSHA256, PolicyRevision: service.imagePolicyRevision,
		PolicySHA256:                service.imagePolicySHA256,
		RoleRuntimeContractRevision: service.roleRuntimeContractRevision,
		RoleRuntimeContractSHA256:   service.roleRuntimeContractSHA256}
	if spec.Validate() != nil {
		return entity.RoleImageRecipeSpec{}, errs.ErrInvalidInput
	}
	return spec, nil
}

func exactRoleImageInputRef(reference, repository string) bool {
	prefix := "oci://" + repository + "@sha256:"
	return strings.HasPrefix(reference, prefix) && len(reference) == len(prefix)+64
}

func (service *Service) insertImageBuild(
	ctx context.Context,
	tx domainrepo.Transaction,
	principal value.Principal,
	recipe entity.Resource,
	recipeSpec entity.RoleImageRecipeSpec,
	now time.Time,
) (entity.Resource, error) {
	immutableBuildSHA256, err := canonicalHash(struct {
		RecipeID         string
		RecipeVersion    uint64
		RecipeGeneration uint64
		SpecSHA256       string
		Input            entity.RoleImageRecipeInput
	}{recipe.ID, recipe.Version, recipeSpec.Generation, recipeSpec.SpecSHA256, recipeSpec.Input})
	if err != nil {
		return entity.Resource{}, errs.ErrInternal
	}
	buildID := uuid.NewSHA1(uuid.NameSpaceOID, []byte("role-image-build\x00"+recipe.ID+"\x00"+
		strconv.FormatUint(recipeSpec.Generation, 10)+"\x00"+recipeSpec.SpecSHA256)).String()
	spec := entity.ImageBuildSpec{
		RecipeID: recipe.ID, RecipeVersion: recipe.Version, RecipeGeneration: recipeSpec.Generation,
		SpecSHA256: recipeSpec.SpecSHA256, Attempt: 1, Stage: entity.ImageBuildStageQueued,
		ImmutableBuildSHA256: immutableBuildSHA256, AvailableAt: now, MaximumAttempts: service.imageMaximumAttempts,
	}
	build, err := entity.New(buildID, recipe.OrganizationID, recipe.ProjectID, recipe.ID,
		recipe.OwnerActorID, enum.KindImageBuild, "Сборка "+recipe.Name, spec, now)
	if err != nil {
		return entity.Resource{}, errs.ErrInternal
	}
	if err := tx.Insert(ctx, build); err != nil {
		return entity.Resource{}, err
	}
	return build, service.appendMutationRecords(ctx, tx, principal, "queue_image_build", build)
}

func (service *Service) cancelRecipeWork(
	ctx context.Context,
	tx domainrepo.Transaction,
	imageTx domainrepo.ImageTransaction,
	principal value.Principal,
	recipeID string,
	now time.Time,
) error {
	builds, err := imageTx.ImageBuildsForRecipeForUpdate(ctx, principal.OrganizationID, principal.ProjectID, recipeID)
	if err != nil {
		return err
	}
	for _, build := range builds {
		spec, ok := build.Spec.(entity.ImageBuildSpec)
		if !ok {
			return errs.ErrStateConflict
		}
		clearImageBuildClaim(&spec)
		spec.Stage = entity.ImageBuildStageCancelled
		spec.ErrorCode = "CANCELLED_BY_OWNER"
		updated, transitionErr := build.ReplaceAndTransition(spec, enum.StateCancelled, now)
		if transitionErr != nil {
			return errs.ErrStateConflict
		}
		if err := tx.Update(ctx, updated, build.Version); err != nil {
			return err
		}
		if err := service.appendMutationRecords(ctx, tx, principal, "cancel_image_build", updated); err != nil {
			return err
		}
	}
	artifacts, err := imageTx.ImageArtifactsForRecipeForUpdate(ctx, principal.OrganizationID,
		principal.ProjectID, recipeID)
	if err != nil {
		return err
	}
	for _, artifact := range artifacts {
		spec, ok := artifact.Spec.(entity.ImageArtifactSpec)
		if !ok || artifact.OwnerActorID != principal.ActorID {
			return errs.ErrStateConflict
		}
		spec.AdmissionClaimantWorkloadID, spec.AdmissionClaimTokenSHA256 = "", ""
		spec.AdmissionClaimExpiresAt = time.Time{}
		clearPromotionClaim(&spec)
		updated, transitionErr := artifact.ReplaceAndTransition(spec, enum.StateCancelled, now)
		if transitionErr != nil {
			return errs.ErrStateConflict
		}
		if err := tx.Update(ctx, updated, artifact.Version); err != nil {
			return err
		}
		if err := service.appendMutationRecords(ctx, tx, principal, "cancel_image_artifact", updated); err != nil {
			return err
		}
	}
	return nil
}

func (service *Service) resolveRoleImageRecipeResult(
	ctx context.Context,
	recipe entity.Resource,
) (ManageRoleImageRecipeResult, error) {
	result := ManageRoleImageRecipeResult{Recipe: recipe}
	if recipe.State != enum.StateActive {
		return result, nil
	}
	spec, ok := recipe.Spec.(entity.RoleImageRecipeSpec)
	if !ok {
		return ManageRoleImageRecipeResult{}, errs.ErrInternal
	}
	err := service.repository.Transact(ctx, domainrepo.Scope{OrganizationID: recipe.OrganizationID,
		ProjectID: recipe.ProjectID, ActorID: recipe.OwnerActorID}, func(tx domainrepo.Transaction) error {
		imageTx, ok := tx.(domainrepo.ImageTransaction)
		if !ok {
			return errs.ErrInternal
		}
		artifact, artifactErr := imageTx.PromotedImageArtifactBySpec(ctx, recipe.OrganizationID,
			recipe.ProjectID, recipe.OwnerActorID, spec.SpecSHA256, spec.PolicyRevision, spec.PolicySHA256)
		if artifactErr == nil {
			result.ImageArtifact, result.Reused = artifact, true
			return nil
		}
		if !errors.Is(artifactErr, errs.ErrNotFound) {
			return artifactErr
		}
		buildID := uuid.NewSHA1(uuid.NameSpaceOID, []byte("role-image-build\x00"+recipe.ID+"\x00"+
			strconv.FormatUint(spec.Generation, 10)+"\x00"+spec.SpecSHA256)).String()
		build, buildErr := tx.Get(ctx, recipe.OrganizationID, recipe.ProjectID, buildID)
		if buildErr == nil {
			result.ImageBuild = build
			return nil
		}
		return buildErr
	})
	return result, err
}

type ClaimImageBuildInput struct {
	Principal      value.Principal
	IdempotencyKey string
}

type ImageBuildClaim struct {
	ImageBuild                  entity.Resource
	RecipeInput                 entity.RoleImageRecipeInput
	PolicyRevision              uint64
	PolicySHA256                string
	RoleRuntimeContractRevision uint64
	RoleRuntimeContractSHA256   string
	LeaseToken                  string
	Fence                       uint64
	AuthorityGeneration         uint64
	LeaseExpiresAt              time.Time
}

func (service *Service) ClaimImageBuild(ctx context.Context, input ClaimImageBuildInput) (ImageBuildClaim, error) {
	if err := authorizeImageWorkload(input.Principal, permissionClaimImageBuild,
		service.imageBuilderWorkload, service.imageBuilderSPIFFEID); err != nil {
		return ImageBuildClaim{}, err
	}
	if value.ValidateIdempotencyKey(input.IdempotencyKey) != nil || input.Principal.ProjectID == "" {
		return ImageBuildClaim{}, errs.ErrInvalidInput
	}
	requestHash, _ := canonicalHash(struct {
		Identity commandIdentity
		Key      string
	}{identity(input.Principal), hashString(input.IdempotencyKey)})
	build, err := service.withResourceReceipt(ctx, input.Principal, input.IdempotencyKey,
		"claim_image_build", requestHash, func(tx domainrepo.Transaction) (entity.Resource, error) {
			imageTx, ok := tx.(domainrepo.ImageTransaction)
			if !ok {
				return entity.Resource{}, errs.ErrInternal
			}
			now, err := tx.CurrentTime(ctx)
			if err != nil {
				return entity.Resource{}, err
			}
			candidate, err := imageTx.NextImageBuild(ctx, input.Principal.OrganizationID, input.Principal.ProjectID, now)
			if err != nil {
				return entity.Resource{}, err
			}
			spec, ok := candidate.Spec.(entity.ImageBuildSpec)
			if !ok || candidate.State != enum.StateQueued || spec.Stage != entity.ImageBuildStageQueued {
				return entity.Resource{}, errs.ErrStateConflict
			}
			recipe, err := tx.GetForUpdate(ctx, candidate.OrganizationID, candidate.ProjectID, spec.RecipeID)
			if err != nil {
				return entity.Resource{}, err
			}
			recipeSpec, ok := recipe.Spec.(entity.RoleImageRecipeSpec)
			if !ok || recipe.State != enum.StateActive || recipe.Version != spec.RecipeVersion ||
				recipeSpec.Generation != spec.RecipeGeneration || recipeSpec.SpecSHA256 != spec.SpecSHA256 ||
				recipeSpec.PolicyRevision != service.imagePolicyRevision || recipeSpec.PolicySHA256 != service.imagePolicySHA256 {
				return entity.Resource{}, errs.ErrStateConflict
			}
			spec.AuthorityGeneration++
			if spec.AuthorityGeneration < input.Principal.AuthorityGeneration {
				spec.AuthorityGeneration = input.Principal.AuthorityGeneration
			}
			spec.Fence++
			spec.ClaimantWorkloadID = input.Principal.CallerWorkload
			spec.ClaimantSPIFFEID = input.Principal.CallerSPIFFEID
			spec.LeaseExpiresAt = now.Add(service.imageBuildLeaseDuration).UTC().Truncate(time.Microsecond)
			token := service.imageLeaseToken(candidate.ID, spec.Attempt, spec.Fence,
				spec.AuthorityGeneration, input.IdempotencyKey)
			spec.LeaseTokenSHA256 = hashString(token)
			spec.ClaimJTISHA256 = hashString(candidate.ID + "\x00" + strconv.FormatUint(uint64(spec.Attempt), 10) +
				"\x00" + strconv.FormatUint(spec.Fence, 10) + "\x00" + hashString(input.IdempotencyKey))
			updated, err := candidate.ReplaceAndTransition(spec, enum.StateClaimed, now)
			if err != nil {
				return entity.Resource{}, errs.ErrStateConflict
			}
			if err := tx.Update(ctx, updated, candidate.Version); err != nil {
				return entity.Resource{}, err
			}
			return updated, service.appendMutationRecords(ctx, tx, input.Principal, "claim_image_build", updated)
		})
	if err != nil {
		return ImageBuildClaim{}, err
	}
	current, currentErr := service.repository.Get(ctx, build.OrganizationID, build.ProjectID, build.ID, enum.KindImageBuild)
	if currentErr != nil || current.Version != build.Version {
		return ImageBuildClaim{}, errs.ErrIdempotencyConflict
	}
	return service.imageBuildClaimReadback(ctx, input.Principal, build, input.IdempotencyKey)
}

func (service *Service) imageBuildClaimReadback(
	ctx context.Context,
	principal value.Principal,
	build entity.Resource,
	idempotencyKey string,
) (ImageBuildClaim, error) {
	spec, ok := build.Spec.(entity.ImageBuildSpec)
	if !ok || (build.State != enum.StateClaimed && build.State != enum.StateRunning) ||
		spec.ClaimantWorkloadID != principal.CallerWorkload || spec.ClaimantSPIFFEID != principal.CallerSPIFFEID {
		return ImageBuildClaim{}, errs.ErrStateConflict
	}
	recipe, err := service.repository.Get(ctx, build.OrganizationID, build.ProjectID,
		spec.RecipeID, enum.KindRoleImageRecipe)
	if err != nil {
		return ImageBuildClaim{}, err
	}
	recipeSpec, ok := recipe.Spec.(entity.RoleImageRecipeSpec)
	if !ok || recipe.Version != spec.RecipeVersion || recipeSpec.Generation != spec.RecipeGeneration ||
		recipeSpec.SpecSHA256 != spec.SpecSHA256 {
		return ImageBuildClaim{}, errs.ErrStateConflict
	}
	token := service.imageLeaseToken(build.ID, spec.Attempt, spec.Fence, spec.AuthorityGeneration, idempotencyKey)
	if hashString(token) != spec.LeaseTokenSHA256 {
		return ImageBuildClaim{}, errs.ErrStateConflict
	}
	return ImageBuildClaim{ImageBuild: build, RecipeInput: recipeSpec.Input,
		PolicyRevision: recipeSpec.PolicyRevision, PolicySHA256: recipeSpec.PolicySHA256,
		RoleRuntimeContractRevision: recipeSpec.RoleRuntimeContractRevision,
		RoleRuntimeContractSHA256:   recipeSpec.RoleRuntimeContractSHA256,
		LeaseToken:                  token, Fence: spec.Fence, AuthorityGeneration: spec.AuthorityGeneration,
		LeaseExpiresAt: spec.LeaseExpiresAt}, nil
}

type ImageBuildLeaseInput struct {
	Principal       value.Principal
	IdempotencyKey  string
	ImageBuildID    string
	ExpectedVersion uint64
	ExpectedAttempt uint32
	ExpectedFence   uint64
	LeaseToken      string
}

type RenewImageBuildResult struct {
	ImageBuild     entity.Resource
	LeaseToken     string
	LeaseExpiresAt time.Time
}

func (service *Service) RenewImageBuild(ctx context.Context, input ImageBuildLeaseInput) (RenewImageBuildResult, error) {
	if err := authorizeImageWorkload(input.Principal, permissionRenewImageBuild,
		service.imageBuilderWorkload, service.imageBuilderSPIFFEID); err != nil {
		return RenewImageBuildResult{}, err
	}
	updated, token, err := service.mutateImageBuildLease(ctx, input, "renew_image_build", "",
		func(spec *entity.ImageBuildSpec, current entity.Resource, now time.Time) (enum.State, error) {
			spec.LeaseExpiresAt = now.Add(service.imageBuildLeaseDuration).UTC().Truncate(time.Microsecond)
			token := service.imageLeaseToken(current.ID, spec.Attempt, spec.Fence,
				spec.AuthorityGeneration, input.IdempotencyKey)
			spec.LeaseTokenSHA256 = hashString(token)
			return current.State, nil
		})
	if err != nil {
		return RenewImageBuildResult{}, err
	}
	spec := updated.Spec.(entity.ImageBuildSpec)
	return RenewImageBuildResult{ImageBuild: updated, LeaseToken: token, LeaseExpiresAt: spec.LeaseExpiresAt}, nil
}

type ReportImageBuildProgressInput struct {
	ImageBuildLeaseInput
	Stage           entity.ImageBuildStage
	ProgressPercent uint32
}

func (service *Service) ReportImageBuildProgress(
	ctx context.Context,
	input ReportImageBuildProgressInput,
) (entity.Resource, error) {
	if err := authorizeImageWorkload(input.Principal, permissionReportImageBuild,
		service.imageBuilderWorkload, service.imageBuilderSPIFFEID); err != nil {
		return entity.Resource{}, err
	}
	if !buildProgressStage(input.Stage) || input.ProgressPercent > 99 {
		return entity.Resource{}, errs.ErrInvalidInput
	}
	payloadHash, err := canonicalHash(struct {
		Stage           entity.ImageBuildStage
		ProgressPercent uint32
	}{input.Stage, input.ProgressPercent})
	if err != nil {
		return entity.Resource{}, errs.ErrInvalidInput
	}
	updated, _, err := service.mutateImageBuildLease(ctx, input.ImageBuildLeaseInput,
		"report_image_build_progress", payloadHash,
		func(spec *entity.ImageBuildSpec, current entity.Resource, _ time.Time) (enum.State, error) {
			if imageBuildStageOrder(input.Stage) < imageBuildStageOrder(spec.Stage) ||
				input.ProgressPercent < spec.ProgressPercent {
				return "", errs.ErrStateConflict
			}
			spec.Stage, spec.ProgressPercent = input.Stage, input.ProgressPercent
			return enum.StateRunning, nil
		})
	return updated, err
}

type CompleteImageBuildInput struct {
	ImageBuildLeaseInput
	StagingReference     string
	ManifestDigest       string
	ProvenanceSHA256     string
	ImmutableBuildSHA256 string
}

type CompleteImageBuildResult struct {
	ImageBuild    entity.Resource
	ImageArtifact entity.Resource
}

func (service *Service) CompleteImageBuild(
	ctx context.Context,
	input CompleteImageBuildInput,
) (CompleteImageBuildResult, error) {
	if err := authorizeImageWorkload(input.Principal, permissionCompleteImageBuild,
		service.imageBuilderWorkload, service.imageBuilderSPIFFEID); err != nil {
		return CompleteImageBuildResult{}, err
	}
	if !validDigest(input.ManifestDigest) || !validSHA256Text(input.ProvenanceSHA256) ||
		!validSHA256Text(input.ImmutableBuildSHA256) ||
		input.StagingReference != service.stagingImageRepository+"@"+input.ManifestDigest {
		return CompleteImageBuildResult{}, errs.ErrInvalidInput
	}
	payloadHash, err := canonicalHash(struct {
		StagingReference     string
		ManifestDigest       string
		ProvenanceSHA256     string
		ImmutableBuildSHA256 string
	}{input.StagingReference, input.ManifestDigest, input.ProvenanceSHA256, input.ImmutableBuildSHA256})
	if err != nil {
		return CompleteImageBuildResult{}, errs.ErrInvalidInput
	}
	build, _, err := service.mutateImageBuildLease(ctx, input.ImageBuildLeaseInput,
		"complete_image_build", payloadHash,
		func(spec *entity.ImageBuildSpec, current entity.Resource, _ time.Time) (enum.State, error) {
			if input.ImmutableBuildSHA256 != spec.ImmutableBuildSHA256 {
				return "", errs.ErrStateConflict
			}
			spec.Stage, spec.ProgressPercent = entity.ImageBuildStageCompleted, 100
			spec.StagingReference, spec.ManifestDigest = input.StagingReference, input.ManifestDigest
			spec.ProvenanceSHA256 = input.ProvenanceSHA256
			clearImageBuildClaim(spec)
			return enum.StateSucceeded, nil
		})
	if err != nil {
		return CompleteImageBuildResult{}, err
	}
	spec := build.Spec.(entity.ImageBuildSpec)
	artifactID := uuid.NewSHA1(uuid.NameSpaceOID, []byte("role-image-artifact\x00"+build.ID+"\x00"+
		strconv.FormatUint(uint64(spec.Attempt), 10)+"\x00"+spec.ManifestDigest)).String()
	artifact, err := service.repository.Get(ctx, build.OrganizationID, build.ProjectID, artifactID, enum.KindImageArtifact)
	if err != nil {
		return CompleteImageBuildResult{}, err
	}
	return CompleteImageBuildResult{ImageBuild: build, ImageArtifact: artifact}, nil
}

type FailImageBuildInput struct {
	ImageBuildLeaseInput
	ErrorCode         string
	DiagnosticCode    string
	DiagnosticSummary string
}

func (service *Service) FailImageBuild(ctx context.Context, input FailImageBuildInput) (entity.Resource, error) {
	if err := authorizeImageWorkload(input.Principal, permissionFailImageBuild,
		service.imageBuilderWorkload, service.imageBuilderSPIFFEID); err != nil {
		return entity.Resource{}, err
	}
	if !validImageBuildErrorCode(input.ErrorCode) || !validImageBuildDiagnostic(input.DiagnosticCode, input.DiagnosticSummary) {
		return entity.Resource{}, errs.ErrInvalidInput
	}
	payloadHash, err := canonicalHash(struct {
		ErrorCode         string
		DiagnosticCode    string
		DiagnosticSummary string
	}{input.ErrorCode, input.DiagnosticCode, input.DiagnosticSummary})
	if err != nil {
		return entity.Resource{}, errs.ErrInvalidInput
	}
	updated, _, err := service.mutateImageBuildLease(ctx, input.ImageBuildLeaseInput,
		"fail_image_build", payloadHash,
		func(spec *entity.ImageBuildSpec, _ entity.Resource, _ time.Time) (enum.State, error) {
			spec.Stage, spec.ErrorCode = entity.ImageBuildStageFailed, input.ErrorCode
			spec.DiagnosticCode, spec.DiagnosticSummary = input.DiagnosticCode, input.DiagnosticSummary
			clearImageBuildClaim(spec)
			return enum.StateFailed, nil
		})
	return updated, err
}

type imageBuildMutation func(*entity.ImageBuildSpec, entity.Resource, time.Time) (enum.State, error)

func (service *Service) mutateImageBuildLease(
	ctx context.Context,
	input ImageBuildLeaseInput,
	action string,
	payloadHash string,
	mutate imageBuildMutation,
) (entity.Resource, string, error) {
	if value.ValidateIdempotencyKey(input.IdempotencyKey) != nil || value.ValidateID(input.ImageBuildID) != nil ||
		input.ExpectedVersion == 0 || input.ExpectedAttempt == 0 || input.ExpectedFence == 0 || input.LeaseToken == "" {
		return entity.Resource{}, "", errs.ErrInvalidInput
	}
	requestHash, err := canonicalHash(struct {
		Identity        commandIdentity
		ImageBuildID    string
		ExpectedVersion uint64
		ExpectedAttempt uint32
		ExpectedFence   uint64
		LeaseTokenHash  string
		Action          string
		PayloadHash     string
	}{identity(input.Principal), input.ImageBuildID, input.ExpectedVersion, input.ExpectedAttempt,
		input.ExpectedFence, hashString(input.LeaseToken), action, payloadHash})
	if err != nil {
		return entity.Resource{}, "", errs.ErrInvalidInput
	}
	updated, err := service.withResourceReceipt(ctx, input.Principal, input.IdempotencyKey, action,
		requestHash, func(tx domainrepo.Transaction) (entity.Resource, error) {
			now, err := tx.CurrentTime(ctx)
			if err != nil {
				return entity.Resource{}, err
			}
			current, err := tx.GetForUpdate(ctx, input.Principal.OrganizationID,
				input.Principal.ProjectID, input.ImageBuildID)
			if err != nil {
				return entity.Resource{}, err
			}
			spec, ok := current.Spec.(entity.ImageBuildSpec)
			if !ok || current.Kind != enum.KindImageBuild || current.Version != input.ExpectedVersion ||
				spec.Attempt != input.ExpectedAttempt || spec.Fence != input.ExpectedFence ||
				spec.ClaimantWorkloadID != input.Principal.CallerWorkload ||
				spec.ClaimantSPIFFEID != input.Principal.CallerSPIFFEID ||
				spec.LeaseTokenSHA256 != hashString(input.LeaseToken) || !now.Before(spec.LeaseExpiresAt) ||
				(current.State != enum.StateClaimed && current.State != enum.StateRunning) {
				return entity.Resource{}, errs.ErrStateConflict
			}
			target, err := mutate(&spec, current, now)
			if err != nil {
				return entity.Resource{}, err
			}
			var next entity.Resource
			if target == current.State {
				next, err = current.Update(current.Name, spec, now)
			} else {
				next, err = current.ReplaceAndTransition(spec, target, now)
			}
			if err != nil {
				return entity.Resource{}, errs.ErrStateConflict
			}
			if err := tx.Update(ctx, next, current.Version); err != nil {
				return entity.Resource{}, err
			}
			if target == enum.StateSucceeded {
				recipe, getErr := tx.GetForUpdate(ctx, next.OrganizationID, next.ProjectID, spec.RecipeID)
				if getErr != nil {
					return entity.Resource{}, getErr
				}
				recipeSpec, ok := recipe.Spec.(entity.RoleImageRecipeSpec)
				if !ok || recipe.State != enum.StateActive || recipe.Version != spec.RecipeVersion ||
					recipeSpec.Generation != spec.RecipeGeneration || recipeSpec.SpecSHA256 != spec.SpecSHA256 ||
					recipeSpec.PolicyRevision != service.imagePolicyRevision || recipeSpec.PolicySHA256 != service.imagePolicySHA256 {
					return entity.Resource{}, errs.ErrStateConflict
				}
				artifactID := uuid.NewSHA1(uuid.NameSpaceOID, []byte("role-image-artifact\x00"+next.ID+"\x00"+
					strconv.FormatUint(uint64(spec.Attempt), 10)+"\x00"+spec.ManifestDigest)).String()
				artifactSpec := entity.ImageArtifactSpec{
					RecipeID: recipe.ID, RecipeVersion: recipe.Version, RecipeGeneration: recipeSpec.Generation,
					SpecSHA256: spec.SpecSHA256, BuildID: next.ID, BuildVersion: next.Version, BuildAttempt: spec.Attempt,
					StagingReference: spec.StagingReference, ManifestDigest: spec.ManifestDigest,
					ProvenanceSHA256: spec.ProvenanceSHA256, ImmutableBuildSHA256: spec.ImmutableBuildSHA256,
					BaseImageDigest: recipeSpec.Input.BaseImageDigest, SourceSHA256: recipeSpec.Input.SourceSHA256,
					ContextSHA256: recipeSpec.Input.ContextSHA256, BuilderSHA256: recipeSpec.Input.BuilderSHA256,
					FrontendSHA256: recipeSpec.Input.FrontendSHA256, ToolchainSHA256: recipeSpec.Input.ToolchainSHA256,
					Platforms:      slices.Clone(recipeSpec.Input.Platforms),
					PolicyRevision: recipeSpec.PolicyRevision, PolicySHA256: recipeSpec.PolicySHA256,
					RoleRuntimeContractRevision: recipeSpec.RoleRuntimeContractRevision,
					RoleRuntimeContractSHA256:   recipeSpec.RoleRuntimeContractSHA256,
				}
				artifact, createErr := entity.New(artifactID, next.OrganizationID, next.ProjectID, next.ID,
					next.OwnerActorID, enum.KindImageArtifact, "Артефакт "+recipe.Name, artifactSpec, now)
				if createErr != nil {
					return entity.Resource{}, errs.ErrInternal
				}
				if err := tx.Insert(ctx, artifact); err != nil {
					return entity.Resource{}, err
				}
				if err := service.appendMutationRecords(ctx, tx, input.Principal, "stage_image_artifact", artifact); err != nil {
					return entity.Resource{}, err
				}
			}
			return next, service.appendMutationRecords(ctx, tx, input.Principal, action, next)
		})
	if err != nil {
		return entity.Resource{}, "", err
	}
	current, currentErr := service.repository.Get(ctx, updated.OrganizationID, updated.ProjectID,
		updated.ID, enum.KindImageBuild)
	if currentErr != nil || current.Version != updated.Version {
		return entity.Resource{}, "", errs.ErrIdempotencyConflict
	}
	spec := updated.Spec.(entity.ImageBuildSpec)
	token := service.imageLeaseToken(updated.ID, spec.Attempt, spec.Fence, spec.AuthorityGeneration, input.IdempotencyKey)
	if spec.LeaseTokenSHA256 == "" {
		token = ""
	} else if hashString(token) != spec.LeaseTokenSHA256 {
		return entity.Resource{}, "", errs.ErrStateConflict
	}
	return updated, token, nil
}

type ImageBuildOwnerAction string

const (
	ImageBuildCancel     ImageBuildOwnerAction = "CANCEL"
	ImageBuildRetry      ImageBuildOwnerAction = "RETRY"
	ImageBuildExpire     ImageBuildOwnerAction = "EXPIRE"
	ImageBuildDeadLetter ImageBuildOwnerAction = "DEAD_LETTER"
)

type ManageImageBuildInput struct {
	Principal       value.Principal
	IdempotencyKey  string
	ImageBuildID    string
	ExpectedVersion uint64
	Action          ImageBuildOwnerAction
}

func (service *Service) ManageImageBuild(ctx context.Context, input ManageImageBuildInput) (entity.Resource, error) {
	if err := authorize(input.Principal, permissionManageImageBuild); err != nil {
		return entity.Resource{}, err
	}
	if input.Principal.CallerWorkload != controlAPIGatewayWorkload || input.Principal.CallerSPIFFEID != controlAPIGatewaySPIFFEID ||
		value.ValidateIdempotencyKey(input.IdempotencyKey) != nil || value.ValidateID(input.ImageBuildID) != nil ||
		input.ExpectedVersion == 0 || !validImageBuildOwnerAction(input.Action) {
		return entity.Resource{}, errs.ErrInvalidInput
	}
	requestHash, _ := canonicalHash(struct {
		Identity commandIdentity
		ID       string
		Version  uint64
		Action   ImageBuildOwnerAction
	}{identity(input.Principal), input.ImageBuildID, input.ExpectedVersion, input.Action})
	return service.withResourceReceipt(ctx, input.Principal, input.IdempotencyKey,
		"manage_image_build_"+strings.ToLower(string(input.Action)), requestHash,
		func(tx domainrepo.Transaction) (entity.Resource, error) {
			now, err := tx.CurrentTime(ctx)
			if err != nil {
				return entity.Resource{}, err
			}
			current, err := tx.GetForUpdate(ctx, input.Principal.OrganizationID, input.Principal.ProjectID, input.ImageBuildID)
			if err != nil {
				return entity.Resource{}, err
			}
			spec, ok := current.Spec.(entity.ImageBuildSpec)
			if !ok || current.Kind != enum.KindImageBuild || current.OwnerActorID != input.Principal.ActorID {
				return entity.Resource{}, errs.ErrNotFound
			}
			if current.Version != input.ExpectedVersion {
				return entity.Resource{}, errs.ErrVersionMismatch
			}
			var target enum.State
			switch input.Action {
			case ImageBuildCancel:
				if current.State != enum.StateQueued && current.State != enum.StateClaimed &&
					current.State != enum.StateRunning && current.State != enum.StateBlocked {
					return entity.Resource{}, errs.ErrStateConflict
				}
				clearImageBuildClaim(&spec)
				spec.Stage, spec.ErrorCode, target = entity.ImageBuildStageCancelled, "CANCELLED_BY_OWNER", enum.StateCancelled
			case ImageBuildRetry:
				if (current.State != enum.StateFailed && current.State != enum.StateExpired && current.State != enum.StateBlocked) ||
					spec.Stage == entity.ImageBuildStageDeadLetter || spec.Attempt >= spec.MaximumAttempts {
					return entity.Resource{}, errs.ErrStateConflict
				}
				spec.Attempt++
				spec.Fence++
				spec.AuthorityGeneration++
				clearImageBuildClaim(&spec)
				spec.Stage, spec.ProgressPercent, spec.ErrorCode = entity.ImageBuildStageQueued, 0, ""
				spec.DiagnosticCode, spec.DiagnosticSummary = "", ""
				spec.StagingReference, spec.ManifestDigest, spec.ProvenanceSHA256 = "", "", ""
				spec.AvailableAt, target = now, enum.StateQueued
			case ImageBuildExpire:
				if (current.State != enum.StateClaimed && current.State != enum.StateRunning) ||
					now.Before(spec.LeaseExpiresAt) {
					return entity.Resource{}, errs.ErrStateConflict
				}
				clearImageBuildClaim(&spec)
				spec.Stage, spec.ErrorCode, target = entity.ImageBuildStageExpired, "LEASE_EXPIRED", enum.StateExpired
			case ImageBuildDeadLetter:
				if (current.State != enum.StateFailed && current.State != enum.StateExpired && current.State != enum.StateBlocked) ||
					spec.Attempt < spec.MaximumAttempts {
					return entity.Resource{}, errs.ErrStateConflict
				}
				clearImageBuildClaim(&spec)
				spec.Stage, spec.ErrorCode, target = entity.ImageBuildStageDeadLetter, "MAXIMUM_ATTEMPTS_EXHAUSTED", current.State
			}
			var updated entity.Resource
			if target == current.State {
				updated, err = current.Update(current.Name, spec, now)
			} else {
				updated, err = current.ReplaceAndTransition(spec, target, now)
			}
			if err != nil {
				return entity.Resource{}, errs.ErrStateConflict
			}
			if err := tx.Update(ctx, updated, current.Version); err != nil {
				return entity.Resource{}, err
			}
			return updated, service.appendMutationRecords(ctx, tx, input.Principal,
				"manage_image_build_"+strings.ToLower(string(input.Action)), updated)
		})
}

type ClaimImageAdmissionInput struct {
	Principal      value.Principal
	IdempotencyKey string
}

type ImageAdmissionClaim struct {
	ImageArtifact       entity.Resource
	ClaimToken          string
	Fence               uint64
	AuthorityGeneration uint64
	ClaimExpiresAt      time.Time
}

func (service *Service) ClaimImageAdmission(
	ctx context.Context,
	input ClaimImageAdmissionInput,
) (ImageAdmissionClaim, error) {
	if err := authorizeImageWorkload(input.Principal, permissionClaimImageAdmission,
		service.imageAdmissionWorkload, service.imageAdmissionSPIFFEID); err != nil {
		return ImageAdmissionClaim{}, err
	}
	if value.ValidateIdempotencyKey(input.IdempotencyKey) != nil || input.Principal.ProjectID == "" {
		return ImageAdmissionClaim{}, errs.ErrInvalidInput
	}
	requestHash, _ := canonicalHash(struct {
		Identity commandIdentity
		Key      string
	}{identity(input.Principal), hashString(input.IdempotencyKey)})
	artifact, err := service.withResourceReceipt(ctx, input.Principal, input.IdempotencyKey,
		"claim_image_admission", requestHash, func(tx domainrepo.Transaction) (entity.Resource, error) {
			imageTx, ok := tx.(domainrepo.ImageTransaction)
			if !ok {
				return entity.Resource{}, errs.ErrInternal
			}
			now, err := tx.CurrentTime(ctx)
			if err != nil {
				return entity.Resource{}, err
			}
			current, err := imageTx.NextImageAdmission(ctx, input.Principal.OrganizationID, input.Principal.ProjectID, now)
			if err != nil {
				return entity.Resource{}, err
			}
			spec, ok := current.Spec.(entity.ImageArtifactSpec)
			if !ok || current.State != enum.StateWaitingExternal || spec.AdmissionVerdict != "" ||
				spec.PolicyRevision != service.imagePolicyRevision || spec.PolicySHA256 != service.imagePolicySHA256 {
				return entity.Resource{}, errs.ErrStateConflict
			}
			spec.AdmissionAuthorityGeneration++
			if spec.AdmissionAuthorityGeneration < input.Principal.AuthorityGeneration {
				spec.AdmissionAuthorityGeneration = input.Principal.AuthorityGeneration
			}
			spec.AdmissionFence++
			spec.AdmissionClaimantWorkloadID = input.Principal.CallerWorkload
			spec.AdmissionClaimExpiresAt = now.Add(service.imageAdmissionClaimTTL).UTC().Truncate(time.Microsecond)
			token := service.imageAdmissionToken(current.ID, spec.ManifestDigest, spec.BuildAttempt,
				spec.AdmissionFence, spec.AdmissionAuthorityGeneration, input.IdempotencyKey)
			spec.AdmissionClaimTokenSHA256 = hashString(token)
			updated, err := current.Update(current.Name, spec, now)
			if err != nil {
				return entity.Resource{}, errs.ErrStateConflict
			}
			if err := tx.Update(ctx, updated, current.Version); err != nil {
				return entity.Resource{}, err
			}
			return updated, service.appendMutationRecords(ctx, tx, input.Principal, "claim_image_admission", updated)
		})
	if err != nil {
		return ImageAdmissionClaim{}, err
	}
	current, currentErr := service.repository.Get(ctx, artifact.OrganizationID, artifact.ProjectID,
		artifact.ID, enum.KindImageArtifact)
	if currentErr != nil || current.Version != artifact.Version {
		return ImageAdmissionClaim{}, errs.ErrIdempotencyConflict
	}
	spec := artifact.Spec.(entity.ImageArtifactSpec)
	token := service.imageAdmissionToken(artifact.ID, spec.ManifestDigest, spec.BuildAttempt,
		spec.AdmissionFence, spec.AdmissionAuthorityGeneration, input.IdempotencyKey)
	if hashString(token) != spec.AdmissionClaimTokenSHA256 {
		return ImageAdmissionClaim{}, errs.ErrStateConflict
	}
	return ImageAdmissionClaim{ImageArtifact: artifact, ClaimToken: token, Fence: spec.AdmissionFence,
		AuthorityGeneration: spec.AdmissionAuthorityGeneration, ClaimExpiresAt: spec.AdmissionClaimExpiresAt}, nil
}

type RecordImageAdmissionInput struct {
	Principal                         value.Principal
	IdempotencyKey                    string
	ImageArtifactID                   string
	ExpectedVersion                   uint64
	ExpectedFence                     uint64
	ClaimToken                        string
	ManifestDigest                    string
	ImmutableBuildSHA256              string
	ProvenanceSHA256                  string
	SBOMSHA256                        string
	VulnerabilityEvidenceSHA256       string
	PolicyRevision                    uint64
	PolicySHA256                      string
	Verdict                           entity.ImageAdmissionVerdict
	SignatureIdentity                 string
	SignatureSHA256                   string
	AdmissionReceiptSHA256            string
	AdmissionReceiptOCIManifestDigest string
}

type RecordImageAdmissionResult struct {
	ImageArtifact entity.Resource
}

func (service *Service) RecordImageAdmission(
	ctx context.Context,
	input RecordImageAdmissionInput,
) (RecordImageAdmissionResult, error) {
	if err := authorizeImageWorkload(input.Principal, permissionRecordImageAdmission,
		service.imageAdmissionWorkload, service.imageAdmissionSPIFFEID); err != nil {
		return RecordImageAdmissionResult{}, err
	}
	if value.ValidateIdempotencyKey(input.IdempotencyKey) != nil || value.ValidateID(input.ImageArtifactID) != nil ||
		input.ExpectedVersion == 0 || input.ExpectedFence == 0 || input.ClaimToken == "" ||
		!validDigest(input.ManifestDigest) || !validSHA256Text(input.ImmutableBuildSHA256) ||
		!validSHA256Text(input.ProvenanceSHA256) || !validSHA256Text(input.SBOMSHA256) ||
		!validSHA256Text(input.VulnerabilityEvidenceSHA256) || input.PolicyRevision != service.imagePolicyRevision ||
		input.PolicySHA256 != service.imagePolicySHA256 ||
		(input.Verdict != entity.ImageAdmissionAccepted && input.Verdict != entity.ImageAdmissionRejected) ||
		input.SignatureIdentity == "" || !validSHA256Text(input.SignatureSHA256) ||
		!validSHA256Text(input.AdmissionReceiptSHA256) || !validDigest(input.AdmissionReceiptOCIManifestDigest) {
		return RecordImageAdmissionResult{}, errs.ErrInvalidInput
	}
	requestHash, _ := canonicalHash(struct {
		Identity commandIdentity
		Input    RecordImageAdmissionInput
	}{identity(input.Principal), input})
	artifact, err := service.withResourceReceipt(ctx, input.Principal, input.IdempotencyKey,
		"record_image_admission", requestHash, func(tx domainrepo.Transaction) (entity.Resource, error) {
			now, err := tx.CurrentTime(ctx)
			if err != nil {
				return entity.Resource{}, err
			}
			current, err := tx.GetForUpdate(ctx, input.Principal.OrganizationID, input.Principal.ProjectID, input.ImageArtifactID)
			if err != nil {
				return entity.Resource{}, err
			}
			spec, ok := current.Spec.(entity.ImageArtifactSpec)
			if !ok || current.Kind != enum.KindImageArtifact || current.State != enum.StateWaitingExternal ||
				current.Version != input.ExpectedVersion || spec.AdmissionFence != input.ExpectedFence ||
				spec.AdmissionClaimantWorkloadID != input.Principal.CallerWorkload ||
				spec.AdmissionClaimTokenSHA256 != hashString(input.ClaimToken) ||
				!now.Before(spec.AdmissionClaimExpiresAt) || spec.AdmissionVerdict != "" ||
				spec.ManifestDigest != input.ManifestDigest || spec.ImmutableBuildSHA256 != input.ImmutableBuildSHA256 ||
				spec.ProvenanceSHA256 != input.ProvenanceSHA256 || spec.PolicyRevision != input.PolicyRevision ||
				spec.PolicySHA256 != input.PolicySHA256 {
				return entity.Resource{}, errs.ErrStateConflict
			}
			spec.SBOMSHA256, spec.VulnerabilityEvidenceSHA256 = input.SBOMSHA256, input.VulnerabilityEvidenceSHA256
			spec.AdmissionVerdict, spec.SignatureIdentity = input.Verdict, input.SignatureIdentity
			spec.SignatureSHA256, spec.AdmissionReceiptSHA256 = input.SignatureSHA256, input.AdmissionReceiptSHA256
			spec.AdmissionReceiptOCIManifestDigest = input.AdmissionReceiptOCIManifestDigest
			spec.AdmissionRevision++
			spec.AdmissionClaimantWorkloadID, spec.AdmissionClaimTokenSHA256 = "", ""
			spec.AdmissionClaimExpiresAt = time.Time{}
			if input.Verdict == entity.ImageAdmissionAccepted {
				clearPromotionClaim(&spec)
			}
			var updated entity.Resource
			if input.Verdict == entity.ImageAdmissionRejected {
				updated, err = current.ReplaceAndTransition(spec, enum.StateBlocked, now)
			} else {
				updated, err = current.Update(current.Name, spec, now)
			}
			if err != nil {
				return entity.Resource{}, errs.ErrStateConflict
			}
			if err := tx.Update(ctx, updated, current.Version); err != nil {
				return entity.Resource{}, err
			}
			return updated, service.appendMutationRecords(ctx, tx, input.Principal, "record_image_admission", updated)
		})
	if err != nil {
		return RecordImageAdmissionResult{}, err
	}
	return RecordImageAdmissionResult{ImageArtifact: artifact}, nil
}

type ClaimImagePromotionInput struct {
	Principal      value.Principal
	IdempotencyKey string
}

type ImagePromotionClaim struct {
	ImageArtifact       entity.Resource
	PromotionClaim      string
	Fence               uint64
	AuthorityGeneration uint64
	ClaimExpiresAt      time.Time
}

func (service *Service) ClaimImagePromotion(
	ctx context.Context,
	input ClaimImagePromotionInput,
) (ImagePromotionClaim, error) {
	if err := authorizeImageWorkload(input.Principal, permissionClaimImagePromotion,
		service.imagePromotionWorkload, service.imagePromotionSPIFFEID); err != nil {
		return ImagePromotionClaim{}, err
	}
	if value.ValidateIdempotencyKey(input.IdempotencyKey) != nil || input.Principal.ProjectID == "" {
		return ImagePromotionClaim{}, errs.ErrInvalidInput
	}
	requestHash, _ := canonicalHash(struct {
		Identity commandIdentity
		Key      string
	}{identity(input.Principal), hashString(input.IdempotencyKey)})
	artifact, err := service.withResourceReceipt(ctx, input.Principal, input.IdempotencyKey,
		"claim_image_promotion", requestHash, func(tx domainrepo.Transaction) (entity.Resource, error) {
			imageTx, ok := tx.(domainrepo.ImageTransaction)
			if !ok {
				return entity.Resource{}, errs.ErrInternal
			}
			now, err := tx.CurrentTime(ctx)
			if err != nil {
				return entity.Resource{}, err
			}
			current, err := imageTx.NextImagePromotion(ctx, input.Principal.OrganizationID,
				input.Principal.ProjectID, service.imagePolicyRevision, service.imagePolicySHA256, now)
			if err != nil {
				return entity.Resource{}, err
			}
			spec, ok := current.Spec.(entity.ImageArtifactSpec)
			if !ok || current.Kind != enum.KindImageArtifact || current.State != enum.StateWaitingExternal ||
				spec.AdmissionVerdict != entity.ImageAdmissionAccepted || spec.PromotedReference != "" ||
				spec.PolicyRevision != service.imagePolicyRevision || spec.PolicySHA256 != service.imagePolicySHA256 ||
				(spec.PromotionClaimJTISHA256 != "" && now.Before(spec.PromotionClaimExpiresAt)) ||
				(spec.PromotionAuthorizationTokenSHA256 != "" && now.Before(spec.PromotionAuthorizationExpiresAt)) {
				return entity.Resource{}, errs.ErrStateConflict
			}
			if spec.PromotionAuthorizationTokenSHA256 != "" {
				clearPromotionClaim(&spec)
			}
			spec.PromotionAuthorityGeneration++
			if spec.PromotionAuthorityGeneration < input.Principal.AuthorityGeneration {
				spec.PromotionAuthorityGeneration = input.Principal.AuthorityGeneration
			}
			spec.PromotionFence++
			spec.PromotionClaimantWorkloadID = input.Principal.CallerWorkload
			spec.PromotionClaimantSPIFFEID = input.Principal.CallerSPIFFEID
			spec.PromotionClaimExpiresAt = now.Add(service.imagePromotionClaimTTL).UTC().Truncate(time.Microsecond)
			spec.PromotionClaimJTISHA256 = hashString(current.ID + "\x00" +
				strconv.FormatUint(spec.AdmissionRevision, 10) + "\x00" +
				strconv.FormatUint(spec.PromotionFence, 10) + "\x00" + hashString(input.IdempotencyKey))
			updated, err := current.Update(current.Name, spec, now)
			if err != nil {
				return entity.Resource{}, errs.ErrStateConflict
			}
			if err := tx.Update(ctx, updated, current.Version); err != nil {
				return entity.Resource{}, err
			}
			return updated, service.appendMutationRecords(ctx, tx, input.Principal, "claim_image_promotion", updated)
		})
	if err != nil {
		return ImagePromotionClaim{}, err
	}
	spec, ok := artifact.Spec.(entity.ImageArtifactSpec)
	if !ok || spec.PromotionClaimantWorkloadID != input.Principal.CallerWorkload ||
		spec.PromotionClaimantSPIFFEID != input.Principal.CallerSPIFFEID {
		return ImagePromotionClaim{}, errs.ErrStateConflict
	}
	claim, err := service.signPromotionClaim(artifact, spec)
	if err != nil {
		return ImagePromotionClaim{}, err
	}
	return ImagePromotionClaim{ImageArtifact: artifact, PromotionClaim: claim,
		Fence: spec.PromotionFence, AuthorityGeneration: spec.PromotionAuthorityGeneration,
		ClaimExpiresAt: spec.PromotionClaimExpiresAt}, nil
}

type CompleteImagePromotionInput struct {
	Principal               value.Principal
	IdempotencyKey          string
	ImageArtifactID         string
	ExpectedVersion         uint64
	AuthorizationToken      string
	PromotedReference       string
	ManifestDigest          string
	PromotionReadbackSHA256 string
}

func (service *Service) CompleteImagePromotion(
	ctx context.Context,
	input CompleteImagePromotionInput,
) (entity.Resource, error) {
	if err := authorizeImageWorkload(input.Principal, permissionCompleteImagePromotion,
		service.imagePromotionWorkload, service.imagePromotionSPIFFEID); err != nil {
		return entity.Resource{}, err
	}
	if value.ValidateIdempotencyKey(input.IdempotencyKey) != nil || value.ValidateID(input.ImageArtifactID) != nil ||
		input.ExpectedVersion == 0 || input.AuthorizationToken == "" || !validDigest(input.ManifestDigest) ||
		input.PromotedReference != service.promotedImageRepository+"@"+input.ManifestDigest ||
		!validSHA256Text(input.PromotionReadbackSHA256) {
		return entity.Resource{}, errs.ErrInvalidInput
	}
	requestHash, _ := canonicalHash(struct {
		Identity commandIdentity
		ID       string
		Version  uint64
		Claim    string
		Ref      string
		Digest   string
		Readback string
	}{identity(input.Principal), input.ImageArtifactID, input.ExpectedVersion, hashString(input.AuthorizationToken),
		input.PromotedReference, input.ManifestDigest, input.PromotionReadbackSHA256})
	return service.withResourceReceipt(ctx, input.Principal, input.IdempotencyKey,
		"complete_image_promotion", requestHash, func(tx domainrepo.Transaction) (entity.Resource, error) {
			now, err := tx.CurrentTime(ctx)
			if err != nil {
				return entity.Resource{}, err
			}
			current, err := tx.GetForUpdate(ctx, input.Principal.OrganizationID, input.Principal.ProjectID, input.ImageArtifactID)
			if err != nil {
				return entity.Resource{}, err
			}
			spec, ok := current.Spec.(entity.ImageArtifactSpec)
			if !ok || current.State != enum.StateWaitingExternal || current.Version != input.ExpectedVersion ||
				spec.AdmissionVerdict != entity.ImageAdmissionAccepted || spec.ManifestDigest != input.ManifestDigest ||
				spec.PolicyRevision != service.imagePolicyRevision || spec.PolicySHA256 != service.imagePolicySHA256 ||
				spec.PromotedReference != "" || !now.Before(spec.PromotionAuthorizationExpiresAt) ||
				spec.PromotionClaimantWorkloadID != input.Principal.CallerWorkload ||
				spec.PromotionClaimantSPIFFEID != input.Principal.CallerSPIFFEID ||
				spec.PromotionAuthorizationTokenSHA256 != hashString(input.AuthorizationToken) {
				return entity.Resource{}, errs.ErrStateConflict
			}
			spec.PromotedReference, spec.PromotionReadbackSHA256 = input.PromotedReference, input.PromotionReadbackSHA256
			spec.PromotedAt = now.UTC().Truncate(time.Microsecond)
			spec.PromotionAuthorizationTokenSHA256 = ""
			spec.PromotionAuthorizationExpiresAt = time.Time{}
			clearPromotionClaim(&spec)
			updated, err := current.ReplaceAndTransition(spec, enum.StateActive, now)
			if err != nil {
				return entity.Resource{}, errs.ErrStateConflict
			}
			if err := tx.Update(ctx, updated, current.Version); err != nil {
				return entity.Resource{}, err
			}
			return updated, service.appendMutationRecords(ctx, tx, input.Principal, "complete_image_promotion", updated)
		})
}

type AuthorizeImagePromotionInput struct {
	Principal       value.Principal
	IdempotencyKey  string
	ImageArtifactID string
	ExpectedVersion uint64
	PromotionClaim  string
	ManifestDigest  string
}

type AuthorizeImagePromotionResult struct {
	ImageArtifact          entity.Resource
	AuthorizationToken     string
	AuthorizationExpiresAt time.Time
}

// AuthorizeImagePromotion потребляет owner claim в одной транзакции до registry copy.
func (service *Service) AuthorizeImagePromotion(
	ctx context.Context,
	input AuthorizeImagePromotionInput,
) (AuthorizeImagePromotionResult, error) {
	if err := authorizeImageWorkload(input.Principal, permissionAuthorizeImagePromotion,
		service.imagePromotionWorkload, service.imagePromotionSPIFFEID); err != nil {
		return AuthorizeImagePromotionResult{}, err
	}
	if value.ValidateIdempotencyKey(input.IdempotencyKey) != nil || value.ValidateID(input.ImageArtifactID) != nil ||
		input.ExpectedVersion == 0 || input.PromotionClaim == "" || !validDigest(input.ManifestDigest) {
		return AuthorizeImagePromotionResult{}, errs.ErrInvalidInput
	}
	requestHash, _ := canonicalHash(struct {
		Identity    commandIdentity
		ID          string
		Version     uint64
		ClaimSHA256 string
		Digest      string
	}{identity(input.Principal), input.ImageArtifactID, input.ExpectedVersion,
		hashString(input.PromotionClaim), input.ManifestDigest})
	artifact, err := service.withResourceReceipt(ctx, input.Principal, input.IdempotencyKey,
		"authorize_image_promotion", requestHash, func(tx domainrepo.Transaction) (entity.Resource, error) {
			now, err := tx.CurrentTime(ctx)
			if err != nil {
				return entity.Resource{}, err
			}
			current, err := tx.GetForUpdate(ctx, input.Principal.OrganizationID,
				input.Principal.ProjectID, input.ImageArtifactID)
			if err != nil {
				return entity.Resource{}, err
			}
			spec, ok := current.Spec.(entity.ImageArtifactSpec)
			if !ok || current.State != enum.StateWaitingExternal || current.Version != input.ExpectedVersion ||
				spec.AdmissionVerdict != entity.ImageAdmissionAccepted || spec.ManifestDigest != input.ManifestDigest ||
				spec.PromotedReference != "" || spec.PromotionAuthorizationTokenSHA256 != "" ||
				spec.PromotionClaimantWorkloadID != input.Principal.CallerWorkload ||
				spec.PromotionClaimantSPIFFEID != input.Principal.CallerSPIFFEID ||
				service.verifyPromotionClaim(input.PromotionClaim, current, spec, now) != nil {
				return entity.Resource{}, errs.ErrStateConflict
			}
			token := service.imageToken("promotion-authorize", current.ID, spec.ManifestDigest,
				spec.BuildAttempt, spec.PromotionFence, spec.PromotionAuthorityGeneration, input.IdempotencyKey)
			spec.PromotionAuthorizationTokenSHA256 = hashString(token)
			spec.PromotionAuthorizationExpiresAt = spec.PromotionClaimExpiresAt
			spec.PromotionClaimJTISHA256 = ""
			spec.PromotionClaimExpiresAt = time.Time{}
			updated, err := current.Update(current.Name, spec, now)
			if err != nil {
				return entity.Resource{}, errs.ErrStateConflict
			}
			if err := tx.Update(ctx, updated, current.Version); err != nil {
				return entity.Resource{}, err
			}
			return updated, service.appendMutationRecords(ctx, tx, input.Principal, "authorize_image_promotion", updated)
		})
	if err != nil {
		return AuthorizeImagePromotionResult{}, err
	}
	spec := artifact.Spec.(entity.ImageArtifactSpec)
	token := service.imageToken("promotion-authorize", artifact.ID, spec.ManifestDigest,
		spec.BuildAttempt, spec.PromotionFence, spec.PromotionAuthorityGeneration, input.IdempotencyKey)
	if hashString(token) != spec.PromotionAuthorizationTokenSHA256 {
		return AuthorizeImagePromotionResult{}, errs.ErrStateConflict
	}
	current, currentErr := service.repository.Get(ctx, artifact.OrganizationID, artifact.ProjectID,
		artifact.ID, enum.KindImageArtifact)
	currentSpec, currentOK := current.Spec.(entity.ImageArtifactSpec)
	if currentErr != nil || !currentOK || current.Version != artifact.Version ||
		currentSpec.PromotionAuthorizationTokenSHA256 != spec.PromotionAuthorizationTokenSHA256 ||
		currentSpec.PromotionFence != spec.PromotionFence ||
		currentSpec.PromotionAuthorityGeneration != spec.PromotionAuthorityGeneration ||
		!service.now().Before(currentSpec.PromotionAuthorizationExpiresAt) {
		return AuthorizeImagePromotionResult{}, errs.ErrStateConflict
	}
	return AuthorizeImagePromotionResult{ImageArtifact: artifact, AuthorizationToken: token,
		AuthorizationExpiresAt: spec.PromotionAuthorizationExpiresAt}, nil
}

type GetRoleImageRecipeInput struct {
	Principal       value.Principal
	RecipeID        string
	ExpectedVersion uint64
}

func (service *Service) GetRoleImageRecipe(ctx context.Context, input GetRoleImageRecipeInput) (entity.Resource, error) {
	if err := authorize(input.Principal, permissionReadRoleImageRecipe); err != nil {
		return entity.Resource{}, err
	}
	if value.ValidateID(input.RecipeID) != nil || input.ExpectedVersion == 0 {
		return entity.Resource{}, errs.ErrInvalidInput
	}
	found, err := service.repository.Get(ctx, input.Principal.OrganizationID, input.Principal.ProjectID,
		input.RecipeID, enum.KindRoleImageRecipe)
	if err != nil {
		return entity.Resource{}, err
	}
	if found.OwnerActorID != input.Principal.ActorID {
		return entity.Resource{}, errs.ErrNotFound
	}
	if found.Version != input.ExpectedVersion {
		return entity.Resource{}, errs.ErrVersionMismatch
	}
	return found, nil
}

type GetRoleImageBuildInput struct {
	Principal       value.Principal
	ImageBuildID    string
	ExpectedVersion uint64
}

func (service *Service) GetRoleImageBuild(ctx context.Context, input GetRoleImageBuildInput) (entity.Resource, error) {
	if err := authorize(input.Principal, permissionReadImageBuild); err != nil {
		return entity.Resource{}, err
	}
	if value.ValidateID(input.ImageBuildID) != nil || input.ExpectedVersion == 0 {
		return entity.Resource{}, errs.ErrInvalidInput
	}
	found, err := service.repository.Get(ctx, input.Principal.OrganizationID, input.Principal.ProjectID,
		input.ImageBuildID, enum.KindImageBuild)
	if err != nil {
		return entity.Resource{}, err
	}
	if found.OwnerActorID != input.Principal.ActorID {
		return entity.Resource{}, errs.ErrNotFound
	}
	if found.Version != input.ExpectedVersion {
		return entity.Resource{}, errs.ErrVersionMismatch
	}
	return found, nil
}

type promotionClaimPayload struct {
	Version                           uint32    `json:"version"`
	ArtifactID                        string    `json:"artifactId"`
	ArtifactVersion                   uint64    `json:"artifactVersion"`
	BuildID                           string    `json:"buildId"`
	BuildAttempt                      uint32    `json:"buildAttempt"`
	SpecSHA256                        string    `json:"specSha256"`
	ManifestDigest                    string    `json:"manifestDigest"`
	PolicyRevision                    uint64    `json:"policyRevision"`
	PolicySHA256                      string    `json:"policySha256"`
	AdmissionRevision                 uint64    `json:"admissionRevision"`
	AdmissionReceipt                  string    `json:"admissionReceiptSha256"`
	AdmissionReceiptOCIManifestDigest string    `json:"admissionReceiptOciManifestDigest"`
	ClaimantWorkload                  string    `json:"claimantWorkloadId"`
	ClaimantSPIFFEID                  string    `json:"claimantSpiffeId"`
	AuthorityGeneration               uint64    `json:"authorityGeneration"`
	Fence                             uint64    `json:"fence"`
	JTI                               string    `json:"jtiSha256"`
	ExpiresAt                         time.Time `json:"expiresAt"`
}

func (service *Service) signPromotionClaim(resource entity.Resource, spec entity.ImageArtifactSpec) (string, error) {
	payload := promotionClaimPayload{Version: 1, ArtifactID: resource.ID, ArtifactVersion: resource.Version,
		BuildID: spec.BuildID, BuildAttempt: spec.BuildAttempt, SpecSHA256: spec.SpecSHA256,
		ManifestDigest: spec.ManifestDigest, PolicyRevision: spec.PolicyRevision, PolicySHA256: spec.PolicySHA256,
		AdmissionRevision: spec.AdmissionRevision, AdmissionReceipt: spec.AdmissionReceiptSHA256,
		AdmissionReceiptOCIManifestDigest: spec.AdmissionReceiptOCIManifestDigest,
		ClaimantWorkload:                  spec.PromotionClaimantWorkloadID, ClaimantSPIFFEID: spec.PromotionClaimantSPIFFEID,
		AuthorityGeneration: spec.PromotionAuthorityGeneration, Fence: spec.PromotionFence,
		JTI: spec.PromotionClaimJTISHA256, ExpiresAt: spec.PromotionClaimExpiresAt}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", errs.ErrInternal
	}
	mac := hmac.New(sha256.New, service.leaseSigningKey)
	_, _ = mac.Write(raw)
	return base64.RawURLEncoding.EncodeToString(raw) + "." +
		base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

func (service *Service) verifyPromotionClaim(
	compact string,
	resource entity.Resource,
	spec entity.ImageArtifactSpec,
	now time.Time,
) error {
	parts := strings.Split(compact, ".")
	if len(parts) != 2 {
		return errs.ErrStateConflict
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil || len(raw) == 0 || len(raw) > 4096 {
		return errs.ErrStateConflict
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return errs.ErrStateConflict
	}
	mac := hmac.New(sha256.New, service.leaseSigningKey)
	_, _ = mac.Write(raw)
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return errs.ErrStateConflict
	}
	var payload promotionClaimPayload
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&payload); err != nil || decoder.Decode(&struct{}{}) != io.EOF || payload.Version != 1 ||
		payload.ArtifactID != resource.ID || payload.ArtifactVersion != resource.Version ||
		payload.BuildID != spec.BuildID || payload.BuildAttempt != spec.BuildAttempt ||
		payload.SpecSHA256 != spec.SpecSHA256 || payload.ManifestDigest != spec.ManifestDigest ||
		payload.PolicyRevision != spec.PolicyRevision || payload.PolicySHA256 != spec.PolicySHA256 ||
		payload.AdmissionRevision != spec.AdmissionRevision || payload.AdmissionReceipt != spec.AdmissionReceiptSHA256 ||
		payload.AdmissionReceiptOCIManifestDigest != spec.AdmissionReceiptOCIManifestDigest ||
		payload.ClaimantWorkload != spec.PromotionClaimantWorkloadID ||
		payload.ClaimantSPIFFEID != spec.PromotionClaimantSPIFFEID ||
		payload.AuthorityGeneration != spec.PromotionAuthorityGeneration || payload.Fence != spec.PromotionFence ||
		payload.JTI != spec.PromotionClaimJTISHA256 || !payload.ExpiresAt.Equal(spec.PromotionClaimExpiresAt) ||
		!now.Before(payload.ExpiresAt) {
		return errs.ErrStateConflict
	}
	return nil
}

func (service *Service) imageLeaseToken(buildID string, attempt uint32, fence, generation uint64, key string) string {
	return service.imageToken("build", buildID, "", attempt, fence, generation, key)
}

func (service *Service) imageAdmissionToken(
	artifactID, digest string,
	attempt uint32,
	fence, generation uint64,
	key string,
) string {
	return service.imageToken("admission", artifactID, digest, attempt, fence, generation, key)
}

func (service *Service) imageToken(kind, id, digest string, attempt uint32, fence, generation uint64, key string) string {
	mac := hmac.New(sha256.New, service.leaseSigningKey)
	_, _ = fmt.Fprintf(mac, "%s\x00%s\x00%s\x00%d\x00%d\x00%d\x00%s", kind, id, digest,
		attempt, fence, generation, hashString(key))
	return hex.EncodeToString(mac.Sum(nil))
}

func canonicalRoleImageInput(input entity.RoleImageRecipeInput) entity.RoleImageRecipeInput {
	result := input
	result.Platforms = slices.Clone(input.Platforms)
	result.Packages = slices.Clone(input.Packages)
	result.Tools = slices.Clone(input.Tools)
	slices.SortFunc(result.Platforms, func(left, right entity.RoleImagePlatform) int {
		return strings.Compare(left.OS+"/"+left.Architecture+"/"+left.Variant,
			right.OS+"/"+right.Architecture+"/"+right.Variant)
	})
	slices.SortFunc(result.Packages, func(left, right entity.RoleImagePackage) int {
		return strings.Compare(left.Manager+"/"+left.Name+"/"+left.Version+"/"+left.Digest+"/"+left.SourceRef,
			right.Manager+"/"+right.Name+"/"+right.Version+"/"+right.Digest+"/"+right.SourceRef)
	})
	slices.SortFunc(result.Tools, func(left, right entity.RoleImageTool) int {
		return strings.Compare(left.Name+"/"+left.Version+"/"+left.SourceRef+"/"+left.SHA256,
			right.Name+"/"+right.Version+"/"+right.SourceRef+"/"+right.SHA256)
	})
	return result
}

func clearImageBuildClaim(spec *entity.ImageBuildSpec) {
	spec.ClaimantWorkloadID, spec.ClaimantSPIFFEID = "", ""
	spec.LeaseExpiresAt = time.Time{}
	spec.LeaseTokenSHA256, spec.ClaimJTISHA256 = "", ""
}

func clearPromotionClaim(spec *entity.ImageArtifactSpec) {
	spec.PromotionClaimJTISHA256 = ""
	spec.PromotionClaimExpiresAt = time.Time{}
	spec.PromotionClaimantWorkloadID = ""
	spec.PromotionClaimantSPIFFEID = ""
	spec.PromotionAuthorizationTokenSHA256 = ""
	spec.PromotionAuthorizationExpiresAt = time.Time{}
}

func authorizeImageWorkload(principal value.Principal, permission, workload, spiffeID string) error {
	if err := authorize(principal, permission); err != nil {
		return err
	}
	expectedSource := map[string]string{
		"role-image-builder": "IMAGE_BUILD",
		"image-admission":    "IMAGE_ARTIFACT",
		"image-promotion":    "IMAGE_PROMOTION_CLAIM",
	}[workload]
	if expectedSource == "" || principal.CallerWorkload != workload || principal.CallerSPIFFEID != spiffeID ||
		principal.AuthoritySource != expectedSource || principal.ProjectID == "" {
		return errs.ErrPermissionDenied
	}
	return nil
}

func validRoleImageRecipeAction(action RoleImageRecipeAction) bool {
	switch action {
	case RoleImageRecipeCreate, RoleImageRecipeUpdate, RoleImageRecipeArchive,
		RoleImageRecipeRestore, RoleImageRecipeDelete, RoleImageRecipeRequestBuild:
		return true
	default:
		return false
	}
}

func validImageBuildOwnerAction(action ImageBuildOwnerAction) bool {
	switch action {
	case ImageBuildCancel, ImageBuildRetry, ImageBuildExpire, ImageBuildDeadLetter:
		return true
	default:
		return false
	}
}

func validImageBuildErrorCode(code string) bool {
	switch code {
	case "MATERIALIZATION_FAILED", "CONTEXT_INVALID", "BASE_PULL_FAILED", "SOLVE_FAILED", "INSTALLATION_FAILED", "RUNTIME_FINALIZATION_FAILED", "STAGING_PUSH_FAILED",
		"PROVENANCE_INVALID", "LEASE_LOST", "DEPENDENCY_UNAVAILABLE", "BUILD_CANCELLED":
		return true
	default:
		return false
	}
}

func validImageBuildDiagnostic(code, summary string) bool {
	if code == "" && summary == "" {
		return true
	}
	if summary == "" || len(summary) > 256 || strings.TrimSpace(summary) != summary || strings.ContainsAny(summary, "\r\n\x00") {
		return false
	}
	switch code {
	case "INPUT_FETCH_REJECTED", "INPUT_DIGEST_MISMATCH", "ARCHIVE_REJECTED", "BASE_RESOLUTION_REJECTED",
		"BUILD_GRAPH_REJECTED", "INSTALL_COMMAND_REJECTED", "RUNTIME_FINALIZATION_REJECTED", "STAGING_EXPORT_REJECTED", "PROVENANCE_REJECTED",
		"DEPENDENCY_TIMEOUT", "LEASE_REVOKED":
		return true
	default:
		return false
	}
}

func buildProgressStage(stage entity.ImageBuildStage) bool {
	return stage == entity.ImageBuildStageMaterialization || stage == entity.ImageBuildStageContextValidation ||
		stage == entity.ImageBuildStageBasePull || stage == entity.ImageBuildStageSolving ||
		stage == entity.ImageBuildStageInstallation || stage == entity.ImageBuildStageTrustedRuntimeFinalization ||
		stage == entity.ImageBuildStageStagingPush ||
		stage == entity.ImageBuildStageProvenance
}

func imageBuildStageOrder(stage entity.ImageBuildStage) int {
	switch stage {
	case entity.ImageBuildStageQueued:
		return 0
	case entity.ImageBuildStageMaterialization:
		return 1
	case entity.ImageBuildStageContextValidation:
		return 2
	case entity.ImageBuildStageBasePull:
		return 3
	case entity.ImageBuildStageSolving:
		return 4
	case entity.ImageBuildStageInstallation:
		return 5
	case entity.ImageBuildStageTrustedRuntimeFinalization:
		return 6
	case entity.ImageBuildStageStagingPush:
		return 7
	case entity.ImageBuildStageProvenance:
		return 8
	default:
		return 100
	}
}

func validImageRepository(input string) bool {
	return len(input) >= 8 && len(input) <= 255 && input == strings.TrimSpace(input) &&
		!strings.ContainsAny(input, "@?# *") && strings.Contains(input, "/")
}

func validDigest(input string) bool {
	return strings.HasPrefix(input, "sha256:") && validSHA256Text(strings.TrimPrefix(input, "sha256:"))
}
