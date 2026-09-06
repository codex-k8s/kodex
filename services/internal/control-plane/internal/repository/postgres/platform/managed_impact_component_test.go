package platform

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sort"
	"strconv"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

func testManagedImpactPagination(t *testing.T, ctx context.Context, service *platformservice.Service, owner value.Principal, projectRef string, configuration command.Result) {
	t.Helper()
	ref, revision := configuration.ManagedConfiguration.Ref, configuration.ManagedRevision.Ref
	read := func(filter query.Filter) entity.ManagedConfigurationImpact {
		t.Helper()
		result, err := service.GetManagedConfigurationImpact(ctx, owner, ref, revision, filter)
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	before := read(query.Filter{})
	agent := createLifecycleAgent(t, ctx, service, owner, projectRef, "managed-impact-second-agent", "Second impact consumer")
	current, _, _, _, err := service.ListManagedConfigurationHistory(ctx, owner, ref, query.Page{Size: 1})
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.Execute(ctx, command.Command{Kind: command.RebindPromptTemplate, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "managed-impact-second-bind", ExpectedVersion: &current.Version},
		Payload: command.ManagedConfigurationInput{ConfigurationRef: ref, RevisionRef: revision, ImpactDigest: before.Digest,
			Consumers: []entity.ManagedConfigurationConsumer{{Kind: "AGENT", Ref: agent.Ref, ExpectedAbsent: true}}}})
	if err != nil {
		t.Fatal(err)
	}
	first := read(query.Filter{Page: query.Page{Size: 1}})
	if first.Total != 2 || len(first.Consumers) != 1 || first.NextPageToken == "" {
		t.Fatal("missing bounded managed impact first page")
	}
	last := read(query.Filter{Page: query.Page{Size: 1, Token: first.NextPageToken}})
	if last.Total != 2 || len(last.Consumers) != 1 || last.NextPageToken != "" || last.Consumers[0].Ref == first.Consumers[0].Ref || last.Digest != first.Digest {
		t.Fatal("managed impact cursor mismatch")
	}
	for _, filter := range []query.Filter{{Query: "changed", Page: query.Page{Size: 1, Token: first.NextPageToken}}} {
		if _, err := service.GetManagedConfigurationImpact(ctx, owner, ref, revision, filter); !errors.Is(err, errs.ErrInvalid) {
			t.Fatalf("managed impact cursor crossed query: %v", err)
		}
	}
	found := read(query.Filter{Query: agent.Ref, Page: query.Page{Size: 1}})
	missing := read(query.Filter{Query: "no-such-consumer", Page: query.Page{Size: 1}})
	if found.Total != 1 || len(found.Consumers) != 1 || found.Consumers[0].Ref != agent.Ref || missing.Total != 0 || len(missing.Consumers) != 0 || found.Digest != first.Digest || missing.Digest != first.Digest {
		t.Fatal("managed impact search changed full commitment")
	}
	all := append(first.Consumers, last.Consumers...)
	sort.Slice(all, func(i, j int) bool { return all[i].Kind+"\x00"+all[i].Ref < all[j].Kind+"\x00"+all[j].Ref })
	digest := sha256.New()
	_, _ = digest.Write([]byte(ref + "\x00" + revision))
	for _, item := range all {
		_, _ = digest.Write([]byte("\x00" + item.Kind + "\x00" + item.Ref + "\x00" + item.RevisionRef + "\x00" + strconv.FormatInt(item.Version, 10)))
	}
	if hex.EncodeToString(digest.Sum(nil)) != first.Digest {
		t.Fatal("SQL commitment differs from original byte-exact digest")
	}
}
