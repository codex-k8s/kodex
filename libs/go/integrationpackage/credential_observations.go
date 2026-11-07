package integrationpackage

// CredentialObservationGitLabProjectWriteScopeV1 records supported current token/project metadata,
// never a globally verified WRITE or authority to skip the per-object execution check.
// CredentialObservationGitLabProjectWriteScopeV1 фиксирует поддержанные текущие token/project metadata,
// а не global WRITE либо полномочие пропустить проверку объекта при исполнении.
const CredentialObservationGitLabProjectWriteScopeV1 = "GITLAB_PROJECT_WRITE_SCOPE_V1"

// CredentialObservationRequests derives immutable metadata profiles and deniable write keys from
// exact executable package tuples. Unknown or modified tuples receive no observation request.
// CredentialObservationRequests выводит immutable metadata profiles и deniable write keys из точных
// executable package tuples. Неизвестные либо изменённые tuples не получают observation request.
func CredentialObservationRequests(definition Package) (profiles, deniedWriteKeys []string) {
	for _, capability := range definition.Spec.Capabilities {
		contract, exists := CredentialResourcePermissionContractFor(definition.Metadata.Key, capability.Key)
		if !exists || contract.RequiredObservationProfile == "" || CredentialResourcePermissionProfile(definition, capability) == "" {
			continue
		}
		if len(profiles) == 0 {
			profiles = []string{contract.RequiredObservationProfile}
		}
		deniedWriteKeys = append(deniedWriteKeys, capability.Key)
	}
	return profiles, deniedWriteKeys
}

// CredentialObservationMayDeny accepts only the closed profile/capability contract; the owner
// separately restricts this pair to its immutable requested package subset.
// CredentialObservationMayDeny принимает только закрытый profile/capability contract; owner
// отдельно ограничивает пару immutable запрошенным подмножеством package.
func CredentialObservationMayDeny(profile, key string) bool {
	contract, exists := CredentialResourcePermissionContractFor("gitlab", key)
	return exists && profile == CredentialObservationGitLabProjectWriteScopeV1 && contract.RequiredObservationProfile == profile
}
