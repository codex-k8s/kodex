import { defineStore } from "pinia";
import { ref } from "vue";

export const usePwaUpdateStore = defineStore("pwa-update", () => {
  const registration = ref<ServiceWorkerRegistration | null>(null);
  const failed = ref(false);
  function updateFound(value: ServiceWorkerRegistration): void {
    registration.value = value;
    failed.value = false;
  }
  function registrationFailed(): void {
    registration.value = null;
    failed.value = true;
  }
  return { registration, failed, updateFound, registrationFailed };
});
