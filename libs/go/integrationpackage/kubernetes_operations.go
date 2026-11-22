package integrationpackage

import "slices"

// KubernetesExecution separates exact API effects from owner-expanded plans, private streams and
// offline formatting. It cannot be selected through a raw kubectl flag or a caller URL.
// KubernetesExecution разделяет exact API effects, owner-expanded plans, private streams и
// offline formatting. Нельзя выбирать его через raw kubectl flag либо caller URL.
type KubernetesExecution string

const (
	// KubernetesResource executes one exact registered GVR/subresource request after owner admission.
	// KubernetesResource исполняет один exact registered GVR/subresource request после owner admission.
	KubernetesResource KubernetesExecution = "RESOURCE"
	// KubernetesPlan expands a finite owner-pinned child set before any child effect is admitted.
	// KubernetesPlan раскрывает конечный owner-pinned child set до допуска любого child effect.
	KubernetesPlan KubernetesExecution = "PLAN"
	// KubernetesStream uses a separately gated, bounded and revocable broker transport.
	// KubernetesStream использует отдельно подтверждённый bounded/revocable broker transport.
	KubernetesStream KubernetesExecution = "STREAM"
	// KubernetesOffline reads only a bounded artifact bundle or safe managed metadata, never kubeconfig.
	// KubernetesOffline читает только bounded artifact bundle либо safe managed metadata, не kubeconfig.
	KubernetesOffline KubernetesExecution = "OFFLINE"
)

// KubernetesOperation is the shared command classification for owner, gateway and broker.
// Verb is nonempty only for one resource request; plan/stream handlers must resolve their complete
// own requirements. A descriptor alone proves neither handler availability nor any authority.
// KubernetesOperation — общая command classification для owner, gateway и broker.
// Verb задан только для одного resource request; plan/stream handlers разрешают полный
// собственный набор requirements. Descriptor не доказывает handler availability либо authority.
type KubernetesOperation struct {
	Key, Family string
	Risk        Risk
	Approval    ApprovalPolicy
	Idempotency IdempotencyMode
	Execution   KubernetesExecution
	Verb        string
	Subresource string
}

// kubernetesOperationRegistry binds command semantics to immutable policy independently of input.
// Setters, destructive fan-out and interactive operations never inherit a read operation's policy.
// kubernetesOperationRegistry связывает command semantics с immutable policy независимо от input.
// Setters, destructive fan-out и interactive operations не наследуют политику read operation.
var kubernetesOperationRegistry = map[string]KubernetesOperation{
	"kubernetes.get":                  kubernetesRead("get", KubernetesResource, "get", ""),
	"kubernetes.list":                 kubernetesRead("get", KubernetesResource, "list", ""),
	"kubernetes.list.all-namespaces":  kubernetesRead("get", KubernetesResource, "list", ""),
	"kubernetes.status.get":           kubernetesRead("get", KubernetesResource, "get", "status"),
	"kubernetes.status.update":        kubernetesWrite("replace", KubernetesResource, "update", "status", false),
	"kubernetes.status.patch":         kubernetesWrite("patch", KubernetesResource, "patch", "status", false),
	"kubernetes.describe":             kubernetesRead("describe", KubernetesPlan, "", ""),
	"kubernetes.events":               kubernetesRead("events", KubernetesResource, "list", ""),
	"kubernetes.watch":                kubernetesRead("watch", KubernetesStream, "", ""),
	"kubernetes.watch.all-namespaces": kubernetesRead("watch", KubernetesStream, "", ""),
	"kubernetes.logs":                 kubernetesRead("logs", KubernetesStream, "", ""),
	"kubernetes.top":                  kubernetesRead("top", KubernetesPlan, "", ""),
	"kubernetes.wait":                 kubernetesRead("wait", KubernetesPlan, "", ""),
	"kubernetes.discovery":            kubernetesRead("api-resources", KubernetesPlan, "", ""),
	"kubernetes.explain":              kubernetesRead("explain", KubernetesPlan, "", ""),
	"kubernetes.version":              kubernetesRead("version", KubernetesPlan, "", ""),
	"kubernetes.cluster-info":         kubernetesRead("cluster-info", KubernetesPlan, "", ""),
	"kubernetes.identity":             kubernetesRead("auth", KubernetesPlan, "", ""),
	"kubernetes.access":               kubernetesRead("auth", KubernetesPlan, "", ""),
	"kubernetes.diff":                 kubernetesRead("diff", KubernetesPlan, "", ""),
	"kubernetes.rollout.history":      kubernetesRead("rollout", KubernetesPlan, "", ""),
	"kubernetes.rollout.status":       kubernetesRead("rollout", KubernetesPlan, "", ""),
	"kubernetes.create":               kubernetesWrite("create", KubernetesResource, "create", "", false),
	"kubernetes.run":                  kubernetesWrite("run", KubernetesResource, "create", "", false),
	"kubernetes.expose":               kubernetesWrite("expose", KubernetesPlan, "", "", false),
	"kubernetes.apply":                kubernetesWrite("apply", KubernetesResource, "patch", "", true),
	"kubernetes.replace":              kubernetesWrite("replace", KubernetesResource, "update", "", true),
	"kubernetes.patch":                kubernetesWrite("patch", KubernetesResource, "patch", "", false),
	"kubernetes.edit":                 kubernetesWrite("edit", KubernetesResource, "update", "", false),
	"kubernetes.delete":               kubernetesDestructive("delete", KubernetesResource, "delete", ""),
	"kubernetes.label":                kubernetesWrite("label", KubernetesResource, "patch", "", false),
	"kubernetes.annotate":             kubernetesWrite("annotate", KubernetesResource, "patch", "", false),
	"kubernetes.scale":                kubernetesWrite("scale", KubernetesResource, "update", "scale", false),
	"kubernetes.autoscale":            kubernetesWrite("autoscale", KubernetesPlan, "", "", false),
	"kubernetes.set.image":            kubernetesWrite("set", KubernetesResource, "patch", "", false),
	"kubernetes.set.env":              kubernetesWrite("set", KubernetesResource, "patch", "", false),
	"kubernetes.set.resources":        kubernetesWrite("set", KubernetesResource, "patch", "", false),
	"kubernetes.set.selector":         kubernetesWrite("set", KubernetesResource, "patch", "", false),
	"kubernetes.set.serviceaccount":   kubernetesWrite("set", KubernetesResource, "patch", "", true),
	"kubernetes.set.subject":          kubernetesWrite("set", KubernetesResource, "patch", "", true),
	"kubernetes.rollout.restart":      kubernetesWrite("rollout", KubernetesPlan, "", "", false),
	"kubernetes.rollout.pause":        kubernetesWrite("rollout", KubernetesResource, "patch", "", false),
	"kubernetes.rollout.resume":       kubernetesWrite("rollout", KubernetesResource, "patch", "", false),
	"kubernetes.rollout.undo":         kubernetesWrite("rollout", KubernetesPlan, "", "", true),
	"kubernetes.cordon":               kubernetesWrite("cordon", KubernetesResource, "patch", "", true),
	"kubernetes.uncordon":             kubernetesWrite("uncordon", KubernetesResource, "patch", "", true),
	"kubernetes.taint":                kubernetesWrite("taint", KubernetesResource, "patch", "", true),
	"kubernetes.drain":                kubernetesDestructive("drain", KubernetesPlan, "", ""),
	"kubernetes.certificate.approve":  kubernetesWrite("certificate", KubernetesPlan, "", "", true),
	"kubernetes.certificate.deny":     kubernetesWrite("certificate", KubernetesPlan, "", "", true),
	"kubernetes.auth.reconcile":       kubernetesWrite("auth", KubernetesPlan, "", "", true),
	"kubernetes.exec":                 kubernetesSensitive("exec"),
	"kubernetes.attach":               kubernetesSensitive("attach"),
	"kubernetes.debug":                kubernetesSensitive("debug"),
	"kubernetes.cp.upload":            kubernetesSensitive("cp"),
	"kubernetes.cp.download":          kubernetesSensitive("cp"),
	"kubernetes.port-forward":         kubernetesSensitive("port-forward"),
	"kubernetes.kustomize":            kubernetesRead("kustomize", KubernetesOffline, "", ""),
	"kubernetes.config.metadata":      kubernetesRead("config", KubernetesOffline, "", ""),
	"kubernetes.help":                 kubernetesRead("help", KubernetesOffline, "", ""),
}

// kubernetesRead classifies a read without making a plan or stream automatically executable.
// kubernetesRead классифицирует read, не делая plan либо stream автоматически исполняемым.
func kubernetesRead(family string, execution KubernetesExecution, verb, subresource string) KubernetesOperation {
	return KubernetesOperation{Family: family, Risk: RiskRead, Approval: ApprovalNone,
		Idempotency: IdempotencyReadOnly, Execution: execution, Verb: verb, Subresource: subresource}
}

// kubernetesWrite assigns a gate and forbids automatic replay of uncertain external writes.
// kubernetesWrite назначает gate и запрещает automatic replay неопределённых external writes.
func kubernetesWrite(family string, execution KubernetesExecution, verb, subresource string, each bool) KubernetesOperation {
	approval := ApprovalHumanScoped
	if each {
		approval = ApprovalHumanEachEffect
	}
	return KubernetesOperation{Family: family, Risk: RiskWrite, Approval: approval,
		Idempotency: IdempotencyOneAttempt, Execution: execution, Verb: verb, Subresource: subresource}
}

// kubernetesDestructive gives every destructive child its own decision and non-replayable receipt.
// kubernetesDestructive требует отдельное решение и non-replayable receipt для каждого destructive child.
func kubernetesDestructive(family string, execution KubernetesExecution, verb, subresource string) KubernetesOperation {
	operation := kubernetesWrite(family, execution, verb, subresource, true)
	operation.Risk = RiskDestructive
	return operation
}

// kubernetesSensitive keeps interactive execution and transfer under a separate per-effect gate.
// kubernetesSensitive задаёт отдельный per-effect gate для interactive execution и transfer.
func kubernetesSensitive(family string) KubernetesOperation {
	operation := kubernetesWrite(family, KubernetesStream, "", "", true)
	operation.Risk = RiskSensitive
	return operation
}

// KubernetesOperationByKey resolves one exact semantic operation; raw client commands and unknown
// names do not receive a default handler, a lower risk or a synthesized permission.
// KubernetesOperationByKey разрешает одну exact semantic operation; raw client commands и unknown
// names не получают default handler, меньший risk либо синтезированное permission.
func KubernetesOperationByKey(key string) (KubernetesOperation, bool) {
	operation, ok := kubernetesOperationRegistry[key]
	if ok {
		operation.Key = key
	}
	return operation, ok
}

// KubernetesOperations returns deterministic value copies for discovery and contract validation.
// It is a known-command catalog, not a declaration that a selected target can run every command.
// KubernetesOperations возвращает упорядоченные value copies для discovery и contract validation.
// Это known-command catalog, не утверждение о доступности каждой команды на выбранном target.
func KubernetesOperations() []KubernetesOperation {
	keys := make([]string, 0, len(kubernetesOperationRegistry))
	for key := range kubernetesOperationRegistry {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	result := make([]KubernetesOperation, 0, len(keys))
	for _, key := range keys {
		operation, _ := KubernetesOperationByKey(key)
		result = append(result, operation)
	}
	return result
}
