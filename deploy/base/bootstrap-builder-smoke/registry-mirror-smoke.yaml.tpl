apiVersion: batch/v1
kind: Job
metadata:
  name: kodex-registry-mirror-smoke
  namespace: {{ envOr "KODEX_PRODUCTION_NAMESPACE" "kodex-prod" }}
  labels:
    app.kubernetes.io/name: kodex-registry-mirror-smoke
    app.kubernetes.io/part-of: kodex
    app.kubernetes.io/component: bootstrap-smoke
spec:
  backoffLimit: 1
  ttlSecondsAfterFinished: 600
  template:
    metadata:
      labels:
        app.kubernetes.io/name: kodex-registry-mirror-smoke
        app.kubernetes.io/part-of: kodex
        app.kubernetes.io/component: bootstrap-smoke
    spec:
      restartPolicy: Never
      hostNetwork: true
      dnsPolicy: ClusterFirstWithHostNet
      containers:
        - name: mirror
          image: {{ imageOr "crane" "KODEX_IMAGE_MIRROR_TOOL_IMAGE" }}
          imagePullPolicy: IfNotPresent
          command:
            - crane
          args:
            - copy
            - "--platform={{ envOr "KODEX_IMAGE_MIRROR_PLATFORM" "linux/amd64" }}"
            - "{{ imageOr "busybox" "KODEX_REGISTRY_SMOKE_SOURCE_IMAGE" }}"
            - "{{ envOr "KODEX_INTERNAL_REGISTRY_HOST" "127.0.0.1:5000" }}/kodex/mirror/smoke:bootstrap"
            - --insecure
          resources:
            requests:
              cpu: "{{ envOr "KODEX_IMAGE_MIRROR_CPU_REQUEST" "100m" }}"
              memory: "{{ envOr "KODEX_IMAGE_MIRROR_MEMORY_REQUEST" "128Mi" }}"
            limits:
              cpu: "{{ envOr "KODEX_IMAGE_MIRROR_CPU_LIMIT" "1" }}"
              memory: "{{ envOr "KODEX_IMAGE_MIRROR_MEMORY_LIMIT" "512Mi" }}"
          securityContext:
            allowPrivilegeEscalation: false
            capabilities:
              drop:
                - ALL
---
apiVersion: batch/v1
kind: Job
metadata:
  name: kodex-registry-pull-smoke
  namespace: {{ envOr "KODEX_PRODUCTION_NAMESPACE" "kodex-prod" }}
  labels:
    app.kubernetes.io/name: kodex-registry-pull-smoke
    app.kubernetes.io/part-of: kodex
    app.kubernetes.io/component: bootstrap-smoke
spec:
  backoffLimit: 1
  ttlSecondsAfterFinished: 600
  template:
    metadata:
      labels:
        app.kubernetes.io/name: kodex-registry-pull-smoke
        app.kubernetes.io/part-of: kodex
        app.kubernetes.io/component: bootstrap-smoke
    spec:
      restartPolicy: Never
      containers:
        - name: pull
          image: {{ envOr "KODEX_INTERNAL_REGISTRY_HOST" "127.0.0.1:5000" }}/kodex/mirror/smoke:bootstrap
          imagePullPolicy: Always
          command:
            - sh
            - -ec
            - "echo registry mirror smoke ok"
          resources:
            requests:
              cpu: 10m
              memory: 32Mi
            limits:
              cpu: 100m
              memory: 64Mi
