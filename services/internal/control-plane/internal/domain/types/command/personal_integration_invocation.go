package command

// PersonalIntegrationInvocationAdmission addresses only an owner-issued invocation lease.
// Subject, credential, endpoint and recovery mode are resolved from owner state, not worker input.
// PersonalIntegrationInvocationAdmission адресует лишь выданный владельцем invocation lease.
// Subject, credential, endpoint и recovery mode разрешаются из owner state, не worker input.
type PersonalIntegrationInvocationAdmission struct {
	InvocationRef, LeaseRef, Fence, WorkloadInstance string
	Generation                                       int64
}
