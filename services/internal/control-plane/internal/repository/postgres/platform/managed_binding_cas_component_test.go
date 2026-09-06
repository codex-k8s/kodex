package platform

import (
	"context"
	_ "embed"
	"errors"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

//go:embed testdata/sql/managed_binding_cas_read.sql
var queryManagedBindingCASRead string

func testManagedBindingCAS(t *testing.T, ctx context.Context, repository *Repository) {
	t.Helper()
	owner := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{
		ExternalActorID: "20000000-0000-4000-8000-000000000001", ExternalTenantID: "20000000-0000-4000-8000-000000000002",
		CallerWorkload: "control-api-gateway", Operation: "platform.command.projects.create",
	}, "control-api-gateway")
	service, err := platformservice.New(repository)
	if err != nil {
		t.Fatal(err)
	}
	publish := func(name, project, content string, kinds ...command.Kind) command.Result {
		t.Helper()
		format := "JSON"
		if kinds[0] == command.CreatePromptTemplateDraft {
			format = "TEXT"
		}
		created, err := service.Execute(ctx, command.Command{Kind: kinds[0], Principal: owner, Mutation: value.Mutation{IdempotencyKey: name + "-create"}, Payload: command.ManagedConfigurationInput{Name: name, ProjectRef: project, ContentFormat: format, Content: content}})
		if err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
		for index, kind := range kinds[1:] {
			version := created.ManagedConfiguration.Version
			input := command.Command{Kind: kind, Principal: owner, Mutation: value.Mutation{IdempotencyKey: name + string(rune('a'+index)), ExpectedVersion: &version}, Payload: command.ManagedConfigurationInput{ConfigurationRef: created.ManagedConfiguration.Ref, RevisionRef: created.ManagedRevision.Ref}}
			if kind == command.PublishPromptTemplateDraft {
				created, err = executePromptPublicationFixture(t, ctx, service, input)
			} else {
				created, err = service.Execute(ctx, input)
			}
			if err != nil {
				t.Fatalf("transition %s/%s: %v", name, kind, err)
			}
		}
		return created
	}
	impact := func(set command.Result) entity.ManagedConfigurationImpact {
		t.Helper()
		result, err := service.GetManagedConfigurationImpact(ctx, owner, set.ManagedConfiguration.Ref, set.ManagedRevision.Ref, query.Filter{})
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	request := func(key string, kind command.Kind, set command.Result, preview entity.ManagedConfigurationImpact, consumers ...entity.ManagedConfigurationConsumer) command.Command {
		version := set.ManagedConfiguration.Version
		return command.Command{Kind: kind, Principal: owner, Mutation: value.Mutation{IdempotencyKey: key, ExpectedVersion: &version}, Payload: command.ManagedConfigurationInput{ConfigurationRef: set.ManagedConfiguration.Ref, RevisionRef: set.ManagedRevision.Ref, ImpactDigest: preview.Digest, Consumers: consumers}}
	}
	sttContent := `{"name":"CAS STT","stt":{"enabled":false,"providerAccountRef":"pacc_fixture","model":"whisper-1","language":"ru","permissionKey":"platform.stt.use"}}`
	sttKinds := []command.Kind{command.CreateSystemSTTDraft, command.ValidateSystemSTTDraft, command.PublishSystemSTTDraft}
	a := publish("CAS STT A", "", sttContent, sttKinds...)
	b := publish("CAS STT B", "", sttContent, sttKinds...)
	beforeA, beforeB := impact(a), impact(b)
	initial := entity.ManagedConfigurationConsumer{Kind: "STT_SERVICE", Ref: "stt-tts-service", ExpectedAbsent: true}
	resolved, err := repository.ResolvePrincipal(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	scope, err := repository.resolveScope(ctx, resolved)
	if err != nil {
		t.Fatal(err)
	}
	err = repository.pool.QueryRow(ctx, queryManagedBindingCASRead, scope.organizationID).Scan(&initial.RevisionRef, &initial.Version)
	if err == nil {
		initial.ExpectedAbsent = false
	} else if !errors.Is(err, pgx.ErrNoRows) {
		t.Fatal(err)
	}
	first := request("cas-stt-first", command.RebindSystemSTT, a, beforeA, initial)
	bound, err := service.Execute(ctx, first)
	if err != nil {
		t.Fatal(err)
	}
	a = bound
	if _, err = service.Execute(ctx, request("cas-stt-stale", command.RebindSystemSTT, b, beforeB, initial)); !errors.Is(err, errs.ErrVersionMismatch) {
		t.Fatalf("cross-set stale accepted: %v", err)
	}
	freshB := impact(b)
	if freshB.Digest == beforeB.Digest {
		t.Fatal("global binding omitted from impact commitment")
	}
	if _, err = service.Execute(ctx, request("cas-stt-stale-absence", command.RebindSystemSTT, b, freshB, entity.ManagedConfigurationConsumer{Kind: "STT_SERVICE", Ref: "stt-tts-service", ExpectedAbsent: true})); !errors.Is(err, errs.ErrVersionMismatch) {
		t.Fatalf("stale absence accepted: %v", err)
	}
	pins := impact(a).Consumers[0]
	wrong := pins
	wrong.Version++
	if _, err = service.Execute(ctx, request("cas-stt-wrong-pin", command.RebindSystemSTT, b, freshB, wrong)); !errors.Is(err, errs.ErrVersionMismatch) {
		t.Fatalf("wrong version accepted: %v", err)
	}
	wrong = pins
	wrong.RevisionRef = b.ManagedRevision.Ref
	if _, err = service.Execute(ctx, request("cas-stt-wrong-revision", command.RebindSystemSTT, b, freshB, wrong)); !errors.Is(err, errs.ErrVersionMismatch) {
		t.Fatalf("wrong revision accepted: %v", err)
	}
	bound, err = service.Execute(ctx, request("cas-stt-move", command.RebindSystemSTT, b, freshB, pins))
	if err != nil {
		t.Fatal(err)
	}
	b = bound
	if _, err = service.Execute(ctx, first); err != nil {
		t.Fatalf("exact receipt replay failed: %v", err)
	}
	if len(impact(b).Consumers) != 1 || len(impact(a).Consumers) != 0 {
		t.Fatal("receipt replay changed current binding")
	}
	foreign := first
	foreign.Principal.AuthorityTenant = "foreign-tenant"
	if _, err = service.Execute(ctx, foreign); err == nil {
		t.Fatal("foreign tenant replay accepted")
	}
	// Два разных target set не разделяют set row lock. Только один exact CAS выигрывает.
	c := publish("CAS STT C", "", sttContent, sttKinds...)
	d := publish("CAS STT D", "", sttContent, sttKinds...)
	pins = impact(b).Consumers[0]
	requests := []command.Command{request("cas-stt-race-c", command.RebindSystemSTT, c, impact(c), pins), request("cas-stt-race-d", command.RebindSystemSTT, d, impact(d), pins)}
	start := make(chan struct{})
	results := make(chan error, 2)
	for _, input := range requests {
		go func(input command.Command) { <-start; _, err := service.Execute(ctx, input); results <- err }(input)
	}
	close(start)
	success, stale := 0, 0
	for range requests {
		err := <-results
		if err == nil {
			success++
		} else if errors.Is(err, errs.ErrVersionMismatch) {
			stale++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || stale != 1 {
		t.Fatalf("CAS race outcomes: success=%d stale=%d", success, stale)
	}
	// Частичный список: первая запись вставилась, вторая stale; вся транзакция откатывается.
	project, err := service.Execute(ctx, command.Command{Kind: command.CreateProject, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "cas-project"}, Payload: command.ProjectInput{Name: "CAS project", Language: "ru"}})
	if err != nil {
		t.Fatal(err)
	}
	one := createLifecycleAgent(t, ctx, service, owner, project.Project.Ref, "cas-agent-one", "CAS one")
	two := createLifecycleAgent(t, ctx, service, owner, project.Project.Ref, "cas-agent-two", "CAS two")
	promptKinds := []command.Kind{command.CreatePromptTemplateDraft, command.ValidatePromptTemplateDraft, command.PublishPromptTemplateDraft}
	p := publish("CAS Prompt A", project.Project.Ref, `{"name":"CAS prompt","template":"Bounded instructions"}`, promptKinds...)
	q := publish("CAS Prompt B", project.Project.Ref, `{"name":"CAS prompt","template":"Other bounded instructions"}`, promptKinds...)
	foreignProject, err := service.Execute(ctx, command.Command{Kind: command.CreateProject, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "cas-foreign-project"}, Payload: command.ProjectInput{Name: "Other CAS project", Language: "ru"}})
	if err != nil {
		t.Fatal(err)
	}
	foreignAgent := createLifecycleAgent(t, ctx, service, owner, foreignProject.Project.Ref, "cas-foreign-agent", "Other project agent")
	for _, ref := range []string{foreignAgent.Ref, "agt_absent_consumer"} {
		_, err := service.Execute(ctx, request("cas-foreign-"+ref, command.RebindPromptTemplate, q, impact(q), entity.ManagedConfigurationConsumer{Kind: "AGENT", Ref: ref, ExpectedAbsent: true}))
		if !errors.Is(err, errs.ErrNotFound) {
			t.Fatalf("foreign or absent consumer accepted: %v", err)
		}
	}
	three := createLifecycleAgent(t, ctx, service, owner, project.Project.Ref, "cas-agent-three", "CAS three")
	r := publish("CAS Prompt C", project.Project.Ref, "Concurrent first binding.", promptKinds...)
	s := publish("CAS Prompt D", project.Project.Ref, "Concurrent first binding alternative.", promptKinds...)
	if _, err = service.Execute(ctx, request("cas-match-absent", command.RebindPromptTemplate, r, impact(r), entity.ManagedConfigurationConsumer{Kind: "AGENT", Ref: three.Ref, RevisionRef: r.ManagedRevision.Ref, Version: 1})); !errors.Is(err, errs.ErrVersionMismatch) {
		t.Fatalf("MATCH created an absent binding: %v", err)
	}
	absentThree := entity.ManagedConfigurationConsumer{Kind: "AGENT", Ref: three.Ref, ExpectedAbsent: true}
	requests = []command.Command{request("cas-absent-race-c", command.RebindPromptTemplate, r, impact(r), absentThree), request("cas-absent-race-d", command.RebindPromptTemplate, s, impact(s), absentThree)}
	start = make(chan struct{})
	results = make(chan error, 2)
	for _, input := range requests {
		go func(input command.Command) { <-start; _, err := service.Execute(ctx, input); results <- err }(input)
	}
	close(start)
	success, stale = 0, 0
	for range requests {
		err := <-results
		if err == nil {
			success++
		} else if errors.Is(err, errs.ErrVersionMismatch) {
			stale++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || stale != 1 {
		t.Fatalf("first binding CAS outcomes: success=%d stale=%d", success, stale)
	}
	absentOne := entity.ManagedConfigurationConsumer{Kind: "AGENT", Ref: one.Ref, ExpectedAbsent: true}
	bound, err = service.Execute(ctx, request("cas-agent-first", command.RebindPromptTemplate, p, impact(p), absentOne))
	if err != nil {
		t.Fatal(err)
	}
	p = bound
	if _, err = service.Execute(ctx, request("cas-batch-stale", command.RebindPromptTemplate, q, impact(q), entity.ManagedConfigurationConsumer{Kind: "AGENT", Ref: two.Ref, ExpectedAbsent: true}, absentOne)); !errors.Is(err, errs.ErrVersionMismatch) {
		t.Fatalf("partial batch accepted: %v", err)
	}
	if len(impact(q).Consumers) != 0 || len(impact(p).Consumers) != 1 {
		t.Fatal("partial batch leaked a binding")
	}
	if _, err = service.Execute(ctx, request("cas-batch-retry", command.RebindPromptTemplate, q, impact(q), entity.ManagedConfigurationConsumer{Kind: "AGENT", Ref: two.Ref, ExpectedAbsent: true}, impact(p).Consumers[0])); err != nil {
		t.Fatalf("batch rollback changed set OCC: %v", err)
	}
}
