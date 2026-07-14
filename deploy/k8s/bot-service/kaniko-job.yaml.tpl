apiVersion: batch/v1
kind: Job
metadata:
  name: ${MATTERCODEX_KANIKO_JOB_NAME}
  namespace: ${MATTERCODEX_NAMESPACE}
  labels:
    app.kubernetes.io/name: matter-codex-kaniko
    app.kubernetes.io/component: image-build
    matter-codex.dev/build-component: ${MATTERCODEX_KANIKO_COMPONENT}
spec:
  backoffLimit: 0
  ttlSecondsAfterFinished: ${MATTERCODEX_KANIKO_JOB_TTL_SECONDS}
  activeDeadlineSeconds: ${MATTERCODEX_KANIKO_ACTIVE_DEADLINE_SECONDS}
  template:
    metadata:
      labels:
        app.kubernetes.io/name: matter-codex-kaniko
        app.kubernetes.io/component: image-build
        matter-codex.dev/build-component: ${MATTERCODEX_KANIKO_COMPONENT}
    spec:
      restartPolicy: Never
      containers:
        - name: kaniko
          image: ${MATTERCODEX_KANIKO_IMAGE}
          imagePullPolicy: IfNotPresent
          args:
            - "--context=dir:///workspace/${MATTERCODEX_KANIKO_CONTEXT_SUBDIR}"
            - "--dockerfile=/workspace/${MATTERCODEX_KANIKO_CONTEXT_SUBDIR}/${MATTERCODEX_KANIKO_DOCKERFILE}"
            - "--destination=${MATTERCODEX_KANIKO_DESTINATION}"
            - "--cache=true"
            - "--cache-repo=${MATTERCODEX_KANIKO_CACHE_REPO}"
            - "--insecure"
            - "--insecure-registry=${MATTERCODEX_IMAGE_REGISTRY_PUSH_HOST}"
            - "--skip-unused-stages=true"
            - "--cleanup"
${MATTERCODEX_KANIKO_EXTRA_ARGS_YAML}
          volumeMounts:
            - name: context
              mountPath: /workspace
          resources:
            requests:
              cpu: ${MATTERCODEX_KANIKO_CPU_REQUEST}
              memory: ${MATTERCODEX_KANIKO_MEMORY_REQUEST}
            limits:
              memory: ${MATTERCODEX_KANIKO_MEMORY_LIMIT}
      volumes:
        - name: context
          persistentVolumeClaim:
            claimName: ${MATTERCODEX_KANIKO_CONTEXT_PVC}
