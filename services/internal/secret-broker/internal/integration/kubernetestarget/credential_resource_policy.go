package kubernetestarget

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"
)

// validateCredentialResourcePolicy verifies the private owner's typed policy/checksum pair.
// This does not establish owner authority or freshness: the private Resolve mapping and every
// upper Begin/renew must already bind the current target revision/digest to the root invocation.
// validateCredentialResourcePolicy сверяет typed policy/checksum pair приватного owner.
// Owner authority/freshness этим не доказываются: private Resolve mapping и каждый upper
// Begin/renew уже связывают current target revision/digest с root invocation.
func validateCredentialResourcePolicy(target Target) error {
	if target.ResourcePolicy == nil {
		if target.ResourcePolicySHA256 != "" {
			return errors.New(invalidCredential)
		}
		return nil
	}
	if target.ResourcePolicy.Validate() != nil {
		return errors.New(invalidCredential)
	}
	raw, err := json.Marshal(target.ResourcePolicy)
	if err != nil {
		return errors.New(invalidCredential)
	}
	digest := sha256.Sum256(raw)
	if target.ResourcePolicySHA256 != hex.EncodeToString(digest[:]) {
		return errors.New(invalidCredential)
	}
	return nil
}

// cloneCredentialResourcePolicy detaches the operator metadata arrays before retaining a private
// credential. Profile values contain only strings/bools; later caller mutations cannot change
// the parsed credential's policy or checksum while a proposal is waiting for owner admission.
// cloneCredentialResourcePolicy отделяет operator metadata arrays до сохранения private
// credential. Profile values содержат только strings/bools; поздние caller mutations не меняют
// policy/checksum parsed credential, пока proposal ждёт owner admission.
func cloneCredentialResourcePolicy(target *Target) {
	if target.ResourcePolicy == nil {
		return
	}
	profile := *target.ResourcePolicy
	profile.Storage = slices.Clone(profile.Storage)
	profile.Controllers = slices.Clone(profile.Controllers)
	target.ResourcePolicy = &profile
}
