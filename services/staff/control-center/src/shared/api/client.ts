import { client } from "@/shared/api/generated/openapi/client.gen";
import { runtimeConfig } from "@/shared/config/runtime";
import { currentLocale } from "@/shared/locale";
import { selectedProjectRef } from "@/shared/project-context";
import { ownerRequestSignal } from "./owner-lifetime";

import {
  documentFetch,
  installDocumentRequestLifetime,
  retainRequestSignalParents,
} from "./document-lifetime";

const projectReferenceHeader = "X-Kodex-Project-ID";
let projectInterceptorConfigured = false;

export function configureApiClient(): void {
  installDocumentRequestLifetime();
  client.setConfig({
    baseUrl: runtimeConfig().apiBaseUrl,
    credentials: "include",
    fetch: documentFetch,
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
      return retainRequestSignalParents(
        new Request(request, { headers: localizedHeaders }),
        request,
      );
    localizedHeaders.set(projectReferenceHeader, reference);
    return retainRequestSignalParents(
      new Request(request, { headers: localizedHeaders }),
      request,
    );
  });
  projectInterceptorConfigured = true;
}

export function requestSignal(parent?: AbortSignal): AbortSignal {
  const timeout = AbortSignal.timeout(runtimeConfig().requestTimeoutMs);
  return AbortSignal.any([
    ownerRequestSignal(),
    timeout,
    ...(parent ? [parent] : []),
  ]);
}
