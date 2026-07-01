apiVersion: v1
kind: Pod
metadata:
  name: ${MATTERCODEX_KANIKO_UPLOADER_POD}
  namespace: ${MATTERCODEX_NAMESPACE}
  labels:
    app.kubernetes.io/name: matter-codex-kaniko
    app.kubernetes.io/component: build-context-uploader
spec:
  restartPolicy: Never
  containers:
    - name: uploader
      image: ${MATTERCODEX_BUSYBOX_IMAGE}
      imagePullPolicy: IfNotPresent
      command:
        - sh
        - -c
        - trap 'exit 0' TERM INT; sleep 3600 & wait
      volumeMounts:
        - name: context
          mountPath: /workspace
      resources:
        requests:
          cpu: 25m
          memory: 32Mi
        limits:
          cpu: 250m
          memory: 128Mi
  volumes:
    - name: context
      persistentVolumeClaim:
        claimName: ${MATTERCODEX_KANIKO_CONTEXT_PVC}
