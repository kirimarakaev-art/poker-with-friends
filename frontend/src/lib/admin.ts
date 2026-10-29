// Admin API client. The admin is an ordinary logged-in player with isAdmin, so
// requests go through the game's fetch client and its session cookie; see docs/API.md.
import { User } from '../types';
import { request } from './api';

export interface AdminPlayer {
  seat: number;
  name: string;
  stack: number;
  bet: number;
  connected: boolean;
  inHand: boolean;
  leaving: boolean;
}

export interface AdminRoom {
  code: string;
  smallBlind: number;
  bigBlind: number;
  hand: number;
  street: string;
  players: (AdminPlayer | null)[]; // null is a free seat
}

export const MAX_BALANCE = 1_000_000_000;

// adminMessage turns an admin API failure into text for the page; status 0 means no network.
export function adminMessage(status: number): string {
  switch (status) {
    case 0:
      return 'Нет связи с сервером';
    case 400:
      return 'Неверное значение: баланс от 0 до 1 000 000 000';
    case 401:
      return 'Войдите в игру под аккаунтом админа';
    case 403:
      return 'У этого аккаунта нет прав админа';
    case 404:
      return 'Комната, игрок или пользователь уже не найдены';
    case 429:
      return 'Слишком много запросов. Подождите минуту';
  }
  return `Ошибка сервера (${status})`;
}

const call = <T>(method: 'GET' | 'POST' | 'DELETE', path: string, body?: unknown) =>
  request<T>(method, `/admin${path}`, body, adminMessage);

export const adminApi = {
  rooms: () => call<{ rooms: AdminRoom[] }>('GET', '/rooms').then(r => r.rooms),
  kick: (code: string, seat: number) => call('POST', `/rooms/${encodeURIComponent(code)}/kick`, { seat }),
  closeRoom: (code: string) => call('DELETE', `/rooms/${encodeURIComponent(code)}`),
  users: () => call<{ users: User[] }>('GET', '/users').then(r => r.users),
  setBalance: (id: number, balance: number) => call<User>('POST', `/users/${id}/balance`, { balance }),
};

// parseBalance reads a balance typed by the admin ("1 500" is fine); null if it is not a whole number in range.
export function parseBalance(input: string): number | null {
  const s = input.replace(/\s/g, '');
  if (!/^\d+$/.test(s)) return null;
  const n = Number(s);
  return n <= MAX_BALANCE ? n : null;
}
