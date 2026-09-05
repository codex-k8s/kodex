package draftowner

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"
	"unicode/utf8"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/runtimesecret"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/repository/secretdrafts"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/types/value"
	"google.golang.org/protobuf/types/known/timestamppb"
	"k8s.io/apimachinery/pkg/util/validation"
)

func castWork(v *cp.RuntimeSecretDraftWork, recovery bool) (value.DraftWork, error) {
	if v == nil {
		return value.DraftWork{}, secretdrafts.ErrConflict
	}
	draft, err := castDraft(v.GetDraft())
	if err != nil {
		return value.DraftWork{}, err
	}
	w := value.DraftWork{OperationRef: v.GetOperationRef(), Kind: value.DraftOperation(strings.TrimPrefix(v.GetKind().String(), "RUNTIME_SECRET_DRAFT_OPERATION_KIND_")), ClaimantID: v.GetClaimantId(), ClaimGeneration: v.GetClaimGeneration(), Draft: draft, StagedNamespace: v.GetStagedNamespace(), StagedName: v.GetStagedSecretName(), StagedKey: v.GetStagedSecretKey(), RuntimeNamespace: v.GetNamespace(), TargetRevision: v.GetTargetRevision(), ExpiresAt: timestamp(v.GetExpiresAt()), LeaseDeadline: timestamp(v.GetLeaseDeadline())}
	w.Binding = value.SecretDraftBinding{ProjectRef: draft.ProjectRef, SecretRef: draft.SecretRef, DraftRef: draft.Ref, DraftGeneration: draft.Generation, ValueType: draft.ValueType, ContentSHA256: v.GetExpectedContentSha256()}
	if v.GetEncrypted() != nil {
		descriptor := castEncrypted(v.GetEncrypted())
		if !validEncrypted(w, descriptor) {
			return value.DraftWork{}, secretdrafts.ErrConflict
		}
		w.Encrypted = &descriptor
	}
	if !validNativeWork(w, recovery) {
		return value.DraftWork{}, secretdrafts.ErrConflict
	}
	if v.GetRecoveryEncrypted() != nil {
		descriptor := castEncrypted(v.GetRecoveryEncrypted())
		if !recovery || !validEncrypted(w, descriptor) {
			return value.DraftWork{}, secretdrafts.ErrConflict
		}
		w.RecoveryEncrypted = &descriptor
	}
	if v.GetRecoveryMaterialization() != nil {
		p := v.GetRecoveryMaterialization()
		descriptor := value.DraftMaterialization{Namespace: p.GetNamespace(), Name: p.GetSecretName(), DataKey: p.GetSecretKey(), UID: p.GetSecretUid(), ResourceVersion: p.GetSecretResourceVersion(), ContentSHA256: p.GetContentSha256(), Revision: w.TargetRevision}
		if !recovery || !validMaterialized(w, descriptor) {
			return value.DraftWork{}, secretdrafts.ErrConflict
		}
		w.RecoveryMaterialization = &descriptor
	}
	return w, nil
}

func validNativeWork(w value.DraftWork, recovery bool) bool {
	if !reference(w.OperationRef) || w.Binding.Validate() != nil || w.Draft.Ref != w.Binding.DraftRef || w.Draft.Generation != w.Binding.DraftGeneration || w.Draft.ProjectRef != w.Binding.ProjectRef || w.Draft.SecretRef != w.Binding.SecretRef || w.Draft.ValueType != w.Binding.ValueType || w.Draft.Version < 1 || w.ExpiresAt.IsZero() || len(validation.IsDNS1123Label(w.RuntimeNamespace)) != 0 || len(validation.IsDNS1123Label(w.StagedNamespace)) != 0 || w.StagedKey != "ciphertext" {
		return false
	}
	digest := sha256.Sum256([]byte(w.Draft.Ref))
	if w.StagedName != "runtime-secret-draft-"+hex.EncodeToString(digest[:16]) {
		return false
	}
	if w.ClaimGeneration == 0 && w.ClaimantID == "" {
		if !recovery || !w.LeaseDeadline.IsZero() {
			return false
		}
	} else if w.ClaimGeneration < 1 || !reference(w.ClaimantID) || w.LeaseDeadline.IsZero() {
		return false
	}
	switch w.Kind {
	case value.DraftPublish:
		if w.TargetRevision < 1 {
			return false
		}
	case value.DraftSave, value.DraftValidate, value.DraftDiscard:
		if w.TargetRevision != 0 {
			return false
		}
	default:
		return false
	}
	return true
}

func castDraft(v *cp.RuntimeSecretDraft) (value.SecretDraft, error) {
	if v == nil {
		return value.SecretDraft{}, secretdrafts.ErrConflict
	}
	draft := value.SecretDraft{Ref: v.GetRef(), Version: v.GetVersion(), Generation: v.GetGeneration(), ProjectRef: v.GetProjectRef(), SecretRef: v.GetSecretRef(), Name: v.GetName(), Description: v.GetDescription(), ValueType: strings.TrimPrefix(v.GetValueType().String(), "RUNTIME_SECRET_VALUE_TYPE_"), State: strings.TrimPrefix(v.GetState().String(), "RUNTIME_SECRET_DRAFT_STATE_"), PublishedRevision: v.GetPublishedRevision(), CreatedAt: timestamp(v.GetCreatedAt()), UpdatedAt: timestamp(v.GetUpdatedAt()), ExpiresAt: timestamp(v.GetExpiresAt())}
	draft.SecretVersion = v.GetSecretVersion()
	if draft.SecretVersion < 1 {
		return value.SecretDraft{}, secretdrafts.ErrConflict
	}
	if !reference(draft.Ref) || !reference(draft.ProjectRef) || !reference(draft.SecretRef) || draft.Version < 1 || draft.Generation < 1 || draft.PublishedRevision < 0 || !boundedText(draft.Name, 128, false) || !boundedText(draft.Description, 4096, true) || draft.CreatedAt.IsZero() || draft.UpdatedAt.Before(draft.CreatedAt) || draft.ExpiresAt.IsZero() {
		return value.SecretDraft{}, secretdrafts.ErrConflict
	}
	if _, err := runtimesecret.VersionedKubernetesName(draft.SecretRef, 1); err != nil {
		return value.SecretDraft{}, secretdrafts.ErrConflict
	}
	switch draft.ValueType {
	case "STRING", "BINARY", "JSON":
	default:
		return value.SecretDraft{}, secretdrafts.ErrConflict
	}
	switch draft.State {
	case "PREPARING", "DRAFT", "VALID", "PUBLISHING", "DISCARDED", "EXPIRED", "FAILED":
	case "PUBLISHED":
		if draft.PublishedRevision < 1 {
			return value.SecretDraft{}, secretdrafts.ErrConflict
		}
	default:
		return value.SecretDraft{}, secretdrafts.ErrConflict
	}
	return draft, nil
}

func sameDraft(actual, expected value.SecretDraft) bool {
	return actual.Ref == expected.Ref && actual.ProjectRef == expected.ProjectRef && actual.SecretRef == expected.SecretRef && actual.Generation == expected.Generation && actual.ValueType == expected.ValueType && actual.Name == expected.Name && actual.Description == expected.Description && actual.Version >= expected.Version && actual.CreatedAt.Equal(expected.CreatedAt) && actual.ExpiresAt.Equal(expected.ExpiresAt)
}

func castResult(d *cp.RuntimeSecretDraft, s *cp.RuntimeSecret, work value.DraftWork) (value.DraftResult, error) {
	draft, err := castDraft(d)
	if err != nil || !sameDraft(draft, work.Draft) {
		return value.DraftResult{}, secretdrafts.ErrConflict
	}
	expected := ""
	switch work.Kind {
	case value.DraftSave:
		expected = "DRAFT"
	case value.DraftValidate:
		expected = "VALID"
	case value.DraftPublish:
		expected = "PUBLISHED"
	case value.DraftDiscard:
		expected = "DISCARDED"
	}
	if draft.State != expected {
		return value.DraftResult{}, secretdrafts.ErrConflict
	}
	result := value.DraftResult{Draft: draft}
	if work.Kind != value.DraftPublish {
		if s != nil {
			return value.DraftResult{}, secretdrafts.ErrConflict
		}
		return result, nil
	}
	if s == nil || s.GetRef() != work.Draft.SecretRef || s.GetProjectRef() != work.Draft.ProjectRef || s.GetName() != work.Draft.Name || s.GetDescription() != work.Draft.Description || s.GetCurrentRevision() != work.TargetRevision || draft.PublishedRevision != work.TargetRevision || s.GetVersion() != draft.SecretVersion || s.GetState() != "ACTIVE" || strings.TrimPrefix(s.GetValueType().String(), "RUNTIME_SECRET_VALUE_TYPE_") != work.Draft.ValueType || timestamp(s.GetCreatedAt()).IsZero() || timestamp(s.GetUpdatedAt()).Before(timestamp(s.GetCreatedAt())) {
		return value.DraftResult{}, secretdrafts.ErrConflict
	}
	result.Secret = &value.PublishedSecret{Ref: s.GetRef(), ProjectRef: s.GetProjectRef(), Name: s.GetName(), Description: s.GetDescription(), ValueType: work.Draft.ValueType, Status: s.GetState(), Version: s.GetVersion(), Revision: s.GetCurrentRevision(), CreatedAt: timestamp(s.GetCreatedAt()), UpdatedAt: timestamp(s.GetUpdatedAt())}
	return result, nil
}

func requestDescriptors(work value.DraftWork, e *value.DraftEncryptedDescriptor, m *value.DraftMaterialization, recovery bool) (*cp.RuntimeSecretDraftEncryptedDescriptor, *cp.RuntimeSecretMaterialization, error) {
	if !validNativeWork(work, recovery) {
		return nil, nil, secretdrafts.ErrInvalid
	}
	var encrypted *cp.RuntimeSecretDraftEncryptedDescriptor
	if e != nil {
		if !validEncrypted(work, *e) {
			return nil, nil, secretdrafts.ErrInvalid
		}
		encrypted = &cp.RuntimeSecretDraftEncryptedDescriptor{Namespace: e.Namespace, SecretName: e.Name, SecretKey: e.DataKey, SecretUid: e.UID, SecretResourceVersion: e.ResourceVersion, CiphertextSha256: e.CiphertextSHA256, EncryptionKeyId: e.EncryptionKey.ID, EncryptionKeyGeneration: e.EncryptionKey.Generation}
	}
	var materialized *cp.RuntimeSecretMaterialization
	if m != nil {
		if !validMaterialized(work, *m) {
			return nil, nil, secretdrafts.ErrInvalid
		}
		materialized = &cp.RuntimeSecretMaterialization{Namespace: m.Namespace, SecretName: m.Name, SecretKey: m.DataKey, SecretUid: m.UID, SecretResourceVersion: m.ResourceVersion, ContentSha256: m.ContentSHA256}
	}
	return encrypted, materialized, nil
}

func castEncrypted(v *cp.RuntimeSecretDraftEncryptedDescriptor) value.DraftEncryptedDescriptor {
	return value.DraftEncryptedDescriptor{Namespace: v.GetNamespace(), Name: v.GetSecretName(), DataKey: v.GetSecretKey(), UID: v.GetSecretUid(), ResourceVersion: v.GetSecretResourceVersion(), CiphertextSHA256: v.GetCiphertextSha256(), EncryptionKey: value.DraftEncryptionKey{ID: v.GetEncryptionKeyId(), Generation: v.GetEncryptionKeyGeneration()}}
}
func validEncrypted(w value.DraftWork, e value.DraftEncryptedDescriptor) bool {
	return e.Namespace == w.StagedNamespace && e.Name == w.StagedName && e.DataKey == "ciphertext" && identityToken(e.UID) && identityToken(e.ResourceVersion) && digest(e.CiphertextSHA256) && digest(e.EncryptionKey.ID) && e.EncryptionKey.Generation > 0
}
func validMaterialized(w value.DraftWork, m value.DraftMaterialization) bool {
	name, err := runtimesecret.VersionedKubernetesName(w.Draft.SecretRef, w.TargetRevision)
	return err == nil && w.Kind == value.DraftPublish && w.ClaimGeneration > 0 && m.Namespace == w.RuntimeNamespace && m.Name == name && m.DataKey == "value" && m.Revision == w.TargetRevision && m.ContentSHA256 == w.Binding.ContentSHA256 && identityToken(m.UID) && identityToken(m.ResourceVersion)
}
func identityToken(s string) bool {
	if len(s) == 0 || len(s) > 128 {
		return false
	}
	for _, c := range s {
		if c < 0x21 || c > 0x7e {
			return false
		}
	}
	return true
}
func timestamp(v *timestamppb.Timestamp) time.Time {
	if v == nil || v.CheckValid() != nil {
		return time.Time{}
	}
	return v.AsTime()
}
func reference(s string) bool {
	if len(s) < 1 || len(s) > 128 {
		return false
	}
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-') {
			return false
		}
	}
	return true
}
func boundedText(s string, limit int, empty bool) bool {
	return (empty || len(s) > 0) && len(s) <= limit && utf8.ValidString(s) && !strings.ContainsRune(s, '\x00')
}
func digest(s string) bool {
	if len(s) != 64 || strings.ToLower(s) != s {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil
}
