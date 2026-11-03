#!/usr/bin/env bash
set -euo pipefail

# Штатный renderer остаётся владельцем Job/PVC. Меняется только SDK native issuer.
/opt/kodex/render-image-admission-job.sh "$@" |
  yq 'with(select(.kind == "Job");
    with(.spec.template.spec.initContainers[]? |
      select(.name == "internal-rpc-authority-issuer" and .restartPolicy == "Always");
      .env = ([.env[] | select(.name == "DEPLOYMENT_ENVIRONMENT")] +
        [{"name": "OTEL_SDK_DISABLED", "value": "true"}] +
        [.env[] | select(.name != "DEPLOYMENT_ENVIRONMENT" and .name != "OTEL_SDK_DISABLED")])
    ))'
