package mailtransport

import (
	"context"
	"slices"
	"strconv"

	api "github.com/codex-k8s/kodex/libs/go/emailbridgeapi"
	"github.com/codex-k8s/kodex/services/internal/email-bridge/internal/domain/errs"
	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
)

// Apply вызывается только после durable unknown receipt и точной проверки scopes.
func (p *Provider) Apply(ctx context.Context, m api.Mailbox, cmd api.Command, id string) (api.Result, error) {
	r := api.Result{Status: "unknown", Folder: cmd.Folder, Uid: cmd.Uid, UidValidity: cmd.UidValidity}
	c, done, err := p.imap(ctx, m)
	if err != nil {
		return r, err
	}
	defer done()
	s, err := selectIMAP(c, cmd.Folder, cmd.UidValidity, false)
	if err != nil {
		r.Status = "failed"
		return r, nil
	}
	r.UidValidity = s.UIDValidity
	if cmd.Operation == api.OperationDraftCreate || cmd.Operation == api.OperationDraftUpdate || cmd.Operation == api.OperationDraftDelete || cmd.Operation == api.OperationDelete || cmd.Operation == api.OperationMove || cmd.Operation == api.OperationArchive {
		if !c.Caps().Has(imap.CapUIDPlus) && !c.Caps().Has(imap.CapIMAP4rev2) {
			r.Status = "failed"
			return r, nil
		}
	}
	var uid imap.UID
	if cmd.Operation != api.OperationDraftCreate {
		uid, err = parseUID(cmd.Uid)
		if err != nil {
			r.Status = "failed"
			return r, nil
		}
		items, err := c.Fetch(imap.UIDSetNum(uid), &imap.FetchOptions{UID: true, Flags: true}).Collect()
		if err != nil {
			return r, errs.Unavailable
		}
		if len(items) != 1 || items[0].UID != uid {
			r.Status = "failed"
			return r, nil
		}
		if cmd.Operation == api.OperationDraftUpdate || cmd.Operation == api.OperationDraftDelete {
			if !slices.Contains(items[0].Flags, imap.FlagDraft) {
				r.Status = "failed"
				return r, nil
			}
		}
		if cmd.Operation == api.OperationDraftUpdate {
			raw, _, err := imapRaw(c, m, uid)
			if err != nil {
				return r, err
			}
			if api.Digest(raw) != cmd.ExpectedDigest {
				r.Status = "failed"
				return r, nil
			}
		}
	}
	set := imap.UIDSetNum(uid)
	switch cmd.Operation {
	case api.OperationMarkRead, api.OperationMarkUnread:
		op := imap.StoreFlagsAdd
		if cmd.Operation == api.OperationMarkUnread {
			op = imap.StoreFlagsDel
		}
		if err := c.Store(set, &imap.StoreFlags{Op: op, Flags: []imap.Flag{imap.FlagSeen}, Silent: true}, nil).Close(); err != nil {
			return r, errs.Unavailable
		}
	case api.OperationDelete, api.OperationDraftDelete:
		if err := imapDelete(c, set); err != nil {
			return r, err
		}
		r.Status = "deleted"
		return r, nil
	case api.OperationMove, api.OperationArchive:
		// COPY подтверждается до STORE/EXPUNGE; библиотечный MOVE fallback это не гарантирует.
		if c.Caps().Has(imap.CapMove) {
			data, err := c.Move(set, cmd.DestinationFolder).Wait()
			if err != nil {
				return r, errs.Unavailable
			}
			if err = setDestination(&r, data.DestUIDs, data.UIDValidity, cmd.DestinationFolder); err != nil {
				return r, err
			}
		} else {
			data, err := c.Copy(set, cmd.DestinationFolder).Wait()
			if err != nil {
				return r, errs.Unavailable
			}
			if err = setDestination(&r, data.DestUIDs, data.UIDValidity, cmd.DestinationFolder); err != nil {
				return r, err
			}
			if err = imapDelete(c, set); err != nil {
				return r, err
			}
		}
	case api.OperationDraftCreate, api.OperationDraftUpdate:
		raw, err := compose(m, cmd.Message, id, "")
		if err != nil {
			r.Status = "failed"
			return r, nil
		}
		appendCmd := c.Append(cmd.Folder, int64(len(raw)), &imap.AppendOptions{Flags: []imap.Flag{imap.FlagDraft}})
		if _, err = appendCmd.Write(raw); err != nil {
			return r, errs.Unavailable
		}
		if err = appendCmd.Close(); err != nil {
			return r, errs.Unavailable
		}
		data, err := appendCmd.Wait()
		if err != nil {
			return r, errs.Unavailable
		}
		if data.UID == 0 || data.UIDValidity != s.UIDValidity {
			return r, errs.Unavailable
		}
		r.Uid = strconv.FormatUint(uint64(data.UID), 10)
		r.ContentDigest = api.Digest(raw)
		if cmd.Operation == api.OperationDraftUpdate {
			if err = imapDelete(c, set); err != nil {
				return r, err
			}
		}
	default:
		return r, errs.Unsupported
	}
	r.Status = "accepted"
	return r, nil
}

func imapDelete(c *imapclient.Client, set imap.UIDSet) error {
	if err := c.Store(set, &imap.StoreFlags{Op: imap.StoreFlagsAdd, Flags: []imap.Flag{imap.FlagDeleted}, Silent: true}, nil).Close(); err != nil {
		return errs.Unavailable
	}
	// Никогда не EXPUNGE без UID: чужие помеченные сообщения не затрагиваются.
	if err := c.UIDExpunge(set).Close(); err != nil {
		return errs.Unavailable
	}
	return nil
}
func setDestination(r *api.Result, numbers imap.NumSet, validity uint32, folder string) error {
	set, ok := numbers.(imap.UIDSet)
	if !ok {
		return errs.Unavailable
	}
	if validity == 0 || len(set) != 1 || set[0].Start == 0 || set[0].Start != set[0].Stop {
		return errs.Unavailable
	}
	r.Uid = strconv.FormatUint(uint64(set[0].Start), 10)
	r.UidValidity = validity
	r.Folder = folder
	return nil
}
