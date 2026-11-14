package integration

import "github.com/codex-k8s/kodex/libs/go/integrationpackage"

// decodeTypedProviderJSON rejects duplicate keys, trailing documents and noncanonical case aliases of known fields before typed semantic use; distinct case-sensitive user label keys remain valid and unknown provider additions stay unprojected.
// decodeTypedProviderJSON отклоняет duplicate keys, trailing documents и noncanonical case aliases известных полей до typed semantic use; разные регистрозависимые label keys допустимы, неизвестные provider additions не выдаются.
func decodeTypedProviderJSON(raw []byte, target any) error {
	if integrationpackage.ValidateProviderJSON(raw) != nil {
		return &SafeError{Code: providerResponseInvalidCode}
	}
	return observabilityDecode(raw, target)
}
