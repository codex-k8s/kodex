package integration

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"
)

// TestCatalogPageProjectionKeepsWireAndReceipt fixes literal wire bytes for empty/nil/text/escaped/typed lists and the resulting receipt digest.
// No provider client or credentials are created; caller input bindings and omitted zero cursor must survive the shared serialization path.
// TestCatalogPageProjectionKeepsWireAndReceipt закрепляет буквальные байты пустых, nil, текстовых, экранированных и типизированных списков и дайджест квитанции.
// Клиент провайдера и реквизиты не создаются; привязки входа и отсутствие нулевого курсора должны сохраниться при общей сериализации.
func TestCatalogPageProjectionKeepsWireAndReceipt(t *testing.T) {
	t.Parallel()
	request := Request{Operation: "provider.list", EffectKey: "effect-fixture", InputDigest: "input-fixture"}
	for _, test := range []struct {
		name, want string
		items      []string
		next       int
	}{
		{name: "empty", items: []string{}, want: `{"items":"[]","count":0}`},
		{name: "nil preserved", want: `{"items":"null","count":0}`},
		{name: "unicode and next", items: []string{"one", "два"}, next: 3, want: `{"items":"[\"one\",\"два\"]","count":2,"next_cursor":3}`},
		{name: "escaping", items: []string{"<tag>", "a\nb"}, want: `{"items":"[\"\\u003ctag\\u003e\",\"a\\nb\"]","count":2}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := catalogPageProjection(request, 2, test.items, test.next)
			if err != nil {
				t.Fatal(err)
			}
			digest := sha256.Sum256([]byte(test.want))
			want := Result{Summary: test.want, Receipt: Receipt{
				EffectKey: request.EffectKey, InputDigest: request.InputDigest,
				ProviderEffectRef: "provider.list:2", ResponseDigest: hex.EncodeToString(digest[:]),
			}}
			if got != want {
				t.Fatalf("page projection = %#v, want %#v", got, want)
			}
		})
	}
	t.Run("typed elements", func(t *testing.T) {
		got, err := catalogPageProjection(request, 2, []struct {
			Name string `json:"name"`
		}{{Name: "typed"}}, 0)
		if err != nil || got.Summary != `{"items":"[{\"name\":\"typed\"}]","count":1}` || got.Receipt.ProviderEffectRef != "provider.list:2" {
			t.Fatal("typed page projection changed")
		}
	})
}

// TestCatalogPageProjectionRejectsUnencodableItems requires the previous safe invalid-response error and no partial result on JSON failure.
// TestCatalogPageProjectionRejectsUnencodableItems требует прежнюю безопасную ошибку неверного ответа и отсутствие частичного результата при сбое JSON.
func TestCatalogPageProjectionRejectsUnencodableItems(t *testing.T) {
	t.Parallel()
	result, err := catalogPageProjection(Request{Operation: "provider.list"}, 1, []chan int{nil}, 0)
	var safe *SafeError
	if result != (Result{}) || !errors.As(err, &safe) || safe.Code != "INTEGRATION_RESPONSE_INVALID" || err.Error() != "INTEGRATION_RESPONSE_INVALID" {
		t.Fatal("unencodable items exposed partial state or a private error")
	}
}
