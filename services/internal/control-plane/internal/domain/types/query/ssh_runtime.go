package query

// SSHRuntimeRead binds result disclosure to one live immutable runtime execution.
// SSHRuntimeRead связывает выдачу результата с одним действующим неизменяемым выполнением.
type SSHRuntimeRead struct {
	InvocationRef, LeaseRef, Fence, RuntimeInputDigest string
	Generation                                         int64
}
