import { defineStore } from "pinia";
import { reactive } from "vue";

import {
  createTeam,
  fetchMattermostBinding,
  fetchMattermostTeams,
  fetchTeamOperation,
  linkTeam,
  relinkTeam,
  unlinkTeam,
} from "@/shared/api/adapters/owner-control";
import type {
  MattermostMappingOperation,
  MattermostTeam,
  MattermostTeamBinding,
} from "@/shared/api/generated/openapi/types.gen";
import { createFeatureRuntime } from "@/shared/lib/feature-store";
import {
  completeMutationIntent,
  pendingMutationKey,
} from "@/shared/lib/identity";
import { invalidate, remoteState, resetRemoteState } from "@/shared/lib/remote";
import { subscribeRealtimeSnapshot } from "@/shared/realtime/snapshot-bus";

export const useWorkspaceTeamStore = defineStore("workspace-team", () => {
  const teams = reactive(remoteState<MattermostTeam[]>([]));
  const teamBinding = reactive(remoteState<MattermostTeamBinding | null>(null));
  const teamOperation = reactive(
    remoteState<MattermostMappingOperation | null>(null),
  );
  const runtime = createFeatureRuntime();

  async function recoverOperation(
    action: "BIND" | "RELINK" | "UNLINK",
    scope: string,
    body: unknown,
    version?: number,
  ): Promise<boolean> {
    if (!runtime.mutationProblem.value?.retryable) return false;
    const key = await pendingMutationKey(scope, body, version);
    if (!key) return false;
    try {
      const operation = await fetchTeamOperation(action, key);
      teamOperation.data = operation;
      teamOperation.phase = "ready";
      if (
        operation.state === "BOUND" ||
        operation.state === "UNLINKED" ||
        operation.state === "REPAIR_REQUIRED"
      )
        await completeMutationIntent(scope, body, version);
      await loadTeams();
      return true;
    } catch {
      return false;
    }
  }

  const loadTeams = () =>
    Promise.all([
      runtime.loadInto(
        teams,
        async () => (await fetchMattermostTeams()).teams,
        (items) => items.length === 0,
      ),
      runtime.loadInto(
        teamBinding,
        fetchMattermostBinding,
        (value) => value === null,
      ),
    ]).then(() => undefined);
  const addTeam = (displayName: string, slugIntent: string) =>
    runtime.mutate(
      () => createTeam({ displayName, slugIntent }),
      loadTeams,
      teams,
    );
  const bindTeam = async (selector: string) => {
    const body = { selector };
    const ok = await runtime.mutate(
      async () => {
        teamOperation.data = (await linkTeam(selector)).operation;
        teamOperation.phase = "ready";
      },
      loadTeams,
      teamBinding,
    );
    return ok || recoverOperation("BIND", "mattermost-binding:bind", body);
  };
  const rebindTeam = async (
    selector: string,
    binding: MattermostTeamBinding,
  ) => {
    const body = {
      selector,
      expectedGeneration: binding.mappingGeneration,
    };
    const version = binding.mappingVersion;
    const ok = await runtime.mutate(
      async () => {
        teamOperation.data = (await relinkTeam(body, version)).operation;
        teamOperation.phase = "ready";
      },
      loadTeams,
      teamBinding,
    );
    return (
      ok ||
      recoverOperation("RELINK", "mattermost-binding:relink", body, version)
    );
  };
  const removeTeamBinding = async (version: number, generation: number) => {
    const body = { generation };
    const ok = await runtime.mutate(
      async () => {
        teamOperation.data = (await unlinkTeam(version, generation)).operation;
        teamOperation.phase = "ready";
      },
      loadTeams,
      teamBinding,
    );
    return (
      ok ||
      recoverOperation("UNLINK", "mattermost-binding:unlink", body, version)
    );
  };

  function replaceTeams(items: MattermostTeam[]): void {
    invalidate(teams);
    teams.data = items;
    teams.phase = items.length ? "ready" : "empty";
  }
  subscribeRealtimeSnapshot("WORKSPACE_TEAMS", (snapshot) =>
    replaceTeams(snapshot.items.teams ?? []),
  );
  function reset(): void {
    runtime.invalidate();
    resetRemoteState(teams, []);
    resetRemoteState(teamBinding, null);
    resetRemoteState(teamOperation, null);
  }

  return {
    teams,
    teamBinding,
    teamOperation,
    mutationProblem: runtime.mutationProblem,
    mutating: runtime.mutating,
    loadTeams,
    addTeam,
    bindTeam,
    rebindTeam,
    removeTeamBinding,
    replaceTeams,
    reset,
  };
});
