package platform

import (
	"reflect"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

// TestEnvironmentValuesKeepWireAndValidationContract checks read projection and the real payload
// validator against literal ordered wire results and failures. Nil/empty inputs remain allocated
// runtime slices and [] JSON; reads preserve even invalid data for later validation, while writes
// reject duplicate names, invalid names and NUL values without partial output. Copies stay independent.
// TestEnvironmentValuesKeepWireAndValidationContract проверяет проекцию чтения и настоящий
// валидатор входа по буквальным упорядоченным JSON и ошибкам. Nil и пустой вход дают созданные
// runtime-срезы и JSON []; чтение сохраняет даже неверные данные для последующей проверки,
// запись отклоняет повторные или неверные имена и NUL без частичного результата. Копии независимы.
func TestEnvironmentValuesKeepWireAndValidationContract(t *testing.T) {
	for _, test := range []struct {
		name          string
		values        []entity.RuntimeEnvironmentValue
		want          []runtimecontract.RuntimeEnvironmentValue
		wire, failure string
	}{
		{name: "nil", want: []runtimecontract.RuntimeEnvironmentValue{}, wire: `[]`},
		{name: "empty", values: []entity.RuntimeEnvironmentValue{}, want: []runtimecontract.RuntimeEnvironmentValue{}, wire: `[]`},
		{name: "ordered", values: []entity.RuntimeEnvironmentValue{{Name: "ZONE", Value: "public-zone"}, {Name: "MODE", Value: "safe"}}, want: []runtimecontract.RuntimeEnvironmentValue{{Name: "ZONE", Value: "public-zone"}, {Name: "MODE", Value: "safe"}}, wire: `[{"name":"ZONE","value":"public-zone"},{"name":"MODE","value":"safe"}]`},
		{name: "duplicate", values: []entity.RuntimeEnvironmentValue{{Name: "MODE", Value: "one"}, {Name: "MODE", Value: "two"}}, want: []runtimecontract.RuntimeEnvironmentValue{{Name: "MODE", Value: "one"}, {Name: "MODE", Value: "two"}}, failure: "runtime environment name is duplicated"},
		{name: "invalid-name", values: []entity.RuntimeEnvironmentValue{{Name: "bad-name", Value: "safe"}}, want: []runtimecontract.RuntimeEnvironmentValue{{Name: "bad-name", Value: "safe"}}, failure: "runtime environment value is invalid"},
		{name: "nul-value", values: []entity.RuntimeEnvironmentValue{{Name: "MODE", Value: "a\x00b"}}, want: []runtimecontract.RuntimeEnvironmentValue{{Name: "MODE", Value: "a\x00b"}}, failure: "runtime environment value is invalid"},
	} {
		t.Run(test.name, func(t *testing.T) {
			readValues, readSecrets := runtimeEnvironmentContract(entity.RuntimeEnvironmentVersion{Values: test.values})
			if !reflect.DeepEqual(readValues, test.want) || readSecrets == nil || len(readSecrets) != 0 {
				t.Fatal("environment read changed ordering, validation or empty shape")
			}
			rawValues, rawSecrets, writtenValues, writtenSecrets, err := validateEnvironmentPayload(test.values, nil)
			if test.failure != "" {
				if err == nil || err.Error() != test.failure || rawValues != nil || rawSecrets != nil || writtenValues != nil || writtenSecrets != nil {
					t.Fatal("environment write changed rejection or returned partial output")
				}
			} else if err != nil || string(rawValues) != test.wire || string(rawSecrets) != `[]` || !reflect.DeepEqual(writtenValues, test.want) || writtenSecrets == nil || len(writtenSecrets) != 0 {
				t.Fatal("environment write changed wire, ordering or empty shape")
			}
			if len(test.values) > 0 {
				prior := test.values[0].Value
				test.values[0].Value = "changed-after-projection"
				if readValues[0].Value != prior || err == nil && writtenValues[0].Value != prior {
					t.Fatal("environment projection aliases caller slice")
				}
			}
		})
	}
}

// TestAssistantPublicValueNameGuardMatchesInsertionAndRemoval checks fixed ordinary/sensitive
// names through both public-value parsing and sparse removal, plus update/removal conflict.
// All values are synthetic public text; the guard must preserve the existing substring rules and
// snapshot while returning no partial result for forbidden names. It does not inspect credentials.
// TestAssistantPublicValueNameGuardMatchesInsertionAndRemoval проверяет фиксированные обычные и
// чувствительные имена через разбор публичных значений и частичное удаление, а также конфликт
// обновления с удалением. Все значения синтетические и публичные; прежние правила фрагментов и
// снимок сохраняются, запрещённые имена не дают частичного результата. Credential не читаются.
func TestAssistantPublicValueNameGuardMatchesInsertionAndRemoval(t *testing.T) {
	snapshot := entity.RuntimeEnvironmentDraftSpecification{Values: []entity.RuntimeEnvironmentValue{{Name: "MODE", Value: "safe"}}}
	for _, test := range []struct {
		name    string
		allowed bool
	}{
		{name: "MODE", allowed: true},
		{name: "MONKEY", allowed: true},
		{name: "X_SECRET_X"},
		{name: "PASSWORD_HINT"},
		{name: "TOKEN_COUNT"},
		{name: "CREDENTIAL_LABEL"},
		{name: "PRIVATE_KEY_PATH"},
		{name: "API_KEY_NAME"},
	} {
		t.Run(test.name, func(t *testing.T) {
			values, valid := assistantEnvironmentPublicValues(map[string]any{"publicValues": []any{map[string]any{"name": test.name, "value": "public-example"}}})
			removed, removedValid := assistantEnvironmentPatchedPublicValues(snapshot, map[string]any{"publicValueRemovals": []any{test.name}})
			if valid != test.allowed || removedValid != test.allowed {
				t.Fatal("public-value insertion/removal name rules diverged")
			}
			if !test.allowed {
				if values != nil || removed != nil {
					t.Fatal("forbidden public name returned partial values")
				}
				return
			}
			wantRemoval := []entity.RuntimeEnvironmentValue{{Name: "MODE", Value: "safe"}}
			if test.name == "MODE" {
				wantRemoval = []entity.RuntimeEnvironmentValue{}
			}
			if !reflect.DeepEqual(values, []entity.RuntimeEnvironmentValue{{Name: test.name, Value: "public-example"}}) || !reflect.DeepEqual(removed, wantRemoval) {
				t.Fatal("public-value insertion or removal changed result shape")
			}
		})
	}
	if _, valid := assistantEnvironmentPatchedPublicValues(snapshot, map[string]any{"publicValueUpdates": []any{map[string]any{"name": "MODE", "value": "public-update"}}, "publicValueRemovals": []any{"MODE"}}); valid {
		t.Fatal("conflicting public-value update/removal accepted")
	}
	if !reflect.DeepEqual(snapshot.Values, []entity.RuntimeEnvironmentValue{{Name: "MODE", Value: "safe"}}) {
		t.Fatal("public-value patch mutated snapshot")
	}
}
