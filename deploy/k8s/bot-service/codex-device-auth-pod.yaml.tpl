apiVersion: v1
kind: Pod
metadata:
  name: ${POD_NAME}
  namespace: ${MATTERCODEX_NAMESPACE}
  labels:
    app.kubernetes.io/name: matter-codex-agent-runner
    app.kubernetes.io/component: codex-device-auth
spec:
  restartPolicy: Never
  automountServiceAccountToken: false
  securityContext:
    runAsNonRoot: true
    runAsUser: 10001
    runAsGroup: 10001
    fsGroup: 10001
    fsGroupChangePolicy: OnRootMismatch
    seccompProfile:
      type: RuntimeDefault
  containers:
    - name: codex-device-auth
      image: ${MATTERCODEX_AGENT_RUNNER_IMAGE}
      imagePullPolicy: IfNotPresent
      command: ["/usr/local/bin/mattercodex-init", "entrypoint", "/usr/local/bin/matter-codex-agent-runner"]
      args: ["codex-auth"]
      volumeMounts:
        - name: codex-home
          mountPath: /codex-home
        - name: runner-home
          mountPath: /home/matter-codex
        - name: runner-tmp
          mountPath: /tmp
      securityContext:
        runAsNonRoot: true
        runAsUser: 10001
        runAsGroup: 10001
        allowPrivilegeEscalation: false
        readOnlyRootFilesystem: true
        capabilities:
          drop:
            - ALL
  volumes:
    - name: codex-home
      emptyDir: {}
    - name: runner-home
      emptyDir: {}
    - name: runner-tmp
      emptyDir: {}
