package opencode

import (
	"encoding/json"
	"io"
	"os"
	"strings"
)

// checkpointExcludesCredentials rejects a staged export containing a known process credential, including JSON-escaped text.
// It never rewrites native history; the caller retains private upstream state for reconciliation and withholds publication.
// checkpointExcludesCredentials отклоняет staged export с известным process credential, включая JSON-escaped text.
// Native history не переписывается: caller сохраняет private upstream state для сверки и запрещает публикацию.
func checkpointExcludesCredentials(file *os.File, credentials []string) error {
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return ErrCheckpoint
	}
	decoder := json.NewDecoder(io.LimitReader(file, MaximumCheckpointBytes+1))
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return ErrCheckpoint
		}
		text, ok := token.(string)
		if !ok {
			continue
		}
		for _, credential := range credentials {
			if len(credential) >= 8 && strings.Contains(text, credential) {
				return ErrCheckpoint
			}
		}
	}
}
