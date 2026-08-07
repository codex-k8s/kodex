import { defineStore } from "pinia";
import { reactive, ref } from "vue";

import { fetchResources } from "@/shared/api/adapters/resources";
import {
  createAutomation,
  deleteAutomation,
  fetchScheduleOccurrences,
  recoverScheduleOccurrence,
  runAutomationNow,
  updateAutomation,
} from "@/shared/api/adapters/schedules";
import type {
  CreateSchedule,
  ResolveScheduleRecovery,
  Resource,
  ScheduleOccurrence,
  UpdateSchedule,
} from "@/shared/api/generated/openapi/types.gen";
import { asProblem, type AppProblem } from "@/shared/api/problem";
import {
  beginRequest,
  failRequest,
  finishRequest,
  invalidate,
  remoteState,
} from "@/shared/lib/remote";

export interface ScheduleCatalogs {
  targets: Resource[];
  prompts: Resource[];
  artifacts: Resource[];
  runtimes: Resource[];
  rooms: Resource[];
  sessions: Resource[];
}

const emptyCatalogs = (): ScheduleCatalogs => ({
  targets: [],
  prompts: [],
  artifacts: [],
  runtimes: [],
  rooms: [],
  sessions: [],
});

export const useSchedulesStore = defineStore("schedules", () => {
  const schedules = reactive(remoteState<Resource[]>([]));
  const catalogs = reactive(remoteState<ScheduleCatalogs>(emptyCatalogs()));
  const occurrences = reactive(
    remoteState<Record<string, ScheduleOccurrence[]>>({}),
  );
  const mutationProblem = ref<AppProblem | null>(null);
  const mutating = ref(false);
  let mutationVersion = 0;

  async function load(): Promise<void> {
    const scheduleRequest = beginRequest(schedules);
    const catalogRequest = beginRequest(catalogs);
    try {
      const [
        schedulePage,
        targets,
        prompts,
        artifacts,
        runtimes,
        rooms,
        sessions,
      ] = await Promise.all([
        fetchResources("SCHEDULE"),
        fetchResources("ROLE"),
        fetchResources("PROMPT_PROFILE"),
        fetchResources("ARTIFACT"),
        fetchResources("RUNTIME_REVISION"),
        fetchResources("CHAT"),
        fetchResources("SESSION"),
      ]);
      finishRequest(
        schedules,
        scheduleRequest,
        schedulePage.resources,
        schedulePage.resources.length === 0,
      );
      const data = {
        targets: targets.resources,
        prompts: prompts.resources,
        artifacts: artifacts.resources,
        runtimes: runtimes.resources,
        rooms: rooms.resources,
        sessions: sessions.resources,
      };
      const empty = Object.values(data).every((items) => items.length === 0);
      finishRequest(catalogs, catalogRequest, data, empty);
    } catch (error) {
      const problem = asProblem(error);
      failRequest(schedules, scheduleRequest, problem);
      failRequest(catalogs, catalogRequest, problem);
    }
  }

  async function loadOccurrences(scheduleId: string): Promise<void> {
    const request = beginRequest(occurrences);
    try {
      const page = await fetchScheduleOccurrences(scheduleId);
      const data = { ...occurrences.data, [scheduleId]: page.occurrences };
      finishRequest(occurrences, request, data, page.occurrences.length === 0);
    } catch (error) {
      failRequest(occurrences, request, asProblem(error));
    }
  }

  async function mutate(operation: () => Promise<unknown>): Promise<boolean> {
    const version = ++mutationVersion;
    invalidate(schedules);
    invalidate(catalogs);
    invalidate(occurrences);
    mutationProblem.value = null;
    mutating.value = true;
    try {
      await operation();
      if (version !== mutationVersion) return false;
      await load();
      if (version !== mutationVersion) return false;
      return true;
    } catch (error) {
      if (version !== mutationVersion) return false;
      mutationProblem.value = asProblem(error);
      if (mutationProblem.value.kind === "conflict")
        schedules.phase = "conflict";
      return false;
    } finally {
      if (version === mutationVersion) mutating.value = false;
    }
  }

  const create = (body: CreateSchedule) => mutate(() => createAutomation(body));
  const update = (resource: Resource, body: UpdateSchedule) =>
    mutate(() => updateAutomation(resource, body));
  const remove = (resource: Resource) =>
    mutate(() => deleteAutomation(resource));
  const runNow = (resource: Resource) =>
    mutate(() => runAutomationNow(resource));
  const recover = async (
    schedule: Resource,
    occurrence: ScheduleOccurrence,
    body: ResolveScheduleRecovery,
  ) => {
    const success = await mutate(() =>
      recoverScheduleOccurrence(schedule, occurrence, body),
    );
    if (success) await loadOccurrences(schedule.id);
    return success;
  };

  return {
    schedules,
    catalogs,
    occurrences,
    mutationProblem,
    mutating,
    load,
    loadOccurrences,
    create,
    update,
    remove,
    runNow,
    recover,
  };
});
