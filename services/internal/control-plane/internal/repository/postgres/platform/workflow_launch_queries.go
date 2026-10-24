package platform

import _ "embed"

var (
	// queryWorkflowLaunchResumeStepRun resumes a SUCCEEDED non-root run in the exact organization,
	// clearing finish time and incrementing version. The caller establishes the required-workflow
	// continuation and owns surrounding graph/event updates; this SQL alone does not authorize resume.
	// queryWorkflowLaunchResumeStepRun возобновляет SUCCEEDED run, не являющийся корнем, в точной
	// организации: очищает время завершения и увеличивает версию. Причину продолжения и связанные
	// изменения графа и событий обеспечивает вызывающий; запрос сам не авторизует возобновление.
	//go:embed sql/workflow_launch__resume_step_run.sql
	queryWorkflowLaunchResumeStepRun string
	// queryWorkflowLaunchRunPending returns whether an internal run has an OPEN required launch and
	// whether the origin node has any historical launch. These two existence checks have different
	// state predicates and no organization filter; trusted caller resolution supplies the internal
	// IDs.
	// queryWorkflowLaunchRunPending возвращает наличие OPEN обязательного запуска у внутреннего run и
	// любого исторического запуска у origin node. Условия состояния различаются, фильтра организации
	// нет; внутренние ID разрешает доверенный вызывающий.
	//go:embed sql/workflow_launch__run_pending.sql
	queryWorkflowLaunchRunPending string
	// queryWorkflowLaunchClaimAuthorities walks required-launch ancestors up to depth 9 and returns
	// each root actor, organization, project, origin agent capabilities and workflow in depth order.
	// The final organization filter narrows results, but this projection does not itself reject
	// invalid or closed ancestry; claim eligibility is checked separately.
	// queryWorkflowLaunchClaimAuthorities обходит предков обязательного запуска до глубины 9,
	// возвращая корневого субъекта, организацию, проект, агента источника, его права и workflow по
	// глубине. Итоговый фильтр ограничивает организацию; недопустимость или завершение предка
	// проверяется отдельно, проекция сама их не отклоняет.
	//go:embed sql/workflow_launch__claim_authorities.sql
	queryWorkflowLaunchClaimAuthorities string
	// queryWorkflowLaunchTerminalRoots lists terminal root runs in exact organization and project IDs
	// that participate as required-launch origin or child, ordered by internal run ID. It provides
	// reconciliation candidates and does not itself close leases, claims or graph nodes.
	// queryWorkflowLaunchTerminalRoots выбирает завершённые корневые run точных организации и
	// проектов, участвующие источником или потомком обязательного запуска, в порядке внутренних ID.
	// Возвращает кандидатов сверки, самостоятельно не закрывая аренды, захваты и узлы графа.
	//go:embed sql/workflow_launch__terminal_roots.sql
	queryWorkflowLaunchTerminalRoots string
	// queryWorkflowLaunchLockProjects resolves organization projects by explicit ref or trusted
	// run/lease/gate/session locator and optionally adds projects with open launches or active
	// workflow roots. It acquires transaction advisory locks in project-ID order; resolving and
	// locking a project does not grant actor authority or start work.
	// queryWorkflowLaunchLockProjects разрешает проекты организации по ссылке либо указателю run,
	// lease, gate или session; при claim включает проекты с открытыми запусками и активными workflow
	// roots. Берёт транзакционные advisory locks в порядке ID; разрешение и блокировка не выдают
	// полномочий и не запускают работу.
	//go:embed sql/workflow_launch__lock_projects.sql
	queryWorkflowLaunchLockProjects string
	// queryWorkflowLaunchResolveOrigin resolves a live claimed lease by exact organization, ref, fence
	// hash and generation, joining consistent
	// run/root/node/session/turn/runtime-revision/project/agent/actor ownership. It requires deadline
	// and runtime states, launch capability in both revision and agent, and assistantScope NONE,
	// locking node/lease/root/actor. Requested workflow eligibility and replay remain caller checks;
	// payload IDs alone are not ancestry.
	// queryWorkflowLaunchResolveOrigin разрешает живую CLAIMED аренду по точным организации, ссылке,
	// хешу ограждения и поколению, связывая согласованные run, root, node, session, turn, runtime
	// revision, проект, агента и субъекта. Проверяет deadline, состояния, launch capability в ревизии
	// и агенте, assistantScope NONE; блокирует node, lease, root и actor. Допустимость запрошенного
	// workflow и повтор проверяет вызывающий; ID из payload не доказывают происхождение.
	//go:embed sql/workflow_launch__resolve_origin.sql
	queryWorkflowLaunchResolveOrigin string
	// queryWorkflowLaunchExisting finds a prior child/launch/callback edge for the exact organization,
	// origin runtime revision, workflow and request digest. It supports semantic replay but has no
	// fresh origin-authority or lifecycle predicate; those checks precede lookup in the owner path.
	// queryWorkflowLaunchExisting находит прежние child, launch и callback edge по точным организации,
	// исходной runtime revision, workflow и хешу запроса. Служит повтору результата; свежие полномочия
	// источника и жизненный цикл проверяются владельцем до поиска.
	//go:embed sql/workflow_launch__existing.sql
	queryWorkflowLaunchExisting string
	// queryWorkflowLaunchBound checks fewer than 16 direct required launches, ancestor depth below 8
	// and fewer than 128 descendant roots for the tenant-bound launch tree. These structural limits do
	// not check actor authority or concurrent reservation; caller locks serialize the decision.
	// queryWorkflowLaunchBound проверяет менее 16 прямых обязательных запусков, глубину предков менее
	// 8 и менее 128 корней потомков в дереве организации. Структурные пределы не проверяют субъекта и
	// не резервируют место; решение упорядочивается блокировками вызывающего.
	//go:embed sql/workflow_launch__bound.sql
	queryWorkflowLaunchBound string
	// queryWorkflowLaunchInsertProxy inserts a WAITING EXTERNAL_ACTION proxy node under trusted
	// organization/root/run/parent IDs with an OPEN action and child-run reference. Lineage, actor
	// eligibility and atomic launch/callback creation belong to the enclosing transaction.
	// queryWorkflowLaunchInsertProxy вставляет WAITING EXTERNAL_ACTION proxy node с доверенными ID
	// организации, корня, run и родителя, действием OPEN и ссылкой child run. Происхождение, права
	// субъекта и атомарное создание launch и callback принадлежат внешней транзакции.
	//go:embed sql/workflow_launch__insert_proxy.sql
	queryWorkflowLaunchInsertProxy string
	// queryWorkflowLaunchInsert persists origin generation/attempt/input/revision pins and the
	// child/callback/proxy lineage only when the child is an exact organization/project root initiated
	// by the supplied actor. Child workflow version is selected from persisted state; origin
	// authority, callback exactness and affected-row validation remain caller responsibilities.
	// queryWorkflowLaunchInsert сохраняет исходные generation, attempt, input и revision привязки,
	// связи child, callback и proxy, только если child — корень точных организации и проекта, начатый
	// данным субъектом. Версия workflow берётся из состояния child; полномочия источника, точность
	// callback и число строк проверяет вызывающий.
	//go:embed sql/workflow_launch__insert.sql
	queryWorkflowLaunchInsert string
	// queryWorkflowLaunchBindParent assigns the parent ID once to a root child with no existing parent
	// in the exact organization/ref. It does not prove the parent lineage or actor itself; the caller
	// must resolve that relation and require the expected affected-row count.
	// queryWorkflowLaunchBindParent однократно назначает parent ID корневому child без родителя по
	// точным организации и ссылке. Происхождение родителя и субъекта не доказывает; вызывающий
	// разрешает связь и проверяет число изменённых строк.
	//go:embed sql/workflow_launch__bind_parent.sql
	queryWorkflowLaunchBindParent string
	// queryWorkflowLaunchFailed tests whether a resolved internal root has any FAILED or CANCELLED
	// required launch, irrespective of open siblings. There is no independent organization or actor
	// predicate; the caller supplies an owner-resolved root and decides the aggregate outcome.
	// queryWorkflowLaunchFailed проверяет наличие FAILED либо CANCELLED обязательного запуска у
	// разрешённого внутреннего корня независимо от открытых соседних запусков. Собственных условий
	// организации и субъекта нет; корень разрешает владелец, вызывающий определяет общий исход.
	//go:embed sql/workflow_launch__failed.sql
	queryWorkflowLaunchFailed string
	// queryWorkflowLaunchTerminal locks at most 129 OPEN launch rows in exact organization/projects
	// when parent root or child is terminal, returning graph identities, child outcome/version and
	// proxy/callback metadata in launch-ID order. The caller closes dependent lifecycle state; this
	// selection alone performs no terminal transition.
	// queryWorkflowLaunchTerminal блокирует до 129 OPEN launch строк точных организации и проектов при
	// завершённом parent root либо child, возвращая ID графа, исход и версию child, proxy и callback
	// по ID запуска. Связанное состояние закрывает вызывающий; выборка сама не выполняет итоговый
	// переход.
	//go:embed sql/workflow_launch__terminal.sql
	queryWorkflowLaunchTerminal string
	// queryWorkflowLaunchComplete closes an OPEN launch and any nonterminal proxy in one statement
	// with the supplied state and reports whether a proxy changed. It relies on caller-resolved launch
	// ID and legal terminal state; leases, callback effects, audit and aggregate propagation remain in
	// the enclosing owner transaction.
	// queryWorkflowLaunchComplete закрывает OPEN launch и незавершённый proxy одной командой
	// переданным состоянием, сообщая, изменился ли proxy. Опирается на разрешённый ID и допустимый
	// итог; аренды, callback effects, аудит и распространение исхода принадлежат внешней транзакции
	// владельца.
	//go:embed sql/workflow_launch__complete.sql
	queryWorkflowLaunchComplete string
	// queryWorkflowLaunchClaimOrigin rejects an ancestor chain with wrong organization, depth above 8,
	// non-OPEN launch, inactive/non-user actor, invalid parent/agent state, mismatched actor/project
	// pins, missing launch capability or changed child workflow version. An empty ancestry passes the
	// NOT EXISTS predicate; this is a lineage condition alongside the ordinary claim checks, not
	// standalone authority.
	// queryWorkflowLaunchClaimOrigin отклоняет цепочку предков при чужой организации, глубине выше 8,
	// закрытом launch, неактивном либо не-USER субъекте, неверных состояниях parent и агента,
	// несовпадении субъекта и проекта, отсутствии launch capability или иной версии workflow child.
	// Пустая цепочка проходит NOT EXISTS; это дополнительное условие происхождения вместе с обычными
	// claim проверками, а не самостоятельные полномочия.
	//go:embed sql/workflow_launch__claim_origin.sql
	queryWorkflowLaunchClaimOrigin string
)
