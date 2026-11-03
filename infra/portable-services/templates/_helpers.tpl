{{- define "portable.name" -}}{{ .Release.Name }}{{- end -}}
{{- define "portable.labels" -}}
app.kubernetes.io/part-of: kodex
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/managed-by: Helm
{{- end -}}
{{- define "portable.validate" -}}
{{- $v := .Values -}}
{{- if and $v.storage.ui.enabled (not $v.storage.enabled) -}}{{ fail "storage UI requires storage" }}{{- end -}}
{{- if $v.trustBundle.enabled -}}
{{- if or (ne $v.pki.mode "isolated") (not $v.trustBundle.namespaces) -}}{{ fail "trust bundle requires isolated PKI and exact namespaces" }}{{- end -}}
{{- if not (or $v.storage.enabled $v.registry.enabled) -}}{{ fail "trust bundle requires an enabled service" }}{{- end -}}
{{- end -}}
{{- if or $v.storage.enabled $v.registry.enabled -}}
{{- if eq $v.pki.mode "existing" -}}{{- $_ := required "existing issuerName required" $v.pki.issuerName -}}{{- end -}}
{{- if $v.storage.enabled -}}{{- $_ := required "storage authSecret required" $v.storage.authSecret -}}{{- end -}}
{{- if $v.registry.enabled -}}{{- $_ := required "registry authSecret required" $v.registry.authSecret -}}
{{- if eq $v.registry.readerUsername $v.registry.writerUsername -}}{{ fail "registry identities must differ" }}{{- end -}}{{- end -}}
{{- if ne $v.exposure.mode "disabled" -}}
{{- if eq $v.exposure.tlsMode "external" -}}
{{- if not $v.exposure.trustedProxyCIDRs -}}{{ fail "external TLS requires exact proxy peers" }}{{- end -}}
{{- else -}}{{- $_ := required "managed public TLS secret required" $v.exposure.tlsSecret -}}{{- end -}}
{{- if eq $v.exposure.mode "shared_host" -}}
{{- $_ := required "sharedHost required" $v.exposure.sharedHost -}}
{{- if and $v.storage.enabled (not $v.storage.trustedProxyCIDRs) -}}{{ fail "S3 prefix requires exact backend proxy peers" }}{{- end -}}
{{- range $p := list $v.exposure.s3Prefix $v.exposure.storagePrefix -}}
{{- if regexMatch "^/(api|auth|assets|static|v1|v2|ws|health|ready)(/|$)" $p -}}{{ fail "reserved application prefix" }}{{- end -}}
{{- end -}}
{{- $a := $v.exposure.s3Prefix -}}{{- $b := $v.exposure.storagePrefix -}}
{{- if or (eq $a $b) (hasPrefix (printf "%s/" $a) $b) (hasPrefix (printf "%s/" $b) $a) -}}{{ fail "overlapping service prefixes" }}{{- end -}}
{{- else -}}
{{- if $v.storage.enabled -}}{{- $_ := required "s3Host required" $v.exposure.s3Host -}}{{- end -}}
{{- if $v.storage.ui.enabled -}}{{- $_ := required "storageHost required" $v.exposure.storageHost -}}{{- end -}}
{{- if and $v.registry.enabled $v.registry.expose -}}{{- $_ := required "registryHost required" $v.exposure.registryHost -}}{{- end -}}
{{- $hosts := list -}}
{{- if $v.storage.enabled -}}{{- $hosts = append $hosts $v.exposure.s3Host -}}{{- end -}}
{{- if $v.storage.ui.enabled -}}{{- $hosts = append $hosts $v.exposure.storageHost -}}{{- end -}}
{{- if and $v.registry.enabled $v.registry.expose -}}{{- $hosts = append $hosts $v.exposure.registryHost -}}{{- end -}}
{{- if ne (len $hosts) (len (uniq $hosts)) -}}{{ fail "separate hosts must differ" }}{{- end -}}
{{- end -}}
{{- end -}}
{{- end -}}
{{- end -}}
