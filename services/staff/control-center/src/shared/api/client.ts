import { client } from "@/shared/api/generated/openapi/client.gen";
import { runtimeConfig } from "@/shared/config/runtime";

export function configureApiClient(): void {
  client.setConfig({
    baseUrl: runtimeConfig().apiBaseUrl,
    credentials: "include",
  });
}

export function requestSignal(): AbortSignal {
  return AbortSignal.timeout(runtimeConfig().requestTimeoutMs);
}
