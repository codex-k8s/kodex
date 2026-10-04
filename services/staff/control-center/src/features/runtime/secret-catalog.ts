import type {
  RuntimeSecret,
  RuntimeSecretPage,
} from "@/shared/api/generated/openapi/types.gen";
import type { RuntimeResourceScope } from "./resource-scope";

export interface RuntimeSecretCatalog {
  loadPage(
    scope: RuntimeResourceScope,
    query: string,
    cursor: string | undefined,
    signal: AbortSignal,
    pageSize?: number,
  ): Promise<RuntimeSecretPage>;
  read(
    scope: RuntimeResourceScope,
    secretRef: string,
    signal: AbortSignal,
  ): Promise<RuntimeSecret>;
}
