// Game API client. The session lives in an HttpOnly cookie that the browser
// sends by itself; the page never sees it. See docs/API.md.
import { RoomInfo, User } from '../types';
import { RoomSettings } from './rooms';
import { apiMessage } from './account';

// ApiError carries the HTTP status (0: no network) so callers can tell "log in again" apart.
export class ApiError extends Error {
  constructor(readonly status: number, message: string) {
    super(message);
  }
}

async function errorText(res: Response): Promise<string> {
  try {
    const data = await res.json();
    return typeof data?.error === 'string' ? data.error : '';
  } catch {
    return '';
  }
}

// request is the one fetch client for /api: cookie session, JSON in and out, ApiError with
// a text from message (apiMessage by default) on failure.
export async function request<T>(
  method: 'GET' | 'POST' | 'DELETE',
  path: string,
  body?: unknown,
  message: (status: number, error: string) => string = apiMessage,
): Promise<T> {
  const hasBody = method !== 'GET';
  let res: Response;
  try {
    res = await fetch(`/api${path}`, {
      method,
      credentials: 'same-origin',
      headers: hasBody ? { 'Content-Type': 'application/json' } : undefined,
      body: hasBody ? JSON.stringify(body ?? {}) : undefined,
    });
  } catch {
    throw new ApiError(0, message(0, ''));
  }
  if (!res.ok) throw new ApiError(res.status, message(res.status, await errorText(res)));
  return res.json() as Promise<T>;
}

const call = <T>(method: 'GET' | 'POST', path: string, body?: unknown) => request<T>(method, path, body);

export const api = {
  me: () => call<User>('GET', '/me'),
  login: (username: string, password: string) => call<User>('POST', '/auth/login', { username, password }),
  register: (username: string, password: string) => call<User>('POST', '/auth/register', { username, password }),
  logout: () => call<{ ok: boolean }>('POST', '/auth/logout'),
  bonus: () => call<User>('POST', '/me/bonus'),
  myRooms: () => call<{ rooms: RoomInfo[] }>('GET', '/me/rooms').then(r => r.rooms),
  publicRooms: () => call<{ rooms: RoomInfo[] }>('GET', '/rooms').then(r => r.rooms),
  room: (code: string) => call<RoomInfo>('GET', `/rooms/${encodeURIComponent(code)}`),
  createRoom: (settings: RoomSettings & { public: boolean }) => call<{ code: string }>('POST', '/rooms', settings),
  join: (code: string, buyIn: number) =>
    call<{ seat: number; balance: number }>('POST', `/rooms/${encodeURIComponent(code)}/join`, { buyIn }),
};

export const isUnauthorized = (err: unknown) => err instanceof ApiError && err.status === 401;
