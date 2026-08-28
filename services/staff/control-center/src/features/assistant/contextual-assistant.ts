import { readonly, ref } from "vue";

const open = ref(false);

export function useContextualAssistant() {
  return {
    open: readonly(open),
    show: () => {
      open.value = true;
    },
    hide: () => {
      open.value = false;
    },
  };
}
