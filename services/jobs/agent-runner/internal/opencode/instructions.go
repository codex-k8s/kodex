package opencode

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// InstallCurrentInstructions writes one bounded read-only current owner materialization outside
// the workspace and native export. The caller supplies the fresh private provider HOME and adds
// the returned absolute path to config.instructions; user tasks never enter this channel.
// InstallCurrentInstructions пишет ограниченную read-only материализацию текущего owner вне
// workspace и native export. Caller передаёт свежий private provider HOME и добавляет абсолютный
// путь в config.instructions; пользовательские задачи в этот канал не попадают.
func InstallCurrentInstructions(home, instructions string) (string, error) {
	info, err := os.Lstat(home)
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0o700 || !filepath.IsAbs(home) || filepath.Clean(home) != home ||
		len(instructions) > 1<<20 || !utf8.ValidString(instructions) || strings.ContainsRune(instructions, 0) {
		return "", ErrProtocol
	}
	path := filepath.Join(home, "current-instructions.md")
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o400)
	if err != nil {
		return "", ErrProtocol
	}
	if file.Chmod(0o400) != nil {
		file.Close()
		return "", ErrProtocol
	}
	_, writeErr := io.WriteString(file, instructions)
	syncErr, closeErr := file.Sync(), file.Close()
	if writeErr != nil || syncErr != nil || closeErr != nil {
		return "", ErrProtocol
	}
	return path, nil
}
