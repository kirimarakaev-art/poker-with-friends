import React, { useCallback, useEffect, useState } from 'react';
import { AdminError, AdminPlayer, AdminRoom, adminApi, parseStack } from '../lib/admin';

const REFRESH_MS = 3000;

const STREETS: Record<string, string> = {
  Waiting: 'ожидание',
  Preflop: 'префлоп',
  Flop: 'флоп',
  Turn: 'тёрн',
  River: 'ривер',
  Showdown: 'вскрытие',
};

// Admin is the /admin page: a password form, then every room with its players.
export const Admin: React.FC = () => {
  const [loggedIn, setLoggedIn] = useState(false);
  const [rooms, setRooms] = useState<AdminRoom[]>([]);
  const [error, setError] = useState<string | null>(null);

  const report = useCallback((err: unknown) => {
    if (err instanceof AdminError && err.status === 401) {
      setLoggedIn(false);
      setError('Сессия закончилась, войдите снова');
      return;
    }
    setError(err instanceof Error ? err.message : 'Ошибка сети');
  }, []);

  const refresh = useCallback(async () => {
    try {
      setRooms(await adminApi.rooms());
      setLoggedIn(true);
    } catch (err) {
      if (err instanceof AdminError && err.status === 401) {
        setLoggedIn(false); // not logged in yet: just show the form
      } else {
        report(err);
      }
    }
  }, [report]);

  useEffect(() => {
    refresh();
  }, [refresh]);

  useEffect(() => {
    if (!loggedIn) return;
    const timer = setInterval(refresh, REFRESH_MS);
    return () => clearInterval(timer);
  }, [loggedIn, refresh]);

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

  if (!loggedIn) {
    return <AdminLogin error={error} onLogin={() => run(async () => undefined)} onError={setError} />;
  }

  return (
    <div className="admin">
      <header className="admin-header">
        <h1>Админ-панель</h1>
        <button className="btn btn-secondary admin-logout" onClick={() => run(async () => {
          await adminApi.logout();
          setLoggedIn(false);
          setRooms([]);
        })}>
          Выйти
        </button>
      </header>
      {error && <div className="admin-error">{error}</div>}
      {rooms.length === 0 && <p className="admin-empty">Комнат пока нет.</p>}
      {rooms.map(room => (
        <RoomCard key={room.code} room={room} run={run} />
      ))}
    </div>
  );
};

interface AdminLoginProps {
  error: string | null;
  onLogin: () => void;
  onError: (message: string) => void;
}

const AdminLogin: React.FC<AdminLoginProps> = ({ error, onLogin, onError }) => {
  const [password, setPassword] = useState('');
  const [loading, setLoading] = useState(false);

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setLoading(true);
    try {
      await adminApi.login(password);
      setPassword('');
      onLogin();
    } catch (err) {
      onError(err instanceof Error ? err.message : 'Ошибка сети');
    } finally {
      setLoading(false);
    }
  };

  return (
    <div className="app-container">
      <form className="lobby-card" onSubmit={submit}>
        <h1>Админ-панель</h1>
        {error && <div className="admin-error">{error}</div>}
        <div className="form-group">
          <label htmlFor="admin-password">Пароль</label>
          <input
            id="admin-password"
            type="password"
            className="form-control"
            value={password}
            onChange={e => setPassword(e.target.value)}
            autoComplete="current-password"
            required
          />
        </div>
        <button className="btn" type="submit" disabled={loading}>
          {loading ? 'Вход…' : 'Войти'}
        </button>
      </form>
    </div>
  );
};

interface RoomCardProps {
  room: AdminRoom;
  run: (action: () => Promise<unknown>) => void;
}

const RoomCard: React.FC<RoomCardProps> = ({ room, run }) => {
  const seated = room.players.filter((p): p is AdminPlayer => p !== null);

  const close = () => {
    const text = `Закрыть комнату ${room.code}? Все игроки вернутся в лобби, история раздач удалится.`;
    if (window.confirm(text)) run(() => adminApi.closeRoom(room.code));
  };
  const kick = (p: AdminPlayer) => {
    const text = p.inHand
      ? `Выкинуть ${p.name}? Он в раздаче: место освободится после неё.`
      : `Выкинуть ${p.name}?`;
    if (window.confirm(text)) run(() => adminApi.kick(room.code, p.seat));
  };
  const editStack = (p: AdminPlayer) => {
    const input = window.prompt(`Новый стек для ${p.name}:`, String(p.pendingStack ?? p.stack));
    if (input === null) return;
    const stack = parseStack(input);
    if (stack === null) {
      window.alert('Нужно целое число от 0 до 1 000 000');
      return;
    }
    run(() => adminApi.setStack(room.code, p.seat, stack));
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
                  {p.pendingStack !== undefined && <span className="admin-sub"> → {p.pendingStack} после раздачи</span>}
                </td>
                <td>{playerStatus(p)}</td>
                <td className="admin-actions">
                  <button className="admin-btn" onClick={() => editStack(p)}>
                    Стек
                  </button>
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
