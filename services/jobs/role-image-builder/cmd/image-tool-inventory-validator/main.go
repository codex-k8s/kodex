// The inventory validator reads bounded stdin without credentials or RPC;
// structural validation does not itself grant image admission.
// Валидатор реестра читает ограниченный stdin без реквизитов и RPC;
// проверка структуры сама по себе не разрешает допуск образа.
package main

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

// invalidInventory is the single public failure returned instead of private
// input, parser details or reader/writer diagnostics.
// invalidInventory — единый открытый отказ вместо закрытого ввода,
// подробностей разбора и диагностики чтения/записи.
var invalidInventory = errors.New("image tool inventory is invalid")

// main connects the closed CLI validator to standard streams and exits one on
// failure, emitting only the safe sentinel rather than underlying diagnostics.
// main подключает закрытый CLI-валидатор к стандартным потокам и при отказе
// завершает процесс кодом один, печатая только безопасную общую ошибку.
func main() {
	if err := validate(os.Args[1:], os.Stdin, os.Stdout); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, invalidInventory)
		os.Exit(1)
	}
}

// validate bounds stdin and delegates to strict runtime-contract decoders;
// inventory emits no data, while capture-manifest copies only validated canonical bytes.
// validate ограничивает stdin и использует строгие декодеры runtime-контракта;
// inventory ничего не выдаёт, capture-manifest копирует только проверенные канонические байты.
func validate(args []string, input io.Reader, output io.Writer) error {
	if len(args) != 1 || (args[0] != "capture-manifest" && args[0] != "inventory") {
		return invalidInventory
	}
	raw, err := io.ReadAll(io.LimitReader(input, runtimecontract.MaximumImageInventoryBytes+1))
	if err != nil {
		return invalidInventory
	}
	defer clear(raw)
	if args[0] == "inventory" {
		if _, err := runtimecontract.DecodeImageToolInventory(raw); err != nil {
			return invalidInventory
		}
		return nil
	}
	if _, err := runtimecontract.DecodeImageToolManifest(raw); err != nil {
		return invalidInventory
	}
	if count, err := output.Write(raw); err != nil || count != len(raw) {
		return invalidInventory
	}
	return nil
}
