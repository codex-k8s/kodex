package prompt

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

var assistantCoreRefPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{8,96}$`)

// База не является пользовательским шаблоном и не может назначать slots или
// capabilities. Она материализуется ровно один раз из того же canonical data,
// что и owner overlay; литеральный Go template в инструкции не выполняется снова.
func renderAssistantCore(snapshot Snapshot, data map[string]any) (string, error) {
	core := snapshot.AssistantCore
	if core == nil {
		return "", nil
	}
	if snapshot.ServiceTemplateRevision != "prompt-service-v3" || !assistantCoreRefPattern.MatchString(core.Ref) || !validDigest(core.Digest) ||
		!utf8.ValidString(core.Content) || strings.ContainsRune(core.Content, 0) ||
		(core.Scope != "PROJECT" && core.Scope != "SYSTEM") ||
		(core.Scope == "PROJECT" && snapshot.ProjectRef == "") || (core.Scope == "SYSTEM" && snapshot.ProjectRef != "") {
		return "", ErrInvalid
	}
	revision := strings.TrimPrefix(core.Revision, "system-assistant-core-v")
	number, err := strconv.ParseInt(revision, 10, 32)
	if err != nil || number < 1 || strconv.FormatInt(number, 10) != revision || revision == core.Revision {
		return "", ErrInvalid
	}
	digest := sha256.Sum256([]byte(core.Content))
	if hex.EncodeToString(digest[:]) != core.Digest || len(Validate(core.Content, Catalog())) != 0 {
		return "", ErrInvalid
	}
	parsed, err := parseTemplate(core.Content)
	if err != nil {
		return "", ErrInvalid
	}
	if slots, valid := templateSlots(parsed.Tree.Root); !valid || len(slots) != 0 {
		return "", ErrInvalid
	}
	rendered, err := executeTemplate(parsed, data)
	if err != nil {
		return "", ErrInvalid
	}
	if core.Scope == "SYSTEM" {
		// У SYSTEM та же база уже является основным published шаблоном.
		return "", nil
	}
	return fmt.Sprintf("Platform assistant base (scope=%s, ref=%s, revision=%s, digest=%s):\n%s",
		core.Scope, core.Ref, core.Revision, core.Digest, rendered), nil
}
