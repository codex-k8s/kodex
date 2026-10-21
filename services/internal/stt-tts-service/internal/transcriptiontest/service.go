// Package transcriptiontest supplies stateless service doubles shared by STT composition and transport tests, never production execution.
// RU: Пакет transcriptiontest задаёт общие stateless-заглушки сервиса для тестов композиции и транспорта STT, а не рабочего исполнения.
package transcriptiontest

import (
	"context"
	"errors"

	"github.com/codex-k8s/kodex/libs/go/sttapi/modelprofile"
	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/domain/service/transcription"
	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/domain/types/value"
)

// Service preserves the minimal fake service contract: a local catalog, unavailable provider, pending protected path and no real transcription.
// RU: Service сохраняет минимальный контракт заглушки: локальный каталог, недоступный провайдер, ожидающий защищённый путь и отсутствие реального распознавания.
type Service struct{}

// Catalog returns the shared static model catalog without provider discovery or credentials.
// RU: Catalog возвращает общий статический каталог моделей без обнаружения провайдера или реквизитов.
func (Service) Catalog() modelprofile.Catalog { return modelprofile.OpenAICatalog() }

// GetModelCatalog returns the same local fixture catalog without performing real principal authorization.
// RU: GetModelCatalog возвращает тот же локальный каталог оснастки без настоящей проверки полномочий субъекта.
func (Service) GetModelCatalog(context.Context, value.Principal) (modelprofile.Catalog, error) {
	return modelprofile.OpenAICatalog(), nil
}

// CheckAvailability reports the fixture's fixed unavailable error without reading policy, credentials or a provider.
// RU: CheckAvailability возвращает фиксированную ошибку недоступности оснастки без чтения политики, реквизитов или провайдера.
func (Service) CheckAvailability(context.Context, value.Principal, string) (transcription.Availability, error) {
	return transcription.Availability{}, errors.New("unavailable")
}

// Transcribe returns the original empty successful fixture result without consuming audio or invoking a model.
// RU: Transcribe возвращает прежний пустой успешный результат оснастки без чтения аудио или вызова модели.
func (Service) Transcribe(context.Context, transcription.Input) (value.TranscriptionResult, error) {
	return value.TranscriptionResult{}, nil
}

// CheckLocal keeps the fixture locally ready regardless of external provider availability.
// RU: CheckLocal сохраняет локальную готовность оснастки независимо от доступности внешнего провайдера.
func (Service) CheckLocal(context.Context) error { return nil }

// CheckProtectedPath reports pending delegated authority so local readiness tests cannot imply a healthy remote protected path.
// RU: CheckProtectedPath сообщает ожидание делегированных полномочий, чтобы тест локальной готовности не означал исправность удалённого защищённого пути.
func (Service) CheckProtectedPath(context.Context) error { return errors.New("pending") }
