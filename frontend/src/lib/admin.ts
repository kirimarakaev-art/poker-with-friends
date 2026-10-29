// Admin API client. The session lives in an HttpOnly cookie, so the page
// never sees the token; see docs/API.md.

export interface AdminPlayer {
  seat: number;
  name: string;
  stack: number;
  bet: number;
  connected: boolean;
  inHand: boolean;
  leaving: boolean;
  pendingStack?: number;
}

export interface AdminRoom {
  code: string;
  smallBlind: number;
  bigBlind: number;
  hand: number;
  street: string;
  players: (AdminPlayer | null)[]; // null is a free seat
}

// AdminError carries the HTTP status so the page can tell "log in again" apart.
export class AdminError extends Error {
  constructor(readonly status: number, message: string) {
    super(message);
  }
}

// adminMessage turns an admin API failure into text for the page.
export function adminMessage(status: number): string {
  switch (status) {
    case 401:
      return 'Неверный пароль';
    case 404:
      return 'Комната или игрок уже не найдены';
    case 429:
      return 'Слишком много попыток. Подождите минуту';
    case 400:
      return 'Неверное значение: стек от 0 до 1 000 000';
  }
  return `Ошибка сервера (${status})`;
}

async function call<T>(method: string, path: string, body?: unknown): Promise<T> {
  const res = await fetch(`/api/admin${path}`, {
    method,
    credentials: 'same-origin',
    headers: body === undefined ? undefined : { 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  if (!res.ok) throw new AdminError(res.status, adminMessage(res.status));
  return res.json() as Promise<T>;
}

export const adminApi = {
  login: (password: string) => call<{ ok: boolean }>('POST', '/login', { password }),
  logout: () => call<{ ok: boolean }>('POST', '/logout', {}),
  rooms: () => call<{ rooms: AdminRoom[] }>('GET', '/rooms').then(r => r.rooms),
  kick: (code: string, seat: number) => call('POST', `/rooms/${code}/kick`, { seat }),
  setStack: (code: string, seat: number, stack: number) => call('POST', `/rooms/${code}/stack`, { seat, stack }),
  closeRoom: (code: string) => call('DELETE', `/rooms/${code}`),
};

// parseStack reads a stack typed by the admin; null if it is not a whole number in range.
export function parseStack(input: string): number | null {
  const s = input.trim();
  if (!/^\d+$/.test(s)) return null;
  const n = Number(s);
  return n <= 1_000_000 ? n : null;
}
