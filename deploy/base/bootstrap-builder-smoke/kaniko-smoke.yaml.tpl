apiVersion: batch/v1
kind: Job
metadata:
  name: kodex-kaniko-build-smoke
  namespace: {{ envOr "KODEX_PRODUCTION_NAMESPACE" "kodex-prod" }}
  labels:
    app.kubernetes.io/name: kodex-kaniko-build-smoke
    app.kubernetes.io/part-of: kodex
    app.kubernetes.io/component: bootstrap-smoke
spec:
  backoffLimit: 1
  ttlSecondsAfterFinished: 600
  template:
    metadata:
      labels:
        app.kubernetes.io/name: kodex-kaniko-build-smoke
        app.kubernetes.io/part-of: kodex
        app.kubernetes.io/component: bootstrap-smoke
    spec:
      restartPolicy: Never
      hostNetwork: true
      dnsPolicy: ClusterFirstWithHostNet
      containers:
        - name: kaniko
          image: {{ imageOr "kaniko-executor" "KODEX_KANIKO_EXECUTOR_IMAGE" }}
          imagePullPolicy: IfNotPresent
          args:
            - "--context=dir:///workspace/context"
            - "--dockerfile=/workspace/context/Dockerfile"
            - "--destination={{ envOr "KODEX_INTERNAL_REGISTRY_HOST" "127.0.0.1:5000" }}/kodex/smoke/kaniko:bootstrap"
            - "--insecure"
            - "--skip-tls-verify"
            - "--cache={{ envOr "KODEX_KANIKO_CACHE_ENABLED" "false" }}"
            - "--snapshot-mode={{ envOr "KODEX_KANIKO_SNAPSHOT_MODE" "redo" }}"
            - "--verbosity={{ envOr "KODEX_KANIKO_VERBOSITY" "info" }}"
          resources:
            requests:
              cpu: "{{ envOr "KODEX_KANIKO_CPU_REQUEST" "1" }}"
              memory: "{{ envOr "KODEX_KANIKO_MEMORY_REQUEST" "512Mi" }}"
            limits:
              cpu: "{{ envOr "KODEX_KANIKO_CPU_LIMIT" "2" }}"
              memory: "{{ envOr "KODEX_KANIKO_MEMORY_LIMIT" "2Gi" }}"
          volumeMounts:
            - name: context
              mountPath: /workspace/context
              readOnly: true
      volumes:
        - name: context
          configMap:
            name: kodex-kaniko-smoke-context
