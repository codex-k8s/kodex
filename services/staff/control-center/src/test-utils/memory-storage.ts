// Изолированная синтетическая фикстура Storage для тестов; данные живут только в Map.
// Порядок ключей и отсутствие значения сохранены так же, как в исходных фикстурах.
export function memoryStorage(): Storage {
  const values = new Map<string, string>();
  return {
    get length() {
      return values.size;
    },
    key: (index) => [...values.keys()][index] ?? null,
    getItem: (key) => values.get(key) ?? null,
    setItem: (key, value) => {
      values.set(key, value);
    },
    removeItem: (key) => {
      values.delete(key);
    },
    clear: () => values.clear(),
  };
}
