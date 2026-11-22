package query

// KubernetesRuntimeRead binds result disclosure to one live immutable runtime execution.
// KubernetesRuntimeRead связывает выдачу результата с одним действующим неизменяемым выполнением.
type KubernetesRuntimeRead struct {
	InvocationRef, LeaseRef, Fence, RuntimeInputDigest string
	Generation                                         int64
}
