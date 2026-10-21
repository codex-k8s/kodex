// Общие HTTP-примитивы локальных одноразовых форм. Origin, peer identity,
// intent, срок действия и поглощение запроса остаются у каждого обработчика.
export function singleInputHeader(request, name, requireInput) {
  const values = [];
  for (let index = 0; index < request.rawHeaders.length; index += 2)
    if (request.rawHeaders[index].toLowerCase() === name)
      values.push(request.rawHeaders[index + 1]);
  requireInput(values.length === 1);
  return values[0];
}

export function setInputResponseHeaders(response, origin) {
  response.setHeader("Access-Control-Allow-Origin", origin);
  response.setHeader("Vary", "Origin");
  response.setHeader("Cache-Control", "no-store");
  response.setHeader("Pragma", "no-cache");
}

// Вызывается только после проверки формы preflight и владельца TCP-соединения.
export function completeInputPreflight(request, response) {
  response.setHeader("Access-Control-Allow-Methods", "POST");
  response.setHeader("Access-Control-Allow-Headers", "Content-Type");
  if (request.headers["access-control-request-private-network"] === "true")
    response.setHeader("Access-Control-Allow-Private-Network", "true");
  response.writeHead(204);
  response.end();
}

// Не разбирает intent и не читает реквизиты. Вызывающий проверяет точное тело
// и обнуляет возвращённый Buffer в своём finally, как до выделения примитива.
export async function readInputBody(request, maximumBody, requireInput) {
  const chunks = [];
  let size = 0;
  for await (const chunk of request) {
    size += chunk.length;
    requireInput(size <= maximumBody);
    chunks.push(chunk);
  }
  return { body: Buffer.concat(chunks), size };
}
