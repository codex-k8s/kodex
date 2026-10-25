import { createRenderer } from "vue";

// Создаёт новый renderer для проверки setup/lifecycle без DOM на каждый вызов.
// Host-операции не меняют дерево: nodes являются свежими пустыми объектами,
// parent/sibling отсутствуют. Такая оснастка не проверяет layout или DOM events.
export function createNoopRenderer() {
  return createRenderer<object, object>({
    insert() {},
    remove() {},
    patchProp() {},
    createElement: () => ({}),
    createText: () => ({}),
    createComment: () => ({}),
    setText() {},
    setElementText() {},
    parentNode: () => null,
    nextSibling: () => null,
  });
}
