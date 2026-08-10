import { defineStore } from "pinia";
import { reactive } from "vue";

import { schedulesApi } from "@/features/schedules/api";
import {
  buildScheduleCommand,
  type ScheduleDraft,
  type ScheduleCatalogModel,
  type ScheduleOccurrenceModel,
  type ScheduleResourceOption,
  type ScheduleView,
  toScheduleCatalogModel,
  toScheduleOccurrenceModel,
  toScheduleResourceOption,
  toScheduleView,
} from "@/features/schedules/model";
import { createFeatureRuntime } from "@/shared/lib/feature-store";
import { remoteState, resetRemoteState } from "@/shared/lib/remote";

export const useSchedulesStore = defineStore("schedules", () => {
  const scheduleSelectors = reactive(
    remoteState<ScheduleCatalogModel | null>(null),
  );
  const schedules = reactive(remoteState<ScheduleView[]>([]));
  const scheduleOccurrences = reactive(
    remoteState<ScheduleOccurrenceModel[]>([]),
  );
  const artifacts = reactive(remoteState<ScheduleResourceOption[]>([]));
  const rooms = reactive(remoteState<ScheduleResourceOption[]>([]));
  const scheduleRuntime = createFeatureRuntime();
  const occurrenceRuntime = createFeatureRuntime();
  const loadSchedules = () =>
    Promise.all([
      scheduleRuntime.loadInto(
        scheduleSelectors,
        async () => toScheduleCatalogModel(await schedulesApi.selectors()),
        (value) => value === null,
      ),
      scheduleRuntime.loadInto(
        schedules,
        async () => (await schedulesApi.list()).items.map(toScheduleView),
        (items) => items.length === 0,
      ),
      scheduleRuntime.loadInto(
        artifacts,
        async () =>
          (await schedulesApi.listResources("ARTIFACT")).resources.map(
            toScheduleResourceOption,
          ),
        (items) => items.length === 0,
      ),
      scheduleRuntime.loadInto(
        rooms,
        async () =>
          (await schedulesApi.listResources("CHAT")).resources.map(
            toScheduleResourceOption,
          ),
        (items) => items.length === 0,
      ),
    ]).then(() => undefined);
  const saveScheduleDraft = (
    value: ScheduleView | null,
    draft: ScheduleDraft,
  ) => {
    const body = buildScheduleCommand(draft);
    return scheduleRuntime.mutate(
      () =>
        value
          ? schedulesApi.update(value.scheduleRef, value.version, body)
          : schedulesApi.create(body),
      loadSchedules,
      schedules,
    );
  };
  const runSchedule = (value: ScheduleView) =>
    scheduleRuntime.mutate(
      () => schedulesApi.runNow(value.scheduleRef, value.version),
      loadSchedules,
      schedules,
    );
  const removeSchedule = (value: ScheduleView) =>
    scheduleRuntime.mutate(
      () => schedulesApi.remove(value.scheduleRef, value.version),
      loadSchedules,
      schedules,
    );
  const transitionSchedule = (
    value: ScheduleView,
    targetState: "ACTIVE" | "PAUSED",
  ) =>
    scheduleRuntime.mutate(
      async () => {
        const resource = await schedulesApi.getResource(
          value.scheduleRef,
          "SCHEDULE",
        );
        return schedulesApi.transition(resource, {
          targetState,
          reasonCode: "OWNER_REQUEST",
        });
      },
      loadSchedules,
      schedules,
    );
  const loadScheduleOccurrences = (ref: string) =>
    occurrenceRuntime.loadInto(
      scheduleOccurrences,
      async () =>
        (await schedulesApi.occurrences(ref)).occurrences.map(
          toScheduleOccurrenceModel,
        ),
      (items) => items.length === 0,
    );
  const resolveScheduleOccurrence = (
    schedule: ScheduleView,
    occurrence: ScheduleOccurrenceModel,
    action: "REPAIR" | "CANCEL" | "SKIP",
    reasonCode: string,
  ) => {
    const recoveryEvidenceSha256 = occurrence.recoveryEvidenceSha256;
    if (!recoveryEvidenceSha256) return Promise.resolve(false);
    return occurrenceRuntime.mutate(
      () =>
        schedulesApi.recover(
          schedule.scheduleRef,
          occurrence.occurrenceId,
          occurrence.version,
          {
            action,
            expectedAttempt: occurrence.attempt,
            recoveryEvidenceSha256,
            reasonCode,
          },
        ),
      () => loadScheduleOccurrences(schedule.scheduleRef),
      scheduleOccurrences,
    );
  };
  function reset(): void {
    scheduleRuntime.invalidate();
    occurrenceRuntime.invalidate();
    resetRemoteState(scheduleSelectors, null);
    resetRemoteState(schedules, []);
    resetRemoteState(scheduleOccurrences, []);
    resetRemoteState(artifacts, []);
    resetRemoteState(rooms, []);
  }
  return {
    scheduleSelectors,
    schedules,
    scheduleOccurrences,
    artifacts,
    rooms,
    mutationProblem: scheduleRuntime.mutationProblem,
    occurrenceMutationProblem: occurrenceRuntime.mutationProblem,
    mutating: scheduleRuntime.mutating,
    occurrenceMutating: occurrenceRuntime.mutating,
    loadSchedules,
    saveScheduleDraft,
    runSchedule,
    removeSchedule,
    transitionSchedule,
    loadScheduleOccurrences,
    resolveScheduleOccurrence,
    reset,
  };
});
