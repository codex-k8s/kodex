import type { RealtimeSnapshot } from "@/shared/api/adapters/realtime";
import { useDiagnosticsStore } from "@/features/diagnostics/store";
import { useIntegrationsStore } from "@/features/integrations/store";
import { useOperationsStore } from "@/features/operations/store";
import { useProvidersStore } from "@/features/providers/store";
import { useWorkspaceRecoveryStore } from "@/features/workspace-recovery/store";
import { useWorkspaceTeamStore } from "@/features/workspace-team/store";

export function bindRealtimeSnapshots(): () => void {
  const operations = useOperationsStore();
  const diagnostics = useDiagnosticsStore();
  const integrations = useIntegrationsStore();
  const providers = useProvidersStore();
  const recovery = useWorkspaceRecoveryStore();
  const teams = useWorkspaceTeamStore();
  const receive = (event: Event): void => {
    const snapshot = (event as CustomEvent<RealtimeSnapshot>).detail;
    if (snapshot.channel === "RUNS") {
      operations.replaceRealtimeRuns(snapshot.items.runs ?? []);
    } else if (snapshot.channel === "RESOURCES") {
      operations.replaceRealtimeResources(snapshot.items.resources ?? []);
    } else if (snapshot.channel === "INCIDENTS") {
      operations.replaceRealtimeIncidents(snapshot.items.incidents ?? []);
    } else if (snapshot.channel === "CONFIGURATION_CHANGES") {
      operations.replaceRealtimeChanges(
        snapshot.items.configurationChanges ?? [],
      );
    } else if (snapshot.channel === "WORKSPACE_TEAMS") {
      teams.replaceTeams(snapshot.items.teams ?? []);
    } else if (snapshot.channel === "PROVIDERS") {
      providers.replaceConnections(snapshot.items.providerConnections ?? []);
    } else if (snapshot.channel === "INTEGRATIONS") {
      integrations.replaceIntegrations(
        snapshot.items.integrationConfigurations ?? [],
      );
    } else if (snapshot.channel === "APPROVALS") {
      integrations.replaceApprovals(snapshot.items.approvals ?? []);
    } else if (snapshot.channel === "BACKUPS") {
      recovery.replaceBackups(snapshot.items.resources ?? []);
    } else {
      diagnostics.replaceHealth(snapshot.items.health ?? []);
    }
  };
  window.addEventListener("mattercodex:realtime-snapshot", receive);
  return () =>
    window.removeEventListener("mattercodex:realtime-snapshot", receive);
}
