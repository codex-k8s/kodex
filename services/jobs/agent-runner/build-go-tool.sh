#!/bin/sh
# Каждый CLI собирается в отдельном модуле: package@version у go install
# игнорирует внешние require/replace и оставляет уязвимые зависимости upstream.
set -eu

fail() {
  printf 'Runner Go tool build failed: %s\n' "$1" >&2
  exit 1
}

[ "$#" -eq 1 ] || fail ARGUMENT_INVALID
selection=$1
package=${selection%@*}
version=${selection##*@}
[ "$package" != "$selection" ] || fail VERSION_REQUIRED
printf '%s\n' "$version" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+$' || fail VERSION_INVALID
linker_flags=''
kubernetes_staging=''
case "$package" in
  github.com/pressly/goose/v3/cmd/goose) module=github.com/pressly/goose/v3; binary=goose ;;
  github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen) module=github.com/oapi-codegen/oapi-codegen/v2; binary=oapi-codegen ;;
  google.golang.org/protobuf/cmd/protoc-gen-go) module=google.golang.org/protobuf; binary=protoc-gen-go ;;
  google.golang.org/grpc/cmd/protoc-gen-go-grpc) module=$package; binary=protoc-gen-go-grpc ;;
  github.com/golangci/golangci-lint/v2/cmd/golangci-lint) module=github.com/golangci/golangci-lint/v2; binary=golangci-lint ;;
  golang.org/x/tools/cmd/goimports) module=golang.org/x/tools; binary=goimports ;;
  mvdan.cc/gofumpt) module=$package; binary=gofumpt ;;
  honnef.co/go/tools/cmd/staticcheck) module=honnef.co/go/tools; binary=staticcheck ;;
  github.com/bufbuild/buf/cmd/buf) module=github.com/bufbuild/buf; binary=buf ;;
  github.com/fullstorydev/grpcurl/cmd/grpcurl) module=github.com/fullstorydev/grpcurl; binary=grpcurl ;;
  github.com/mikefarah/yq/v4) module=$package; binary=yq ;;
  github.com/sqlc-dev/sqlc/cmd/sqlc) module=github.com/sqlc-dev/sqlc; binary=sqlc ;;
  go.uber.org/mock/mockgen) module=go.uber.org/mock; binary=mockgen ;;
  github.com/cli/cli/v2/cmd/gh)
    module=github.com/cli/cli/v2; binary=gh
    linker_flags="-X github.com/cli/cli/v2/internal/build.Version=${version#v}" ;;
  helm.sh/helm/v4/cmd/helm)
    module=helm.sh/helm/v4; binary=helm
    linker_flags="-X helm.sh/helm/v4/internal/version.version=$version" ;;
  k8s.io/kubernetes/cmd/kubectl)
    [ "$version" = v1.36.2 ] || fail KUBERNETES_SOURCE_VERSION_UNSUPPORTED
    module=k8s.io/kubernetes; binary=kubectl
    # Exact upstream v1.36.2 go.mod: replaces не наследуются wrapper-модулем.
    # Все required staging modules публикуются как corresponding v0.36.2.
    kubernetes_staging='api apiextensions-apiserver apimachinery apiserver cli-runtime
client-go cloud-provider cluster-bootstrap code-generator component-base
component-helpers controller-manager cri-api cri-client cri-streaming
csi-translation-lib dynamic-resource-allocation endpointslice externaljwt kms
kube-aggregator kube-controller-manager kube-proxy kube-scheduler kubectl kubelet
metrics mount-utils pod-security-admission sample-apiserver streaming'
    linker_flags="-X k8s.io/component-base/version.gitVersion=$version -X k8s.io/component-base/version.gitMajor=1 -X k8s.io/component-base/version.gitMinor=36 -X k8s.io/client-go/pkg/version.gitVersion=$version -X k8s.io/client-go/pkg/version.gitMajor=1 -X k8s.io/client-go/pkg/version.gitMinor=36" ;;
  *) fail PACKAGE_NOT_ALLOWED ;;
esac
case "${GOBIN:-}" in ''|/|[!/]*) fail OUTPUT_DIRECTORY_INVALID ;; esac
[ -d "$GOBIN" ] && [ ! -L "$GOBIN" ] || fail OUTPUT_DIRECTORY_INVALID
output_directory=$GOBIN
export GOENV=off GOTOOLCHAIN=local GOWORK=off CGO_ENABLED=0 GOFLAGS='' GOMAXPROCS=4
export GOPROXY=https://proxy.golang.org GOSUMDB=sum.golang.org
export GOPRIVATE='' GONOPROXY='' GONOSUMDB='' GOINSECURE='' GOAUTH=off
[ "$(go env GOVERSION)" = go1.26.6 ] || fail GO_TOOLCHAIN_MISMATCH
scratch=$(mktemp -d "${TMPDIR:-/tmp}/kodex-go-tool.XXXXXXXX") || fail TEMPORARY_DIRECTORY_FAILED
trap 'rm -rf "$scratch"' EXIT
trap 'exit 1' HUP INT TERM
cd "$scratch"

run_go() {
  stage=$1
  shift
  go "$@" > "$scratch/go-output" 2>&1 || fail "$stage"
}

# Точные исправленные релизы; MVS может выбрать более новую транзитивную
# версию, но helper никогда не понижает уже более новую зависимость.
floors='github.com/getkin/kin-openapi v0.149.0
golang.org/x/crypto v0.56.0
golang.org/x/oauth2 v0.27.0
google.golang.org/grpc v1.83.2
oras.land/oras-go/v2 v2.6.2
golang.org/x/mod v0.40.0
golang.org/x/net v0.56.0
golang.org/x/text v0.39.0'

version_at_least() {
  awk -v actual="$1" -v minimum="$2" 'BEGIN {
    if (actual !~ /^v[0-9]+\.[0-9]+\.[0-9]+$/) exit 1;
    sub(/^v/, "", actual); sub(/^v/, "", minimum);
    split(actual, a, "."); split(minimum, b, ".");
    for (i = 1; i <= 3; i++) {
      if (a[i] + 0 > b[i] + 0) exit 0;
      if (a[i] + 0 < b[i] + 0) exit 1;
    }
    exit 0;
  }'
}

run_go MODULE_INITIALIZATION_FAILED mod init kodex.local/runner-tool
run_go MODULE_SELECTION_FAILED mod edit -go=1.26.6 "-require=$module@$version"
if [ -n "$kubernetes_staging" ]; then
  set -- mod edit
  for staging_module in $kubernetes_staging; do
    set -- "$@" "-require=k8s.io/$staging_module@v0.36.2"
  done
  run_go KUBERNETES_STAGING_SELECTION_FAILED "$@"
fi
run_go MODULE_GRAPH_FAILED list -m -mod=mod -f '{{.Path}} {{.Version}} {{if .Replace}}REPLACED{{end}}' all
cp "$scratch/go-output" "$scratch/selected-graph"
pass=0
while :; do
  changed=0
  while read -r dependency minimum; do
    selected=$(awk -v module="$dependency" '$1 == module {print $2}' "$scratch/selected-graph")
    [ -n "$selected" ] || continue
    if ! version_at_least "$selected" "$minimum"; then
      # require задаёт нижнюю границу MVS; аргумент go get задавал бы точную
      # версию и конфликтовал, если новая зависимость требует более свежую.
      run_go DEPENDENCY_CONSTRAINT_FAILED mod edit "-require=$dependency@$minimum"
      changed=1
    fi
  done <<EOF
$floors
EOF
  # Новый релиз может добавить ещё один из восьми проверяемых модулей.
  # Повторяем только закрытый набор, а не подменяем весь upstream graph.
  [ "$pass" -eq 0 ] || [ "$changed" -ne 0 ] || break
  [ "$pass" -lt 9 ] || fail DEPENDENCY_CLOSURE_UNSAFE
  run_go DEPENDENCY_RESOLUTION_FAILED get "$package@$version"
  run_go MODULE_GRAPH_FAILED list -m -mod=mod -f '{{.Path}} {{.Version}} {{if .Replace}}REPLACED{{end}}' all
  cp "$scratch/go-output" "$scratch/selected-graph"
  pass=$((pass + 1))
done
run_go MODULE_VERIFICATION_FAILED mod verify
set -- build -p=4 -mod=readonly -trimpath -buildvcs=false -o "$scratch/$binary"
if [ -n "$linker_flags" ]; then
  set -- "$@" "-ldflags=$linker_flags"
fi
run_go TOOL_BUILD_FAILED "$@" "$package"
run_go BUILD_METADATA_FAILED version -m "$scratch/$binary"
cp "$scratch/go-output" "$scratch/metadata"
awk -v module="$module" -v version="$version" -v package="$package" '
  NR == 1 {if ($NF != "go1.26.6") exit 1}
  $1 == "path" && $2 == package {path = 1}
  $1 == "mod" && $2 == module && $3 == version {selected = 1}
  $1 == "=>" {exit 1}
  END {if (!path || !selected) exit 1}
' "$scratch/metadata" || fail BUILD_IDENTITY_MISMATCH
while read -r dependency minimum; do
  selected=$(awk -v module="$dependency" '$1 == "dep" && $2 == module {print $3}' "$scratch/metadata")
  [ -n "$selected" ] || continue
  version_at_least "$selected" "$minimum" || fail DEPENDENCY_VERSION_UNSAFE
done <<EOF
$floors
EOF
install -m 0555 "$scratch/$binary" "$output_directory/$binary"
printf 'Runner Go tool built: %s %s\n' "$binary" "$version"
