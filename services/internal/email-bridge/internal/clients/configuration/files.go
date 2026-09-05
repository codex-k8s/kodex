// Package configuration читает один immutable snapshot почтового Secret.
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

const maximumBytes = 900 << 10

type Snapshot struct {
	Configuration api.Configuration
	credentials   map[api.Descriptor][]byte
}

func (s *Snapshot) Read(ctx context.Context, descriptor api.Descriptor) ([]byte, error) {
	if ctx.Err() != nil || s == nil || !api.DescriptorValid(descriptor) || len(s.credentials[descriptor]) == 0 {
		return nil, errs.Unavailable
	}
	return bytes.Clone(s.credentials[descriptor]), nil
}

// Load фиксирует ..data до чтения: смена symlink не смешивает два поколения.
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
