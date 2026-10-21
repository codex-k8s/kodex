// Package testdata embeds the owner-approved portable speech fixture for deterministic input checks.
// Пакет testdata встраивает разрешённую владельцем переносимую речевую фикстуру для воспроизводимых проверок входа.
package testdata

import _ "embed"

// RussianNumbers contains the embedded recording bytes; importing it does not initiate a provider request.
// RussianNumbers содержит байты встроенной записи; импорт не инициирует запрос к провайдеру.
//
//go:embed 1-2-3-4-5.mp3
var RussianNumbers []byte
