import { computed, type WritableComputedRef } from "vue";

export function environmentDisplayField(
  read: () => string,
  write: (value: string) => void,
  localize: (value: string) => string,
): WritableComputedRef<string> {
  return computed({
    get: () => localize(read()),
    set: (value) => {
      if (value !== localize(read())) write(value);
    },
  });
}
