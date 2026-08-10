import type { Pinia } from "pinia";

import { useAuditStore } from "@/features/audit/store";
import { useConfigurationStore } from "@/features/configuration/store";
import { useDiagnosticsStore } from "@/features/diagnostics/store";
import { useIncidentDetailsStore } from "@/features/incident-list/details-store";
import { useIncidentListStore } from "@/features/incident-list/store";
import { useInstructionsStore } from "@/features/instructions/store";
import { useIntegrationsStore } from "@/features/integrations/store";
import { useOverviewStore } from "@/features/overview/store";
import { usePeopleStore } from "@/features/people/store";
import { useProjectsStore } from "@/features/projects/store";
import { useProvidersStore } from "@/features/providers/store";
import { useRealtimeStore } from "@/features/realtime/store";
import { useRoleImagesStore } from "@/features/role-images/store";
import { useRunDetailsStore } from "@/features/runs/details-store";
import { useRunsStore } from "@/features/runs/store";
import { useSchedulesStore } from "@/features/schedules/store";
import { useSearchStore } from "@/features/search/store";
import { useWorkspaceRecoveryStore } from "@/features/workspace-recovery/store";
import { useWorkspaceResourcesStore } from "@/features/workspace-resources/store";
import { useWorkspaceTeamStore } from "@/features/workspace-team/store";

export function resetPrivateRuntime(pinia: Pinia): void {
  useRealtimeStore(pinia).reset();
  useAuditStore(pinia).reset();
  useConfigurationStore(pinia).reset();
  useDiagnosticsStore(pinia).reset();
  useIncidentDetailsStore(pinia).reset();
  useIncidentListStore(pinia).reset();
  useInstructionsStore(pinia).reset();
  useIntegrationsStore(pinia).reset();
  useOverviewStore(pinia).reset();
  usePeopleStore(pinia).reset();
  useProvidersStore(pinia).reset();
  useRunDetailsStore(pinia).reset();
  useRunsStore(pinia).reset();
  useSchedulesStore(pinia).reset();
  useWorkspaceRecoveryStore(pinia).reset();
  useWorkspaceTeamStore(pinia).reset();
  useProjectsStore(pinia).reset();
  useRoleImagesStore(pinia).reset();
  useSearchStore(pinia).reset();
  useWorkspaceResourcesStore(pinia).reset();
}
