import { client } from "@/shared/api/generated/openapi/client.gen";
import { runtimeConfig } from "@/shared/config/runtime";
import {
  isProjectScopedRequest,
  projectReference,
} from "@/shared/lib/project-scope";

const projectReferenceHeader = "X-MatterCodex-Project-ID";
let projectInterceptorConfigured = false;

export function configureApiClient(): void {
  client.setConfig({
    baseUrl: runtimeConfig().apiBaseUrl,
    credentials: "include",
  });
  if (projectInterceptorConfigured) return;
  client.interceptors.request.use((request) => {
    const reference = projectReference();
    if (!reference || !isProjectScopedRequest(request)) return request;
    const headers = new Headers(request.headers);
    headers.set(projectReferenceHeader, reference);
    return new Request(request, { headers });
  });
  projectInterceptorConfigured = true;
}

export function requestSignal(): AbortSignal {
  return AbortSignal.timeout(runtimeConfig().requestTimeoutMs);
}
