package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/codex-k8s/kodex/libs/go/manifestrender"
)

func main() {
	sourcePath := flag.String("source", "", "source file or directory with *.tpl manifest templates")
	outputPath := flag.String("output", "", "output file or directory")
	envFilePath := flag.String("env-file", "", "optional env file with KEY=value values")
	servicesFilePath := flag.String("services-file", "services.yaml", "optional services.yaml inventory for versions and image resolution")
	flag.Parse()

	if err := manifestrender.Render(manifestrender.Options{
		SourcePath:       *sourcePath,
		OutputPath:       *outputPath,
		EnvFilePath:      *envFilePath,
		ServicesFilePath: *servicesFilePath,
	}); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "manifest-render: %v\n", err)
		os.Exit(1)
	}
}
