export const connectingSocketRetirementTimeoutMs = 5_000;

// WebKit пишет console.error, если штатный переход документа вызывает close()
// до завершения WebSocket handshake. Retired socket не становится активным:
// store снимает его identity до вызова helper, а open только завершает cleanup.
export function retireWebSocket(socket: WebSocket, reason: string): void {
  let timer: number | undefined;
  const clear = () => {
    if (timer !== undefined) window.clearTimeout(timer);
    timer = undefined;
    socket.removeEventListener("open", opened);
    socket.removeEventListener("close", clear);
  };
  const close = () => {
    if (
      socket.readyState === WebSocket.CONNECTING ||
      socket.readyState === WebSocket.OPEN
    )
      socket.close(1000, reason);
    clear();
  };
  const opened = () => close();

  if (socket.readyState !== WebSocket.CONNECTING) {
    close();
    return;
  }
  socket.addEventListener("open", opened, { once: true });
  socket.addEventListener("close", clear, { once: true });
  // Неразрешившийся handshake не удерживает retired socket бессрочно.
  // Timeout остаётся настоящим fail-closed close и не скрывается из console.
  timer = window.setTimeout(close, connectingSocketRetirementTimeoutMs);
}
