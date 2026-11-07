package integrationegresspolicy

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/netip"

	"github.com/codex-k8s/kodex/libs/go/mailpolicy"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

// errNativeDocument is a closed diagnostic that never includes operator endpoints or payloads.
// errNativeDocument — закрытая диагностика без endpoints оператора и payloads.
var errNativeDocument = errors.New("native integration document is invalid")

const (
	// NativeSchema distinguishes operator-pinned native routes from public-DNS OpenAPI projections.
	// NativeSchema отличает operator-pinned native routes от публичных DNS-проекций OpenAPI.
	NativeSchema = "egress-native-integration/v1"
	// NativeProfileName identifies only the separately restricted native CONNECT listener.
	// NativeProfileName обозначает только отдельно ограниченный native CONNECT listener.
	NativeProfileName = "integration-native"
	// NativeOperation is network reachability metadata, never an application capability or grant.
	// NativeOperation — сетевая metadata, а не прикладная capability либо grant.
	NativeOperation = "integration.native"
	// NativePort keeps the new optional peer isolated from core, runtime and OpenAPI listeners.
	// NativePort изолирует новый optional peer от core, runtime и OpenAPI listeners.
	NativePort = 8085
)

// NativeDocument is an immutable deployment-owned route set. No Connection, prompt or provider
// response may expand it; application permissions and credential ownership remain in control-plane.
// NativeDocument — неизменяемый набор маршрутов установки. Connection, prompt и ответ API его
// не расширяют; прикладные права и владение credential остаются у control-plane.
type NativeDocument struct {
	Schema              string              `json:"schema"`
	Generation          int64               `json:"generation"`
	GatewayPolicyDigest string              `json:"gateway_policy_digest"`
	Destinations        []NativeDestination `json:"destinations"`
}

// NativeDestination pins one canonical TLS name to explicit operator addresses on port443.
// A private unicast address is meaningful only inside this exact immutable deployment profile.
// NativeDestination закрепляет одно canonical TLS имя за явными адресами оператора на443.
// Private unicast address допустим только в этом точном immutable профиле установки.
type NativeDestination struct {
	Hostname     string   `json:"hostname"`
	Port         int      `json:"port"`
	AddressCIDRs []string `json:"address_cidrs"`
}

// Validate rejects wildcard/URL/IP names, broad or duplicate pins and special-purpose ranges.
// Only RFC1918/ULA or the existing public-address policy is admitted; the global DNS policy is unchanged.
// Validate отклоняет wildcard/URL/IP имена, широкие и повторные pins, специальные диапазоны.
// Допустимы лишь RFC1918/ULA либо прежняя public-address policy; общая DNS policy не меняется.
func (destination NativeDestination) Validate() error {
	if _, err := mailpolicy.NormalizeHostname(destination.Hostname); err != nil || destination.Port != 443 ||
		!runtimecontract.ValidOperatorAddressPins(destination.AddressCIDRs) {
		return errors.New("native integration destination is invalid")
	}
	previous := ""
	for _, raw := range destination.AddressCIDRs {
		prefix, err := netip.ParsePrefix(raw)
		if err != nil || raw <= previous || !prefix.Addr().IsPrivate() && mailpolicy.ValidateAddresses([]netip.Addr{prefix.Addr()}) != nil {
			return errors.New("native integration address pin is invalid")
		}
		previous = raw
	}
	return nil
}

// Validate binds the optional native document to a complete bounded sorted route set and base
// gateway digest. An empty set, unknown schema or missing expected authority cannot enable CONNECT.
// Validate связывает optional native document с полным bounded sorted набором и base gateway
// digest. Пустой набор, неизвестная schema и отсутствующая authority не включают CONNECT.
func (document NativeDocument) Validate() error {
	if document.Schema != NativeSchema || document.Generation < 1 || !validDigest(document.GatewayPolicyDigest) ||
		len(document.Destinations) == 0 || len(document.Destinations) > MaximumDestinations {
		return errNativeDocument
	}
	previous := ""
	for _, destination := range document.Destinations {
		if destination.Hostname <= previous || destination.Validate() != nil {
			return errNativeDocument
		}
		previous = destination.Hostname
	}
	raw, err := json.Marshal(document)
	if err != nil || len(raw) > MaximumFileBytes {
		return errors.New("native integration document size is invalid")
	}
	return nil
}

// Digest hashes exact typed wire bytes, including generation, base policy and all address pins.
// Digest хэширует точные typed wire bytes, включая generation, base policy и все адресные pins.
func (document NativeDocument) Digest() string {
	raw, _ := json.Marshal(document)
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}

// SelectorValue fences Service traffic to this policy generation during a rolling update. The
// full digest remains in immutable configuration/readback; this bounded label fits Kubernetes limits.
// SelectorValue связывает Service с поколением policy при rolling update. Полный digest остаётся
// в immutable configuration/readback; ограниченный label соответствует пределам Kubernetes.
func (document NativeDocument) SelectorValue() string {
	return "native-" + document.Digest()[:48]
}
