import { requestSignal } from "@/shared/api/client";
import {
  listAccessRoles,
  listAccessSubjects,
  listAgents,
  listIntegrationConnections,
  listProjects,
  listWorkflows,
} from "@/shared/api/generated/openapi/sdk.gen";
import type {
  AccessRole,
  AccessSubject,
  AccessSubjectKind,
} from "@/shared/api/generated/openapi/types.gen";
import { unwrap } from "@/shared/api/problem";
import type {
  AsyncEntityOption,
  AsyncEntityOptionPage,
} from "@/shared/ui/async-entity-picker";

export type AccessSubjectCapture = (items: AccessSubject[]) => void;
export type AccessRoleCapture = (items: AccessRole[]) => void;

// Пустые фильтр и cursor не отправляются; пробелы и явный pageSize сохраняются.
function pickerPageQuery(
  query: string,
  pageToken: string | undefined,
  pageSize: number,
) {
  return {
    ...(query ? { query } : {}),
    ...(pageToken ? { pageToken } : {}),
    pageSize,
  };
}

// Вызов SDK остаётся внутри единственного async-шага: синхронный отказ
// подготовки запроса возвращает rejected Promise, как у исходных loaders.
// Capture получает исходные items до проекции; cursor остаётся явным полем.
async function loadPickerOptions<T>(
  request: () => Parameters<
    typeof unwrap<{ items: T[]; nextPageToken?: string }>
  >[0],
  option: (item: T) => AsyncEntityOption,
  capture?: (items: T[]) => void,
): Promise<AsyncEntityOptionPage> {
  const page = (await unwrap(request())).data;
  capture?.(page.items);
  return {
    items: page.items.map(option),
    nextPageToken: page.nextPageToken,
  };
}

export function accessSubjectOptions(
  kind: AccessSubjectKind | undefined,
  query: string,
  pageToken: string | undefined,
  signal: AbortSignal,
  pageSize = 20,
  capture?: AccessSubjectCapture,
): Promise<AsyncEntityOptionPage> {
  return loadPickerOptions(
    () =>
      listAccessSubjects({
        query: {
          ...(query ? { query } : {}),
          ...(kind ? { kind } : {}),
          ...(pageToken ? { pageToken } : {}),
          pageSize,
        },
        signal: requestSignal(signal),
      }),
    (item) => ({
      ref: item.ref,
      title: item.displayName,
    }),
    capture,
  );
}

export function accessRoleOptions(
  query: string,
  pageToken: string | undefined,
  signal: AbortSignal,
  pageSize = 20,
  capture?: AccessRoleCapture,
  value: "ROLE" | "VERSION" = "ROLE",
): Promise<AsyncEntityOptionPage> {
  return loadPickerOptions(
    () =>
      listAccessRoles({
        query: {
          ...(query ? { query } : {}),
          ...(pageToken ? { pageToken } : {}),
          pageSize,
          includeArchived: false,
        },
        signal: requestSignal(signal),
      }),
    (item) => ({
      ref: value === "VERSION" ? item.currentVersion.ref : item.ref,
      title: item.currentVersion.name,
      description: item.currentVersion.description,
      meta: `v${String(item.currentVersion.revision)}`,
    }),
    capture,
  );
}

export function accessProjectOptions(
  query: string,
  pageToken: string | undefined,
  signal: AbortSignal,
  pageSize = 20,
): Promise<AsyncEntityOptionPage> {
  return loadPickerOptions(
    () =>
      listProjects({
        query: pickerPageQuery(query, pageToken, pageSize),
        signal: requestSignal(signal),
      }),
    (item) => ({
      ref: item.ref,
      title: item.name,
      description: item.purpose,
    }),
  );
}

export function accessAgentOptions(
  projectRef: string,
  query: string,
  pageToken: string | undefined,
  signal: AbortSignal,
  pageSize = 20,
): Promise<AsyncEntityOptionPage> {
  if (!projectRef) return Promise.resolve({ items: [] });
  return loadPickerOptions(
    () =>
      listAgents({
        path: { projectRef },
        query: pickerPageQuery(query, pageToken, pageSize),
        signal: requestSignal(signal),
      }),
    (item) => ({
      ref: item.ref,
      title: item.name,
      description: item.roleDescription,
    }),
  );
}

export function accessWorkflowOptions(
  projectRef: string,
  query: string,
  pageToken: string | undefined,
  signal: AbortSignal,
  pageSize = 20,
): Promise<AsyncEntityOptionPage> {
  if (!projectRef) return Promise.resolve({ items: [] });
  return loadPickerOptions(
    () =>
      listWorkflows({
        path: { projectRef },
        query: pickerPageQuery(query, pageToken, pageSize),
        signal: requestSignal(signal),
      }),
    (item) => ({
      ref: item.ref,
      title: item.name,
      description: item.purpose,
    }),
  );
}

export function accessIntegrationOptions(
  query: string,
  pageToken: string | undefined,
  signal: AbortSignal,
  pageSize = 20,
): Promise<AsyncEntityOptionPage> {
  return loadPickerOptions(
    () =>
      listIntegrationConnections({
        query: pickerPageQuery(query, pageToken, pageSize),
        signal: requestSignal(signal),
      }),
    (item) => ({
      ref: item.ref,
      title: item.name,
      description: item.definitionKey,
    }),
  );
}
