import type { Pinia } from "pinia";

import { useDiagnosticsStore } from "@/features/diagnostics/store";
import { useIncidentDetailsStore } from "@/features/incident-details/store";
import { useInstructionsStore } from "@/features/instructions/store";
import { useIntegrationsStore } from "@/features/integrations/store";
import { useOperationsStore } from "@/features/operations/store";
import { usePeopleStore } from "@/features/people/store";
import { useProjectsStore } from "@/features/projects/store";
import { useProvidersStore } from "@/features/providers/store";
import { useRealtimeStore } from "@/features/realtime/store";
import { useRoleImagesStore } from "@/features/role-images/store";
import { useRunDetailsStore } from "@/features/run-details/store";
import { useSchedulesStore } from "@/features/schedules/store";
import { useSearchStore } from "@/features/search/store";
import { useWorkspaceRecoveryStore } from "@/features/workspace-recovery/store";
import { useWorkspaceResourcesStore } from "@/features/workspace-resources/store";
import { useWorkspaceTeamStore } from "@/features/workspace-team/store";

export function resetPrivateRuntime(pinia: Pinia): void {
  useRealtimeStore(pinia).reset();
  useOperationsStore(pinia).reset();
  useDiagnosticsStore(pinia).reset();
  useIncidentDetailsStore(pinia).reset();
  useInstructionsStore(pinia).reset();
  useIntegrationsStore(pinia).reset();
  usePeopleStore(pinia).reset();
  useProvidersStore(pinia).reset();
  useRunDetailsStore(pinia).reset();
  useSchedulesStore(pinia).reset();
  useWorkspaceRecoveryStore(pinia).reset();
  useWorkspaceTeamStore(pinia).reset();
  useProjectsStore(pinia).reset();
  useRoleImagesStore(pinia).reset();
  useSearchStore(pinia).reset();
  useWorkspaceResourcesStore(pinia).reset();
}
