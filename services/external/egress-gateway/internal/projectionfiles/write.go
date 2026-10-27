// Package projectionfiles writes the private artifacts of the gateway projection commands.
// Package projectionfiles записывает закрытые артефакты команд проекции gateway.
package projectionfiles

import (
	"errors"
	"os"
	"path/filepath"
)

// Write creates a new 0700 output directory and writes the caller-rendered artifacts with 0600
// permissions before umask. Filenames and bytes belong to the validated producer. Native map
// iteration and partial output on failure are preserved; diagnostics keep the original closed
// messages. The caller owns cleanup and the directory must not already exist.
// Write создаёт новый output directory с 0700 и записывает отрендеренные caller артефакты с 0600
// до применения umask. Имена и bytes принадлежат проверенному producer. Сохраняет исходный обход
// map и частичный output при отказе, возвращает прежние закрытые diagnostics. Очисткой владеет
// caller; output directory не должен существовать до вызова.
func Write(directory string, files map[string][]byte) error {
	if err := os.Mkdir(directory, 0700); err != nil {
		return errors.New("create projection output directory")
	}
	for name, value := range files {
		if err := os.WriteFile(filepath.Join(directory, name), value, 0600); err != nil {
			return errors.New("write projection artifact")
		}
	}
	return nil
}
