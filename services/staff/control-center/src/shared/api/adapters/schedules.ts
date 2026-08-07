import {
  createSchedule,
  deleteSchedule,
  listScheduleOccurrences,
  resolveScheduleRecovery,
  runScheduleNow,
  updateSchedule,
} from "@/shared/api/generated/openapi/sdk.gen";
import type {
  CreateSchedule,
  ResolveScheduleRecovery,
  Resource,
  ScheduleOccurrence,
  ScheduleOccurrencePage,
  UpdateSchedule,
} from "@/shared/api/generated/openapi/types.gen";
import { requestSignal } from "@/shared/api/client";
import { unwrap } from "@/shared/api/problem";
import { mutationHeaders } from "@/shared/lib/identity";

type MutationHeaders = {
  "X-CSRF-Token": string;
  "Idempotency-Key": string;
  "If-Match": string;
};

export async function createAutomation(
  body: CreateSchedule,
): Promise<Resource> {
  return (
    await unwrap(
      createSchedule({
        body,
        headers: mutationHeaders() as Omit<MutationHeaders, "If-Match">,
        signal: requestSignal(),
      }),
    )
  ).data;
}

export async function updateAutomation(
  resource: Resource,
  body: UpdateSchedule,
): Promise<Resource> {
  return (
    await unwrap(
      updateSchedule({
        body,
        path: { scheduleId: resource.id },
        headers: mutationHeaders(resource.version) as MutationHeaders,
        signal: requestSignal(),
      }),
    )
  ).data;
}

export async function deleteAutomation(resource: Resource): Promise<Resource> {
  return (
    await unwrap(
      deleteSchedule({
        path: { scheduleId: resource.id },
        headers: mutationHeaders(resource.version) as MutationHeaders,
        signal: requestSignal(),
      }),
    )
  ).data;
}

export async function runAutomationNow(
  resource: Resource,
): Promise<ScheduleOccurrence> {
  return (
    await unwrap(
      runScheduleNow({
        path: { scheduleId: resource.id },
        headers: mutationHeaders(resource.version) as MutationHeaders,
        signal: requestSignal(),
      }),
    )
  ).data;
}

export async function fetchScheduleOccurrences(
  scheduleId: string,
): Promise<ScheduleOccurrencePage> {
  return (
    await unwrap(
      listScheduleOccurrences({
        path: { scheduleId },
        query: { pageSize: 100 },
        signal: requestSignal(),
      }),
    )
  ).data;
}

export async function recoverScheduleOccurrence(
  schedule: Resource,
  occurrence: ScheduleOccurrence,
  body: ResolveScheduleRecovery,
): Promise<ScheduleOccurrence> {
  return (
    await unwrap(
      resolveScheduleRecovery({
        body,
        path: {
          scheduleId: schedule.id,
          occurrenceId: occurrence.occurrenceId,
        },
        headers: mutationHeaders(occurrence.version) as MutationHeaders,
        signal: requestSignal(),
      }),
    )
  ).data;
}
