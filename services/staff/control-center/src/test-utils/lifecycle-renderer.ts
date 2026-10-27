import { createRenderer } from "vue";

// createLifecycleTestRenderer создаёт Vue renderer без DOM для mounted watchers.
// Компонент, предоставленные контексты и unmount принадлежат тесту.
export function createLifecycleTestRenderer() {
  return createRenderer<Record<string, never>, Record<string, never>>({
    patchProp() {},
    insert() {},
    remove() {},
    createElement: () => ({}),
    createText: () => ({}),
    createComment: () => ({}),
    setText() {},
    setElementText() {},
    parentNode: () => null,
    nextSibling: () => null,
  });
}
