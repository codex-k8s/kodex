// Package kubernetesprojection содержит общие проверки наблюдаемых Kubernetes-проекций control-plane.
package kubernetesprojection

import appsv1 "k8s.io/api/apps/v1"

// DeploymentReady requires observed identity/version, positive desired replicas and exact
// generation/replica agreement with no unavailable replicas. It does not prove endpoint reachability.
// DeploymentReady требует наблюдаемые identity/version, положительные desired replicas и точное
// соответствие generation/replica без unavailable replicas. Не подтверждает доступность endpoint.
func DeploymentReady(deployment *appsv1.Deployment) bool {
	if deployment.UID == "" || deployment.ResourceVersion == "" || deployment.Generation < 1 || deployment.Spec.Replicas == nil || *deployment.Spec.Replicas < 1 {
		return false
	}
	desired := *deployment.Spec.Replicas
	status := deployment.Status
	return status.ObservedGeneration == deployment.Generation && status.Replicas == desired && status.UpdatedReplicas == desired && status.ReadyReplicas == desired && status.AvailableReplicas == desired && status.UnavailableReplicas == 0
}
