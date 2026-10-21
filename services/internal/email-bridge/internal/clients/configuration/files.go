// Package configuration pins one projected Secret generation before loading mailbox configuration and private credential bytes.
// Пакет configuration фиксирует одно поколение проекции Secret до чтения конфигурации ящиков и закрытых реквизитов.
package configuration

import (
	"bytes"
	"context"
	"path/filepath"
	"strconv"
	"strings"

	api "github.com/codex-k8s/kodex/libs/go/emailbridgeapi"
	"github.com/codex-k8s/kodex/libs/go/securefile"
	"github.com/codex-k8s/kodex/services/internal/email-bridge/internal/domain/errs"
)

// maximumBytes bounds the combined configuration, credential names and credential contents in a loaded generation.
// maximumBytes ограничивает суммарный объём конфигурации, имён и содержимого реквизитов загружаемого поколения.
const maximumBytes = 900 << 10

// Snapshot owns a parsed configuration and private descriptor-keyed credential copies from the same pinned generation.
// Snapshot владеет разобранной конфигурацией и закрытыми копиями реквизитов по descriptor из одного фиксированного поколения.
type Snapshot struct {
	Configuration api.Configuration
	credentials   map[api.Descriptor][]byte
}

// Read rejects cancellation, invalid or missing descriptors and returns a fresh byte copy so callers cannot mutate stored credentials.
// Read отвергает отмену, неверные или отсутствующие descriptor и возвращает копию байтов, не позволяя менять сохранённые реквизиты.
func (s *Snapshot) Read(ctx context.Context, descriptor api.Descriptor) ([]byte, error) {
	if ctx.Err() != nil || s == nil || !api.DescriptorValid(descriptor) || len(s.credentials[descriptor]) == 0 {
		return nil, errs.Unavailable
	}
	return bytes.Clone(s.credentials[descriptor]), nil
}

// Load resolves ..data once, confines all secure reads to that direct generation and loads credentials only for enabled mailboxes.
// Rotation never intentionally mixes generations; disappearance, invalid configuration or budget/cancellation failure rejects the snapshot.
// Load один раз разрешает ..data, ограничивает безопасное чтение этим прямым поколением и читает реквизиты только включённых ящиков.
// Поколения при ротации не смешиваются; исчезновение, неверная конфигурация, превышение бюджета или отмена отклоняют снимок.
func Load(ctx context.Context, root string) (*Snapshot, error) {
	if ctx.Err() != nil || !filepath.IsAbs(root) || filepath.Clean(root) != root {
		return nil, errs.Unavailable
	}
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, errs.Unavailable
	}
	generation, err := filepath.EvalSymlinks(filepath.Join(resolvedRoot, "..data"))
	if err != nil || filepath.Dir(generation) != resolvedRoot || !strings.HasPrefix(filepath.Base(generation), "..") {
		return nil, errs.Unavailable
	}
	raw, err := securefile.ReadWithin(generation, filepath.Join(generation, "mailboxes.json"), maximumBytes)
	if err != nil {
		return nil, errs.Unavailable
	}
	snapshot := &Snapshot{credentials: make(map[api.Descriptor][]byte)}
	if api.Decode(raw, &snapshot.Configuration) != nil || api.ValidateConfiguration(snapshot.Configuration) != nil {
		return nil, errs.Invalid
	}
	size := len(raw)
	for _, mailbox := range snapshot.Configuration.Mailboxes {
		if !mailbox.Enabled {
			continue
		}
		for _, endpoint := range []*api.Endpoint{&mailbox.Smtp, mailbox.Imap, mailbox.Pop} {
			if endpoint == nil {
				continue
			}
			for _, descriptor := range []api.Descriptor{endpoint.Ca, endpoint.Username, endpoint.Secret} {
				if _, exists := snapshot.credentials[descriptor]; exists {
					continue
				}
				if ctx.Err() != nil || !api.DescriptorValid(descriptor) {
					return nil, errs.Unavailable
				}
				key := descriptor.Name + "." + strconv.FormatInt(descriptor.Generation, 10)
				value, err := securefile.ReadWithin(generation, filepath.Join(generation, key), int64(maximumBytes-size))
				if err != nil || len(key)+len(value) > maximumBytes-size {
					return nil, errs.Unavailable
				}
				size += len(key) + len(value)
				snapshot.credentials[descriptor] = value
			}
		}
	}
	if ctx.Err() != nil {
		return nil, errs.Unavailable
	}
	return snapshot, nil
}
