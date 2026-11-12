package callback

import (
	"encoding/json"
	"strings"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

// runtimeToolMutationKey scopes verified native transport identities to their owner-assigned session.
// Only the isolated provider bridge mints ncall IDs from the image hook; IDs grant no permission.
// Other clients retain their exact existing lease-scoped idempotency protocol.
// runtimeToolMutationKey ограничивает verified native transport IDs назначенной владельцем session.
// Только isolated provider bridge формирует ncall IDs из image hook; IDs не выдают полномочий.
// Остальные clients сохраняют прежний lease-scoped idempotency protocol.
func runtimeToolMutationKey(input runtimecontract.RunnerInput, callID json.RawMessage) string {
	var id string
	if input.Provider == "opencode" && input.SessionRef != "" && json.Unmarshal(callID, &id) == nil && strings.HasPrefix(id, "ncall_") && len(id) == 70 {
		valid := true
		for _, c := range id[6:] {
			valid = valid && (c >= 'a' && c <= 'f' || c >= '0' && c <= '9')
		}
		if valid {
			return stableKey(input.SessionRef, id)
		}
	}
	return stableKey(input.LeaseRef, string(callID))
}
