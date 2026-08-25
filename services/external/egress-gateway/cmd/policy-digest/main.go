package main

import (
	"fmt"
	"os"

	"github.com/codex-k8s/kodex/services/external/egress-gateway/internal/policy"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: policy-digest policy-file")
		os.Exit(2)
	}
	digest, err := policy.DigestFile(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, "policy digest calculation failed")
		os.Exit(1)
	}
	fmt.Println(digest)
}
