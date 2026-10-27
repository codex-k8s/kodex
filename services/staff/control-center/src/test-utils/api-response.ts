// Создаёт новый успешный SDK-ответ с пустым HTTP body и сохраняет identity data.
// Фабрика не сериализует DTO и не выполняет сетевой запрос.
export function apiResponse<T>(data: T) {
  return Promise.resolve({
    data,
    response: new Response(null, { status: 200 }),
  });
}
