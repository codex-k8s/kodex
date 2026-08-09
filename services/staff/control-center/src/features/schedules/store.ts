import { defineStore } from "pinia";
import { reactive } from "vue";

import {
  createOwnerSchedule,
  deleteOwnerSchedule,
  fetchOwnerSchedules,
  fetchScheduleOccurrences,
  fetchScheduleSelectors,
  recoverScheduleOccurrence,
  runOwnerScheduleNow,
  updateOwnerSchedule,
} from "@/shared/api/adapters/owner-control";
import {
  fetchResource,
  fetchResources,
  transitionMutableResource,
} from "@/shared/api/adapters/resources";
import {
  buildScheduleCommand,
  type ScheduleDraft,
} from "@/features/schedules/model";
import type {
  OwnerScheduleView,
  Resource,
  ScheduleOccurrence,
  ScheduleSelectorCatalog,
} from "@/shared/api/generated/openapi/types.gen";
import { createFeatureRuntime } from "@/shared/lib/feature-store";
import { remoteState, resetRemoteState } from "@/shared/lib/remote";

export const useSchedulesStore = defineStore("schedules", () => {
  const scheduleSelectors = reactive(
    remoteState<ScheduleSelectorCatalog | null>(null),
  );
  const schedules = reactive(remoteState<OwnerScheduleView[]>([]));
  const scheduleOccurrences = reactive(remoteState<ScheduleOccurrence[]>([]));
  const artifacts = reactive(remoteState<Resource[]>([]));
  const rooms = reactive(remoteState<Resource[]>([]));
  const runtime = createFeatureRuntime();
  const loadSchedules = () =>
    Promise.all([
      runtime.loadInto(
        scheduleSelectors,
        fetchScheduleSelectors,
        (value) => value === null,
      ),
      runtime.loadInto(
        schedules,
        async () => (await fetchOwnerSchedules()).items,
        (items) => items.length === 0,
      ),
      runtime.loadInto(
        artifacts,
        async () => (await fetchResources("ARTIFACT")).resources,
        (items) => items.length === 0,
      ),
      runtime.loadInto(
        rooms,
        async () => (await fetchResources("CHAT")).resources,
        (items) => items.length === 0,
      ),
    ]).then(() => undefined);
  const saveScheduleDraft = (
    value: OwnerScheduleView | null,
    draft: ScheduleDraft,
  ) => {
    const body = buildScheduleCommand(draft);
    return runtime.mutate(
      () =>
        value
          ? updateOwnerSchedule(value.scheduleRef, value.version, body)
          : createOwnerSchedule(body),
      loadSchedules,
      schedules,
    );
  };
  const runSchedule = (value: OwnerScheduleView) =>
    runtime.mutate(
      () => runOwnerScheduleNow(value.scheduleRef, value.version),
      loadSchedules,
      schedules,
    );
  const removeSchedule = (value: OwnerScheduleView) =>
    runtime.mutate(
      () => deleteOwnerSchedule(value.scheduleRef, value.version),
      loadSchedules,
      schedules,
    );
  const transitionSchedule = (
    value: OwnerScheduleView,
    targetState: "ACTIVE" | "PAUSED",
  ) =>
    runtime.mutate(
      async () => {
        const resource = await fetchResource(value.scheduleRef, "SCHEDULE");
        return transitionMutableResource(resource, {
          targetState,
          reasonCode: "OWNER_REQUEST",
        });
      },
      loadSchedules,
      schedules,
    );
  const loadScheduleOccurrences = (ref: string) =>
    runtime.loadInto(
      scheduleOccurrences,
      async () => (await fetchScheduleOccurrences(ref)).occurrences,
      (items) => items.length === 0,
    );
  const resolveScheduleOccurrence = (
    schedule: OwnerScheduleView,
    occurrence: ScheduleOccurrence,
    action: "REPAIR" | "CANCEL" | "SKIP",
    reasonCode: string,
  ) => {
    const recoveryEvidenceSha256 = occurrence.recoveryEvidenceSha256;
    if (!recoveryEvidenceSha256) return Promise.resolve(false);
    return runtime.mutate(
      () =>
        recoverScheduleOccurrence(
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
    runtime.invalidate();
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
    mutationProblem: runtime.mutationProblem,
    mutating: runtime.mutating,
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
