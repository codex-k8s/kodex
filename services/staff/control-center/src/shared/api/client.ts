import { client } from "@/shared/api/generated/openapi/client.gen";
import { runtimeConfig } from "@/shared/config/runtime";
import { currentLocale } from "@/shared/locale";
import { selectedProjectRef } from "@/shared/project-context";

const projectReferenceHeader = "X-Kodex-Project-ID";
let projectInterceptorConfigured = false;

export function configureApiClient(): void {
  client.setConfig({
    baseUrl: runtimeConfig().apiBaseUrl,
    credentials: "include",
  });
  if (projectInterceptorConfigured) return;
  client.interceptors.request.use((request) => {
    const localizedHeaders = new Headers(request.headers);
    localizedHeaders.set("Accept-Language", currentLocale());
    const match = new URL(request.url).pathname.match(
      /^\/api\/v1\/projects\/([^/]+)/,
    );
    const reference = selectedProjectRef();
    if (
      !reference ||
      !match ||
      decodeURIComponent(match[1] ?? "") !== reference
    )
      return new Request(request, { headers: localizedHeaders });
    localizedHeaders.set(projectReferenceHeader, reference);
    return new Request(request, { headers: localizedHeaders });
  });
  projectInterceptorConfigured = true;
}

export function requestSignal(parent?: AbortSignal): AbortSignal {
  const timeout = AbortSignal.timeout(runtimeConfig().requestTimeoutMs);
  return parent ? AbortSignal.any([parent, timeout]) : timeout;
}
