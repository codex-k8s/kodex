// Package integrationegresspolicy binds an owner-read OpenAPI origin snapshot
// to public DNS pins and the integration HTTPS CONNECT listener. It defines
// the immutable projection and render format, not caller authority.
// RU: Пакет integrationegresspolicy связывает прочитанный у владельца снимок
// OpenAPI origins с публичными DNS pins и HTTPS CONNECT listener интеграций.
// Он задаёт immutable-проекцию и формат render, а не полномочия вызывающего.
package integrationegresspolicy

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/netip"
	"slices"

	"github.com/codex-k8s/kodex/libs/go/mailpolicy"
)

const (
	// Schema fixes the decoder version of the projection document.
	// RU: Schema закрепляет версию декодера документа проекции.
	Schema = "egress-integration/v1"
	// ProfileName labels the dedicated OpenAPI integration egress profile.
	// RU: ProfileName обозначает отдельный сетевой профиль OpenAPI-интеграций.
	ProfileName = "integration-openapi"
	// Workload names the gateway receiving this exact projection.
	// RU: Workload обозначает шлюз, получающий эту точную проекцию.
	Workload = "integration-gateway"
	// Operation names the integration OpenAPI egress action, not a grant.
	// RU: Operation обозначает операцию OpenAPI egress, но не является разрешением.
	Operation = "integration.openapi"
	// MaximumFileBytes bounds the serialized immutable ConfigMap payload.
	// RU: MaximumFileBytes ограничивает сериализованные данные immutable ConfigMap.
	MaximumFileBytes = 64 << 10
	// MaximumDestinations caps the number of exact HTTPS origins per document.
	// RU: MaximumDestinations ограничивает число точных HTTPS origins в документе.
	MaximumDestinations = 64
)

// Document contains an owner-projection generation, source/policy digests and
// exact network pins. It excludes connection credentials and tool input;
// its digest is not itself proof of owner authorization.
// RU: Document содержит поколение owner-проекции, дайджесты источника и
// политики и точные сетевые pins. Реквизиты подключения и ввод инструмента
// исключены; дайджест сам по себе не доказывает разрешение владельца.
type Document struct {
	Schema              string        `json:"schema"`
	Generation          int64         `json:"generation"`
	SourceDigest        string        `json:"sourceDigest"`
	GatewayPolicyDigest string        `json:"gatewayPolicyDigest"`
	Destinations        []Destination `json:"destinations"`
}

// Destination binds one normalized hostname to HTTPS port 443 and a bounded,
// ordered set of public IP addresses resolved for that hostname.
// RU: Destination связывает нормализованное имя узла с HTTPS-портом443 и
// ограниченным упорядоченным набором публичных IP-адресов этого имени.
type Destination struct {
	Hostname  string   `json:"hostname"`
	Port      int      `json:"port"`
	Addresses []string `json:"addresses"`
}

// Validate rejects an invalid schema, generation, digest shape, unsorted or
// duplicate hosts and addresses, non-public/mixed DNS pins, non-443 ports and
// a source digest inconsistent with the complete hostname set. It validates
// policy-digest syntax, not the external policy state itself.
// RU: Validate отклоняет неверные schema, поколение и форму дайджеста,
// неупорядоченные или повторные узлы и адреса, непубличные/смешанные DNS pins,
// порт не443 и несоответствие дайджеста полному набору имён. Она проверяет
// синтаксис дайджеста политики, а не внешнее состояние самой политики.
func (document Document) Validate() error {
	invalid := errors.New("integration egress document is invalid")
	if document.Schema != Schema || document.Generation < 1 || !validDigest(document.SourceDigest) ||
		!validDigest(document.GatewayPolicyDigest) || document.Destinations == nil ||
		len(document.Destinations) > MaximumDestinations {
		return invalid
	}
	previousHost := ""
	hostnames := make([]string, 0, len(document.Destinations))
	for _, destination := range document.Destinations {
		if _, err := mailpolicy.NormalizeHostname(destination.Hostname); err != nil ||
			destination.Port != 443 || destination.Hostname <= previousHost ||
			len(destination.Addresses) == 0 || len(destination.Addresses) > 32 {
			return invalid
		}
		previousHost = destination.Hostname
		hostnames = append(hostnames, destination.Hostname)
		addresses := make([]netip.Addr, 0, len(destination.Addresses))
		previousAddress := ""
		for _, raw := range destination.Addresses {
			address, err := netip.ParseAddr(raw)
			if err != nil || address.String() != raw || raw <= previousAddress {
				return invalid
			}
			previousAddress = raw
			addresses = append(addresses, address)
		}
		if mailpolicy.ValidateAddresses(addresses) != nil {
			return invalid
		}
	}
	if document.SourceDigest != SourceDigest(hostnames) {
		return invalid
	}
	return nil
}

// validDigest requires exactly 32 bytes of lowercase canonical hex, so a
// differently encoded or truncated SHA-256 value cannot pass as a pin.
// RU: validDigest требует ровно32 байта канонического строчного hex, поэтому
// иначе закодированный или усечённый SHA-256 не проходит как pin.
func validDigest(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size && hex.EncodeToString(decoded) == value
}

// Digest hashes this document's encoding/json representation, including
// generation and exact destination pins, for immutable object naming.
// RU: Digest хэширует представление документа в encoding/json, включая
// поколение и точные pins, для имени immutable-объекта.
func (document Document) Digest() string {
	raw, _ := json.Marshal(document)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// SourceDigest hashes the sorted hostname sequence; callers validate
// uniqueness before using it as the complete owner-origin digest. DNS
// addresses are bound separately by Document.Digest.
// RU: SourceDigest хэширует отсортированную последовательность имён; вызывающий
// код проверяет уникальность до её использования как дайджеста всех owner
// origins. DNS-адреса отдельно входят в Document.Digest.
func SourceDigest(hostnames []string) string {
	ordered := append([]string{}, hostnames...)
	slices.Sort(ordered)
	raw, _ := json.Marshal(ordered)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
