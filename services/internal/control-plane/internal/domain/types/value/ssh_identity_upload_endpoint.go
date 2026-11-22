package value

import "github.com/codex-k8s/kodex/libs/go/managedssh"

// ErrSSHIdentityUploadEndpointInvalid preserves the shared fixed setup endpoint diagnostic without reflecting trusted configuration values.
// ErrSSHIdentityUploadEndpointInvalid сохраняет shared fixed setup endpoint diagnostic без отражения trusted configuration values.
var ErrSSHIdentityUploadEndpointInvalid = managedssh.ErrIdentityUploadEndpointInvalid

// SSHIdentityUploadEndpoint shares the exact HTTPS/SNI contract with the broker transport; it is metadata and never grants a credential read or materialization.
// SSHIdentityUploadEndpoint разделяет exact HTTPS/SNI contract с broker transport; это metadata без допуска credential read либо materialization.
type SSHIdentityUploadEndpoint = managedssh.IdentityUploadEndpoint
