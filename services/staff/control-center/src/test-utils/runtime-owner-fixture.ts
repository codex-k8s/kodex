import { createPinia, setActivePinia } from "pinia";
import { usePlatformStore } from "@/features/platform/store";
import type { BootstrapState } from "@/shared/api/generated/openapi/types.gen";

export function initializeRuntimeOwnerFixture(organizationRef: string) {
  setActivePinia(createPinia());
  const platform = usePlatformStore();
  platform.bootstrap = {
    organizationRef,
    platformRole: "OWNER",
  } as BootstrapState;
  return platform;
}
