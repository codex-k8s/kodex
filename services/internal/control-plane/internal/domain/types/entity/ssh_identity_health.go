package entity

import "github.com/codex-k8s/kodex/libs/go/managedssh"

// SSHIdentityHealthAdmission uses the single private wire model shared with broker; keys never cross this metadata contract.
// SSHIdentityHealthAdmission использует единый private wire model вместе с broker; keys не пересекают metadata contract.
type SSHIdentityHealthAdmission = managedssh.HealthAdmission

// SSHIdentityHealthReceipt acknowledges only original test completion, including discarded late facts, without resource authority.
// SSHIdentityHealthReceipt подтверждает лишь original test completion, включая discarded late facts, без resource authority.
type SSHIdentityHealthReceipt = managedssh.HealthReceipt
