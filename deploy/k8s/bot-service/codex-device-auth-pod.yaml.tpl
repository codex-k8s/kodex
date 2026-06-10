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
  containers:
    - name: codex-device-auth
      image: ${MATTERCODEX_AGENT_RUNNER_IMAGE}
      imagePullPolicy: IfNotPresent
      command: ["matter-codex-agent-runner"]
      args: ["codex-auth"]
