import { readonly, ref } from "vue";

const storageKey = "mattercodex.project-scope.v1";
const projectReferencePattern =
  /^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i;

function storedProjectReference(): string | null {
  const value = localStorage.getItem(storageKey);
  return value && projectReferencePattern.test(value) ? value : null;
}

const current = ref<string | null>(storedProjectReference());

export const selectedProjectReference = readonly(current);

export function projectReference(): string | null {
  return current.value;
}

export function setProjectReference(value: string | null): void {
  if (value !== null && !projectReferencePattern.test(value)) {
    throw new Error("Project reference is invalid");
  }
  current.value = value;
  if (value === null) localStorage.removeItem(storageKey);
  else localStorage.setItem(storageKey, value);
}

export function isProjectScopedRequest(request: Request): boolean {
  const pathname = new URL(request.url).pathname;
  return (
    !/^\/api\/v1\/projects(?:\/|$)/.test(pathname) &&
    pathname !== "/api/v1/session"
  );
}

export function realtimeProjectURL(raw: string): string {
  const reference = projectReference();
  if (!reference) throw new Error("Project reference is unavailable");
  const url = new URL(raw);
  url.searchParams.set("projectId", reference);
  return url.toString();
}
