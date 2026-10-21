package mattermost

import (
	"context"
	"strconv"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/mattermost/mattermost/server/public/model"
)

const (
	// gateRefProperty correlates the bot's post with an owner gate; the property alone does not authorize a decision.
	// RU: gateRefProperty связывает сообщение бота с owner gate; само свойство не разрешает решение.
	gateRefProperty = "kodex_gate_ref"
	// gateVersionProperty stores the gate version as canonical decimal text to avoid JSON numeric precision loss.
	// RU: gateVersionProperty сохраняет версию gate каноническим десятичным текстом без потери точности JSON-числа.
	gateVersionProperty = "kodex_gate_version"
	// gateRunProperty pins the correlated run in the bot post's gate tuple.
	// RU: gateRunProperty закрепляет связанный запуск в связке gate сообщения бота.
	gateRunProperty = "kodex_run_ref"
)

// gateContext holds the exact gate reference, positive version and run reference used solely for owner-verified correlation.
// RU: gateContext хранит точные ссылку gate, положительную версию и ссылку запуска только для проверяемой владельцем корреляции.
type gateContext struct {
	ref     string
	version int64
	runRef  string
}

// gateFromClaim extracts a complete tuple only for gate-decision delivery and rejects unexpected gate fields on other capability kinds.
// RU: gateFromClaim извлекает полную связку только для доставки решения gate и отклоняет неожиданные поля gate у остальных capabilities.
func gateFromClaim(claim *controlplanev1.InteractionDeliveryClaim) (*gateContext, error) {
	if claim.GetCapabilityKey() != "mattermost.gate_decisions" {
		if claim.GetGateRef() != "" || claim.GetGateVersion() != 0 {
			return nil, errConfiguration
		}
		return nil, nil
	}
	gate := &gateContext{ref: claim.GetGateRef(), version: claim.GetGateVersion(), runRef: claim.GetRunRef()}
	if !gate.valid() {
		return nil, errConfiguration
	}
	return gate, nil
}

// valid checks bounded gate/run references and a positive version without asserting current owner state or permission.
// RU: valid проверяет ограниченные ссылки gate/запуска и положительную версию без утверждения текущего состояния владельца или полномочий.
func (gate *gateContext) valid() bool {
	return gate != nil && boundedReference(gate.ref) && boundedReference(gate.runRef) && gate.version > 0
}

// boundedReference accepts one to 128 ASCII letters, digits, underscores or hyphens and performs no resource lookup.
// RU: boundedReference допускает от одного до 128 ASCII-букв, цифр, подчёркиваний или дефисов без поиска ресурса.
func boundedReference(value string) bool {
	if len(value) == 0 || len(value) > 128 {
		return false
	}
	for _, c := range value {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-') {
			return false
		}
	}
	return true
}

// addToPost writes gate correlation properties, formatting version as decimal text rather than a JSON number.
// RU: addToPost записывает свойства корреляции gate, форматируя версию десятичным текстом, а не JSON-числом.
func (gate *gateContext) addToPost(post *model.Post) {
	post.AddProp(gateRefProperty, gate.ref)
	post.AddProp(gateVersionProperty, strconv.FormatInt(gate.version, 10))
	post.AddProp(gateRunProperty, gate.runRef)
}

// gateFromPost distinguishes absent gate properties from a complete canonical string tuple and rejects partial, numeric or noncanonical versions.
// RU: gateFromPost различает отсутствие свойств gate и полную каноническую строковую связку, отклоняя неполные, числовые или неканонические версии.
func gateFromPost(post *model.Post) (*gateContext, error) {
	ref, version, runRef := post.GetProp(gateRefProperty), post.GetProp(gateVersionProperty), post.GetProp(gateRunProperty)
	if ref == nil && version == nil && runRef == nil {
		return nil, nil
	}
	refString, refOK := ref.(string)
	versionString, versionOK := version.(string)
	runString, runOK := runRef.(string)
	if !refOK || !versionOK || !runOK {
		return nil, errResponse
	}
	number, err := strconv.ParseInt(versionString, 10, 64)
	gate := &gateContext{ref: refString, version: number, runRef: runString}
	if err != nil || !gate.valid() || strconv.FormatInt(number, 10) != versionString {
		return nil, errResponse
	}
	return gate, nil
}

// readGateContext re-reads the exact live root in the same channel and extracts correlation only when the bot authored that root.
// RU: readGateContext перечитывает точный живой корень в том же канале и извлекает корреляцию лишь при авторстве бота.
// Owner acceptance independently checks user binding, delivery receipt and gate version; user-authored properties are not trusted.
// RU: Приём у владельца независимо проверяет привязку пользователя, квитанцию доставки и версию gate; свойствам пользователя доверие не выдаётся.
func readGateContext(ctx context.Context, client *model.Client4, post *model.Post, channelID, botID string) (*gateContext, error) {
	if post.RootId == "" {
		return nil, nil
	}
	root, _, err := client.GetPost(ctx, post.RootId, "")
	if err != nil {
		return nil, classify(err)
	}
	if root == nil || root.Id != post.RootId || root.ChannelId != channelID || root.RootId != "" || root.DeleteAt != 0 {
		return nil, errResponse
	}
	if root.UserId != botID {
		return nil, nil
	}
	// Props только коррелируют с серверной delivery; полномочия и версия
	// повторно проверяются владельцем после разрешения пользовательской привязки.
	return gateFromPost(root)
}
