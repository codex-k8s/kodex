package command

import "github.com/codex-k8s/kodex/libs/go/managedssh"

// SSHExecutionComplete reports one joined private observation for an owner-created intent; caller input cannot create another attempt or replace its original lease.
// SSHExecutionComplete сообщает одно joined private observation owner-created intent; caller input не создаёт новую attempt и не заменяет original lease.
type SSHExecutionComplete struct {
	Binding                    managedssh.Binding
	AttemptRef, SnapshotDigest string
	Observation                managedssh.ExecutionObservation
}
