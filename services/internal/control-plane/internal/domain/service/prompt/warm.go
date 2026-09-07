package prompt

import "strconv"

// MaterializeWarm не выдаёт idle runtime полномочий turn. Core и owner text
// остаются текстом: literal action не позволяет им выдать себя за slot/template.
func MaterializeWarm(core, owner, templateRef, templateDigest, agentRef, sessionRef string) (Materialization, error) {
	text := core
	if owner != "" {
		text += "\n\n" + owner
	}
	return Materialize("{{"+strconv.Quote(text)+"}}", Snapshot{
		ServiceTemplateRevision: ServiceTemplateRevision, Locale: "en", TargetKind: TargetAgent,
		TargetRef: agentRef, SessionRef: sessionRef, TemplateRef: templateRef, TemplateDigest: templateDigest,
	})
}
