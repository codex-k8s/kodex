package prompt

import "strconv"

// MaterializeWarm quotes combined core and owner text as one literal template action for idle
// runtime, preserving mandatory platform sections without granting turn authority or executing
// embedded actions.
// MaterializeWarm заключает объединённый текст базы и владельца в одно литеральное действие
// шаблона idle runtime, сохраняя обязательные секции платформы без выдачи turn-полномочий либо
// исполнения вложенных actions.
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
