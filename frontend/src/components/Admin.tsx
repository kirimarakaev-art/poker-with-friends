import React, { useCallback, useEffect, useState } from 'react';
import { AdminPlayer, AdminRoom, adminApi, parseBalance } from '../lib/admin';
import { ApiError } from '../lib/api';
import { User } from '../types';

const REFRESH_MS = 3000;

const STREETS: Record<string, string> = {
  Waiting: 'ожидание',
  Preflop: 'префлоп',
  Flop: 'флоп',
  Turn: 'тёрн',
  River: 'ривер',
  Showdown: 'вскрытие',
};

type Access = 'loading' | 'ok' | 'login' | 'forbidden';

// Admin is the /admin page for a logged-in player with isAdmin: every room with its players, then every user.
export const Admin: React.FC = () => {
  const [access, setAccess] = useState<Access>('loading');
  const [rooms, setRooms] = useState<AdminRoom[]>([]);
  const [users, setUsers] = useState<User[]>([]);
  const [error, setError] = useState<string | null>(null);

  // report shows a failure; a lost session or missing rights replace the page with a card.
  const report = useCallback((err: unknown) => {
    if (err instanceof ApiError && err.status === 401) {
      setAccess('login');
      return;
    }
    if (err instanceof ApiError && err.status === 403) {
      setAccess('forbidden');
      return;
    }
    setError(err instanceof Error ? err.message : 'Нет связи с сервером');
  }, []);

  const refresh = useCallback(async () => {
    try {
      const [nextRooms, nextUsers] = await Promise.all([adminApi.rooms(), adminApi.users()]);
      setRooms(nextRooms);
      setUsers(nextUsers);
      setAccess('ok');
    } catch (err) {
      report(err);
    }
  }, [report]);

  useEffect(() => {
    refresh();
  }, [refresh]);

  useEffect(() => {
    if (access !== 'ok') return;
    const timer = setInterval(refresh, REFRESH_MS);
    return () => clearInterval(timer);
  }, [access, refresh]);

  // run performs an action and shows the result at once instead of waiting for the next refresh.
  const run = async (action: () => Promise<unknown>) => {
    setError(null);
    try {
      await action();
      await refresh();
    } catch (err) {
      report(err);
    }
  };

  if (access === 'loading') {
    return <AdminNotice text="Загрузка…" />;
  }
  if (access === 'login') {
    return <AdminNotice text="Войдите в игру под аккаунтом админа" link="Войти" />;
  }
  if (access === 'forbidden') {
    return <AdminNotice text="У этого аккаунта нет прав админа" link="В игру" />;
  }

  return (
    <div className="admin">
      <header className="admin-header">
        <h1>Админ-панель</h1>
        <a className="btn btn-secondary admin-back" href="/">
          ← В игру
        </a>
      </header>
      {error && <div className="admin-error">{error}</div>}
      <h2 className="admin-section-title">Комнаты</h2>
      {rooms.length === 0 && <p className="admin-empty">Комнат пока нет.</p>}
      {rooms.map(room => (
        <RoomCard key={room.code} room={room} run={run} />
      ))}
      <h2 className="admin-section-title">Пользователи</h2>
      <UserList users={users} run={run} />
    </div>
  );
};

// AdminNotice is the whole page while loading or when the player cannot use the admin page.
const AdminNotice: React.FC<{ text: string; link?: string }> = ({ text, link }) => (
  <div className="app-container">
    <div className="lobby-card">
      <h1>Админ-панель</h1>
      <p className="admin-notice">{text}</p>
      {link && (
        <a className="btn admin-notice-link" href="/">
          {link}
        </a>
      )}
    </div>
  </div>
);

interface RoomCardProps {
  room: AdminRoom;
  run: (action: () => Promise<unknown>) => void;
}

const RoomCard: React.FC<RoomCardProps> = ({ room, run }) => {
  const seated = room.players.filter((p): p is AdminPlayer => p !== null);

  const close = () => {
    const text = `Закрыть комнату ${room.code}? Фишки со стола вернутся игрокам на баланс, история раздач удалится.`;
    if (window.confirm(text)) run(() => adminApi.closeRoom(room.code));
  };
  const kick = (p: AdminPlayer) => {
    const text = p.inHand
      ? `Выкинуть ${p.name}? Он в раздаче: место освободится после неё, стек вернётся ему на баланс.`
      : `Выкинуть ${p.name}? Стек ${p.stack} вернётся ему на баланс.`;
    if (window.confirm(text)) run(() => adminApi.kick(room.code, p.seat));
  };

  return (
    <section className="admin-room" aria-label={`Комната ${room.code}`}>
      <div className="admin-room-head">
        <div>
          <strong className="admin-code">{room.code}</strong>
          <span className="admin-meta">
            блайнды {room.smallBlind}/{room.bigBlind} · раздача {room.hand} · {STREETS[room.street] ?? room.street}
          </span>
        </div>
        <button className="admin-btn admin-btn--danger" onClick={close}>
          Закрыть комнату
        </button>
      </div>
      {seated.length === 0 ? (
        <p className="admin-empty">За столом никого нет.</p>
      ) : (
        <table className="admin-table">
          <thead>
            <tr>
              <th>Место</th>
              <th>Игрок</th>
              <th>Стек</th>
              <th>Статус</th>
              <th aria-label="Действия" />
            </tr>
          </thead>
          <tbody>
            {seated.map(p => (
              <tr key={p.seat}>
                <td>{p.seat + 1}</td>
                <td className="admin-name">{p.name}</td>
                <td className="admin-num">
                  {p.stack}
                  {p.bet > 0 && <span className="admin-sub"> (в банке {p.bet})</span>}
                </td>
                <td>{playerStatus(p)}</td>
                <td className="admin-actions">
                  <button className="admin-btn admin-btn--danger" onClick={() => kick(p)} disabled={p.leaving}>
                    Выкинуть
                  </button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </section>
  );
};

function playerStatus(p: AdminPlayer): string {
  const parts = [p.connected ? '🟢 в сети' : '⚪ не в сети'];
  if (p.leaving) parts.push('уходит после раздачи');
  else if (p.inHand) parts.push('в раздаче');
  else if (p.stack === 0) parts.push('наблюдает');
  return parts.join(' · ');
}

interface UserListProps {
  users: User[];
  run: (action: () => Promise<unknown>) => void;
}

const UserList: React.FC<UserListProps> = ({ users, run }) => {
  const editBalance = (u: User) => {
    const input = window.prompt(`Новый баланс для ${u.username}:`, String(u.balance));
    if (input === null) return;
    const balance = parseBalance(input);
    if (balance === null) {
      window.alert('Нужно целое число от 0 до 1 000 000 000');
      return;
    }
    run(() => adminApi.setBalance(u.id, balance));
  };

  if (users.length === 0) return <p className="admin-empty">Пользователей пока нет.</p>;

  return (
    <section className="admin-room" aria-label="Пользователи">
      <table className="admin-table admin-users">
        <thead>
          <tr>
            <th>Логин</th>
            <th>Баланс</th>
            <th>Раздачи</th>
            <th aria-label="Действия" />
          </tr>
        </thead>
        <tbody>
          {users.map(u => (
            <tr key={u.id}>
              <td className="admin-name">
                {u.username}
                {u.isAdmin && <span className="admin-badge">админ</span>}
              </td>
              <td className="admin-num">{u.balance} 🪙</td>
              <td className="admin-num">
                {u.stats.hands} <span className="admin-sub">(выиграно {u.stats.handsWon})</span>
              </td>
              <td className="admin-actions">
                <button className="admin-btn" onClick={() => editBalance(u)}>
                  Баланс
                </button>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </section>
  );
};
