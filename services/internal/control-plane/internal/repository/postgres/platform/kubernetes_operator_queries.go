package platform

import _ "embed"

var (
	// queryKubernetesOperatorSerialize takes the fixed transaction lock before first or subsequent import.
	// queryKubernetesOperatorSerialize сериализует первый и последующие imports фиксированной transaction lock.
	//go:embed sql/kubernetes_operator__serialize.sql
	queryKubernetesOperatorSerialize string

	// queryKubernetesOperatorCurrentLock locks the authoritative pointer before accepting a source.
	// queryKubernetesOperatorCurrentLock блокирует authoritative pointer перед принятием source.
	//go:embed sql/kubernetes_operator__current_lock.sql
	queryKubernetesOperatorCurrentLock string

	// queryKubernetesOperatorCurrent reads the authoritative current source without selecting a fallback.
	// queryKubernetesOperatorCurrent читает authoritative current source без выбора fallback.
	//go:embed sql/kubernetes_operator__current.sql
	queryKubernetesOperatorCurrent string

	// queryKubernetesOperatorResetShared invalidates old shared test readiness after an operator source change.
	// queryKubernetesOperatorResetShared сбрасывает прежнюю shared test readiness после operator source change.
	//go:embed sql/kubernetes_operator__reset_shared.sql
	queryKubernetesOperatorResetShared string

	// queryKubernetesOperatorEmpty distinguishes first enrollment from a lost current pointer.
	// queryKubernetesOperatorEmpty отличает первое enrollment от потери current pointer.
	//go:embed sql/kubernetes_operator__empty.sql
	queryKubernetesOperatorEmpty string

	// queryKubernetesOperatorInsert stores one immutable canonical source and digest.
	// queryKubernetesOperatorInsert сохраняет immutable canonical source и digest.
	//go:embed sql/kubernetes_operator__insert.sql
	queryKubernetesOperatorInsert string

	// queryKubernetesOperatorTargetLatest reads the target revision high-watermark without modifying history.
	// queryKubernetesOperatorTargetLatest читает target revision high-watermark без изменения history.
	//go:embed sql/kubernetes_operator__target_latest.sql
	queryKubernetesOperatorTargetLatest string

	// queryKubernetesOperatorTarget reads one exact historical target incarnation.
	// queryKubernetesOperatorTarget читает exact historical target incarnation.
	//go:embed sql/kubernetes_operator__target.sql
	queryKubernetesOperatorTarget string

	// queryKubernetesOperatorTargetInsert appends an immutable target revision without overwrite.
	// queryKubernetesOperatorTargetInsert добавляет immutable target revision без overwrite.
	//go:embed sql/kubernetes_operator__target_insert.sql
	queryKubernetesOperatorTargetInsert string

	// queryKubernetesOperatorKey resolves global serving-key ownership and permanent retirement.
	// queryKubernetesOperatorKey разрешает global serving-key ownership и permanent retirement.
	//go:embed sql/kubernetes_operator__key.sql
	queryKubernetesOperatorKey string

	// queryKubernetesOperatorKeyInsert binds a new serving key to one cluster and NodeUID.
	// queryKubernetesOperatorKeyInsert привязывает новый serving key к одному cluster и NodeUID.
	//go:embed sql/kubernetes_operator__key_insert.sql
	queryKubernetesOperatorKeyInsert string

	// queryKubernetesOperatorNode reads one immutable pin and its retirement fact.
	// queryKubernetesOperatorNode читает immutable pin и retirement fact.
	//go:embed sql/kubernetes_operator__node.sql
	queryKubernetesOperatorNode string

	// queryKubernetesOperatorNodeGeneration reads the accepted node/protocol generation high-watermark.
	// queryKubernetesOperatorNodeGeneration читает accepted node/protocol generation high-watermark.
	//go:embed sql/kubernetes_operator__node_generation.sql
	queryKubernetesOperatorNodeGeneration string

	// queryKubernetesOperatorNodeInsert appends one exact pin without reviving a retired ref.
	// queryKubernetesOperatorNodeInsert добавляет exact pin без восстановления retired ref.
	//go:embed sql/kubernetes_operator__node_insert.sql
	queryKubernetesOperatorNodeInsert string

	// queryKubernetesOperatorRetirePins permanently retires pins omitted from the active source.
	// queryKubernetesOperatorRetirePins навсегда закрывает pins вне active source.
	//go:embed sql/kubernetes_operator__retire_pins.sql
	queryKubernetesOperatorRetirePins string

	// queryKubernetesOperatorRetireKeys retires a key only when no active pin still uses it.
	// queryKubernetesOperatorRetireKeys закрывает key только при отсутствии active pin с этим key.
	//go:embed sql/kubernetes_operator__retire_keys.sql
	queryKubernetesOperatorRetireKeys string

	// queryKubernetesOperatorAdvance advances the pointer after all metadata facts are stored.
	// queryKubernetesOperatorAdvance меняет pointer после сохранения всех metadata facts.
	//go:embed sql/kubernetes_operator__advance.sql
	queryKubernetesOperatorAdvance string

	// queryKubernetesOperatorActive refuses unsettled canonical Kubernetes invocation and test owners.
	// queryKubernetesOperatorActive отклоняет незавершённые canonical Kubernetes invocation и test owners.
	//go:embed sql/kubernetes_operator__active.sql
	queryKubernetesOperatorActive string

	// queryKubernetesOperatorCancelPersonalTests closes unclaimed personal probes before source replacement.
	// queryKubernetesOperatorCancelPersonalTests закрывает unclaimed personal probes перед сменой source.
	//go:embed sql/kubernetes_operator__cancel_personal_tests.sql
	queryKubernetesOperatorCancelPersonalTests string

	// queryKubernetesOperatorCancelSharedTests closes unclaimed shared probes before source replacement.
	// queryKubernetesOperatorCancelSharedTests закрывает unclaimed shared probes перед сменой source.
	//go:embed sql/kubernetes_operator__cancel_shared_tests.sql
	queryKubernetesOperatorCancelSharedTests string

	// queryKubernetesOperatorResetBindings invalidates Kubernetes verification without changing credential generation.
	// queryKubernetesOperatorResetBindings сбрасывает Kubernetes verification без изменения credential generation.
	//go:embed sql/kubernetes_operator__reset_bindings.sql
	queryKubernetesOperatorResetBindings string
)
