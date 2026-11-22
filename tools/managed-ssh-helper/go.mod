module github.com/codex-k8s/kodex/tools/managed-ssh-helper

go 1.26.10

require (
	github.com/codex-k8s/kodex/libs/go/managedssh v0.0.0
	github.com/codex-k8s/kodex/libs/go/securefile v0.0.0
	golang.org/x/sys v0.48.0
)

replace github.com/codex-k8s/kodex/libs/go/managedssh => ../../libs/go/managedssh

replace github.com/codex-k8s/kodex/libs/go/securefile => ../../libs/go/securefile
