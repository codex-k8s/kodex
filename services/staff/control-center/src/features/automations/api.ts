import { requestSignal } from "@/shared/api/client";
import {
  commandSchedule as commandScheduleRequest,
  createSchedule as createScheduleRequest,
  deleteSchedule as deleteScheduleRequest,
  getSchedule,
  listScheduleRevisions,
  listScheduleRuns,
  listSchedules,
  previewSchedule,
  updateSchedule as updateScheduleRequest,
} from "@/shared/api/generated/openapi/sdk.gen";
import type {
  Schedule,
  ScheduleCommand,
  ScheduleInput,
  ScheduleRevisionPage,
  ScheduleRunOccurrencePage,
  SchedulePreviewInput,
} from "@/shared/api/generated/openapi/types.gen";
import { mutate, csrfToken, type MutationHeaders } from "@/shared/api/mutation";
import { AppProblem, asProblem, unwrap } from "@/shared/api/problem";

import {
  verifyScheduleCommandReadback,
  verifyScheduleDeleteReadback,
  verifyScheduleReadback,
} from "./model";

export interface SchedulePage {
  items: Schedule[];
  nextPageToken?: string;
}

const pageSize = 40;

export async function loadSchedulePreview(
  body: SchedulePreviewInput,
  signal: AbortSignal,
) {
  return (
    await unwrap(
      previewSchedule({
        body,
        headers: { "X-CSRF-Token": csrfToken() },
        signal: AbortSignal.any([signal, requestSignal()]),
      }),
    )
  ).data;
}

function mutationHeaders(headers: MutationHeaders): {
  "Idempotency-Key": string;
  "X-CSRF-Token": string;
} {
  return {
    "Idempotency-Key": headers["Idempotency-Key"],
    "X-CSRF-Token": headers["X-CSRF-Token"],
  };
}

function versionedHeaders(headers: MutationHeaders): {
  "Idempotency-Key": string;
  "If-Match": string;
  "X-CSRF-Token": string;
} {
  if (!headers["If-Match"])
    throw new Error("Schedule version header is unavailable");
  return { ...mutationHeaders(headers), "If-Match": headers["If-Match"] };
}

export async function loadSchedulePage(
  projectRef: string,
  query: string,
  pageToken?: string,
  signal: AbortSignal = requestSignal(),
): Promise<SchedulePage> {
  return (
    await unwrap(
      listSchedules({
        path: { projectRef },
        query: {
          pageSize,
          ...(query.trim() ? { query: query.trim() } : {}),
          ...(pageToken ? { pageToken } : {}),
        },
        signal,
      }),
    )
  ).data;
}

export async function readSchedule(
  scheduleRef: string,
  signal: AbortSignal = requestSignal(),
): Promise<Schedule> {
  return (
    await unwrap(
      getSchedule({ path: { scheduleRef }, signal: requestSignal(signal) }),
    )
  ).data;
}

export async function saveSchedule(
  projectRef: string,
  input: ScheduleInput,
  current?: Schedule,
): Promise<Schedule> {
  const mutation = current
    ? await mutate(
        (headers) =>
          updateScheduleRequest({
            path: { scheduleRef: current.ref },
            body: input,
            headers: versionedHeaders(headers),
            signal: requestSignal(),
          }),
        current.version,
      )
    : await mutate((headers) =>
        createScheduleRequest({
          path: { projectRef },
          body: input,
          headers: mutationHeaders(headers),
          signal: requestSignal(),
        }),
      );
  return verifyScheduleReadback(
    input,
    mutation.data,
    await readSchedule(mutation.data.ref),
  );
}

export async function commandSchedule(
  schedule: Schedule,
  action: ScheduleCommand["action"],
): Promise<Schedule> {
  const mutation = await mutate(
    (headers) =>
      commandScheduleRequest({
        path: { scheduleRef: schedule.ref },
        body: { action },
        headers: versionedHeaders(headers),
        signal: requestSignal(),
      }),
    schedule.version,
  );
  return verifyScheduleCommandReadback(
    mutation.data,
    await readSchedule(mutation.data.ref),
  );
}

export async function removeSchedule(schedule: Schedule): Promise<Schedule> {
  const mutation = await mutate(
    (headers) =>
      deleteScheduleRequest({
        path: { scheduleRef: schedule.ref },
        headers: versionedHeaders(headers),
        signal: requestSignal(),
      }),
    schedule.version,
  );
  const readback = await readSchedule(mutation.data.ref)
    .then((value) => ({ kind: "found" as const, schedule: value }))
    .catch((error: unknown) => {
      const problem = asProblem(error);
      if (problem.kind === "not-found") return { kind: "not-found" as const };
      throw problem;
    });
  return verifyScheduleDeleteReadback(mutation.data, readback);
}

export async function loadScheduleRevisionPage(
  scheduleRef: string,
  pageToken?: string,
  signal: AbortSignal = requestSignal(),
): Promise<ScheduleRevisionPage> {
  return (
    await unwrap(
      listScheduleRevisions({
        path: { scheduleRef },
        query: {
          pageSize,
          ...(pageToken ? { pageToken } : {}),
        },
        signal,
      }),
    )
  ).data;
}

export async function loadScheduleRunPage(
  scheduleRef: string,
  pageToken?: string,
  signal: AbortSignal = requestSignal(),
): Promise<ScheduleRunOccurrencePage> {
  const page = (
    await unwrap(
      listScheduleRuns({
        path: { scheduleRef },
        query: {
          pageSize,
          ...(pageToken ? { pageToken } : {}),
        },
        signal,
      }),
    )
  ).data;
  if (
    page.items.some(
      (occurrence) =>
        occurrence.scheduleRef !== scheduleRef ||
        !occurrence.scheduleRevisionRef ||
        !Number.isSafeInteger(occurrence.scheduleRevision) ||
        occurrence.scheduleRevision < 1 ||
        !occurrence.run.ref,
    )
  )
    throw new AppProblem({
      status: 502,
      code: "SCHEDULE_RUN_OCCURRENCE_MISMATCH",
      retryable: true,
      kind: "unavailable",
    });
  return page;
}
