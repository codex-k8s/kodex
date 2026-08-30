module github.com/codex-k8s/kodex/services/internal/control-plane

go 1.26.6

require (
	github.com/caarlos0/env/v11 v11.4.1
	github.com/codex-k8s/kodex/libs/go/controlplaneapi v0.0.0
	github.com/codex-k8s/kodex/libs/go/eventing v0.0.0
	github.com/codex-k8s/kodex/libs/go/grpcserver v0.0.0
	github.com/codex-k8s/kodex/libs/go/integrationpackage v0.0.0
	github.com/codex-k8s/kodex/libs/go/internalrpcauth v0.0.0
	github.com/codex-k8s/kodex/libs/go/objectstorage v0.0.0
	github.com/codex-k8s/kodex/libs/go/oidcverifier v0.0.0
	github.com/codex-k8s/kodex/libs/go/runtimecontract v0.0.0
	github.com/codex-k8s/kodex/libs/go/runtimesecret v0.0.0
	github.com/codex-k8s/kodex/libs/go/serviceruntime v0.0.0-00010101000000-000000000000
	github.com/google/uuid v1.6.0
	github.com/jackc/pgx/v5 v5.10.0
	github.com/pressly/goose/v3 v3.27.3
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260720211330-0afa2a65878a
	google.golang.org/grpc v1.82.1
	google.golang.org/protobuf v1.36.12-0.20260120151049-f2248ac996af
)

require (
	github.com/BurntSushi/toml v1.6.0 // indirect
	github.com/aws/aws-sdk-go-v2 v1.45.1 // indirect
	github.com/aws/aws-sdk-go-v2/aws/protocol/eventstream v1.7.20 // indirect
	github.com/aws/aws-sdk-go-v2/config v1.33.1 // indirect
	github.com/aws/aws-sdk-go-v2/credentials v1.20.1 // indirect
	github.com/aws/aws-sdk-go-v2/feature/ec2/imds v1.19.1 // indirect
	github.com/aws/aws-sdk-go-v2/internal/configsources v1.5.1 // indirect
	github.com/aws/aws-sdk-go-v2/internal/endpoints/v2 v2.8.1 // indirect
	github.com/aws/aws-sdk-go-v2/internal/v4a v1.5.1 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/accept-encoding v1.13.19 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/checksum v1.11.1 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/presigned-url v1.14.1 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/s3shared v1.20.1 // indirect
	github.com/aws/aws-sdk-go-v2/service/s3 v1.109.1 // indirect
	github.com/aws/aws-sdk-go-v2/service/signin v1.7.1 // indirect
	github.com/aws/aws-sdk-go-v2/service/sso v1.35.1 // indirect
	github.com/aws/aws-sdk-go-v2/service/ssooidc v1.40.1 // indirect
	github.com/aws/aws-sdk-go-v2/service/sts v1.47.1 // indirect
	github.com/aws/smithy-go v1.28.1 // indirect
	github.com/codex-k8s/kodex/libs/go/oidcidentity v0.0.0 // indirect
	github.com/codex-k8s/kodex/libs/go/securefile v0.0.0 // indirect
	github.com/coreos/go-oidc/v3 v3.20.0 // indirect
	github.com/davecgh/go-spew v1.1.2-0.20180830191138-d8f796af33cc // indirect
	github.com/decred/dcrd/dcrec/secp256k1/v4 v4.4.1 // indirect
	github.com/emicklei/go-restful/v3 v3.13.0 // indirect
	github.com/fxamacker/cbor/v2 v2.9.0 // indirect
	github.com/go-jose/go-jose/v4 v4.1.4 // indirect
	github.com/go-logr/logr v1.4.4 // indirect
	github.com/go-openapi/jsonpointer v0.21.0 // indirect
	github.com/go-openapi/jsonreference v0.20.2 // indirect
	github.com/go-openapi/swag v0.23.0 // indirect
	github.com/goccy/go-json v0.10.6 // indirect
	github.com/google/gnostic-models v0.7.0 // indirect
	github.com/gowebpki/jcs v1.0.1 // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	github.com/josharian/intern v1.0.0 // indirect
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/klauspost/compress v1.19.1 // indirect
	github.com/lestrrat-go/blackmagic v1.0.4 // indirect
	github.com/lestrrat-go/dsig v1.3.0 // indirect
	github.com/lestrrat-go/dsig-secp256k1 v1.0.0 // indirect
	github.com/lestrrat-go/httpcc v1.0.1 // indirect
	github.com/lestrrat-go/httprc/v3 v3.0.6 // indirect
	github.com/lestrrat-go/jwx/v3 v3.2.0 // indirect
	github.com/lestrrat-go/option/v2 v2.0.0 // indirect
	github.com/mailru/easyjson v0.7.7 // indirect
	github.com/mfridman/interpolate v0.0.2 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.3-0.20250322232337-35a7c28c31ee // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/nats-io/nats.go v1.52.0 // indirect
	github.com/nats-io/nkeys v0.4.15 // indirect
	github.com/nats-io/nuid v1.0.1 // indirect
	github.com/rogpeppe/go-internal v1.16.0 // indirect
	github.com/segmentio/asm v1.2.1 // indirect
	github.com/sethvargo/go-retry v0.4.0 // indirect
	github.com/valyala/fastjson v1.6.10 // indirect
	github.com/x448/float16 v0.8.4 // indirect
	go.opentelemetry.io/otel/sdk/metric v1.44.0 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	go.yaml.in/yaml/v2 v2.4.3 // indirect
	go.yaml.in/yaml/v3 v3.0.4 // indirect
	golang.org/x/crypto v0.54.0 // indirect
	golang.org/x/net v0.57.0 // indirect
	golang.org/x/oauth2 v0.36.0 // indirect
	golang.org/x/sync v0.22.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
	golang.org/x/term v0.45.0 // indirect
	golang.org/x/text v0.40.0 // indirect
	golang.org/x/time v0.14.0 // indirect
	gopkg.in/evanphx/json-patch.v4 v4.13.0 // indirect
	gopkg.in/inf.v0 v0.9.1 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
	k8s.io/api v0.36.3
	k8s.io/apimachinery v0.36.3
	k8s.io/client-go v0.36.3
	k8s.io/klog/v2 v2.140.0 // indirect
	k8s.io/kube-openapi v0.0.0-20260317180543-43fb72c5454a // indirect
	k8s.io/utils v0.0.0-20260210185600-b8788abfbbc2 // indirect
	sigs.k8s.io/json v0.0.0-20250730193827-2d320260d730 // indirect
	sigs.k8s.io/randfill v1.0.0 // indirect
	sigs.k8s.io/structured-merge-diff/v6 v6.3.3 // indirect
	sigs.k8s.io/yaml v1.6.0 // indirect
)

replace github.com/codex-k8s/kodex/libs/go/cache => ../../../libs/go/cache

replace github.com/codex-k8s/kodex/libs/go/controlplaneapi => ../../../libs/go/controlplaneapi

replace github.com/codex-k8s/kodex/libs/go/eventing => ../../../libs/go/eventing

replace github.com/codex-k8s/kodex/libs/go/grpcserver => ../../../libs/go/grpcserver

replace github.com/codex-k8s/kodex/libs/go/i18n => ../../../libs/go/i18n

replace github.com/codex-k8s/kodex/libs/go/internalrpcauth => ../../../libs/go/internalrpcauth

replace github.com/codex-k8s/kodex/libs/go/integrationpackage => ../../../libs/go/integrationpackage

replace github.com/codex-k8s/kodex/libs/go/integrationgatewayauth => ../../../libs/go/integrationgatewayauth

replace github.com/codex-k8s/kodex/libs/go/observability => ../../../libs/go/observability

replace github.com/codex-k8s/kodex/libs/go/oidcidentity => ../../../libs/go/oidcidentity

replace github.com/codex-k8s/kodex/libs/go/oidcverifier => ../../../libs/go/oidcverifier

replace github.com/codex-k8s/kodex/libs/go/objectstorage => ../../../libs/go/objectstorage

replace github.com/codex-k8s/kodex/libs/go/runtimecontract => ../../../libs/go/runtimecontract

replace github.com/codex-k8s/kodex/libs/go/runtimesecret => ../../../libs/go/runtimesecret

replace github.com/codex-k8s/kodex/libs/go/serviceruntime => ../../../libs/go/serviceruntime

replace github.com/codex-k8s/kodex/libs/go/securefile => ../../../libs/go/securefile
