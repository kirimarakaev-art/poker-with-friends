import React, { useCallback, useEffect, useState } from 'react';
import { RoomInfo, User } from '../types';
import { api } from '../lib/api';
import { normalizeRoomCode, validateRoomSettings } from '../lib/rooms';

const REFRESH_MS = 5000;

interface MenuProps {
  user: User;
  notice: string | null;
  onUserChange: (user: User) => void;
  onOpenRoom: (room: RoomInfo) => void;
  onLogout: () => void;
  onError: (err: unknown) => void; // reports failures; a lost session sends the player to the login screen
}

// Menu is the home screen after login: my tables, open tables, a new table, a code, the profile.
export const Menu: React.FC<MenuProps> = ({ user, notice, onUserChange, onOpenRoom, onLogout, onError }) => {
  const [myRooms, setMyRooms] = useState<RoomInfo[] | null>(null);
  const [openRooms, setOpenRooms] = useState<RoomInfo[] | null>(null);
  const [listError, setListError] = useState<string | null>(null);

  const refresh = useCallback(async () => {
    try {
      // The balance changes on its own too: a seat freed after 5 minutes away returns its chips.
      const [me, mine, open] = await Promise.all([api.me(), api.myRooms(), api.publicRooms()]);
      onUserChange(me);
      setMyRooms(mine);
      setOpenRooms(open);
      setListError(null);
    } catch (err) {
      setListError(err instanceof Error ? err.message : 'Нет связи с сервером');
      onError(err);
    }
  }, [onError, onUserChange]);

  useEffect(() => {
    refresh();
    const timer = setInterval(refresh, REFRESH_MS);
    return () => clearInterval(timer);
  }, [refresh]);

  return (
    <div className="menu">
      <header className="menu-header">
        <h1>♠ Покер с друзьями</h1>
        <div className="menu-user">
          {user.isAdmin && (
            <a className="menu-admin-link" href="/admin">
              Админка
            </a>
          )}
          <span className="menu-name">{user.username}</span>
          <span className="menu-balance">{user.balance} 🪙</span>
          <button className="leave-btn" onClick={onLogout}>
            Выйти
          </button>
        </div>
      </header>

      {notice && <div className="menu-notice">{notice}</div>}
      {listError && <div className="form-error">{listError}</div>}

      <div className="menu-grid">
        <section className="menu-section">
          <h2>Мои столы</h2>
          {myRooms === null ? (
            <p className="menu-empty">Загрузка…</p>
          ) : myRooms.length === 0 ? (
            <p className="menu-empty">Вы пока ни за каким столом не сидите.</p>
          ) : (
            <>
              <ul className="room-list">
                {myRooms.map(room => (
                  <RoomRow key={room.code} room={room} action="Вернуться" onClick={() => onOpenRoom(room)} />
                ))}
              </ul>
              <p className="form-hint">Место держится за вами, пока вы не вышли. Если не заходить к столу 5 минут, место освободится, а фишки вернутся на баланс.</p>
            </>
          )}
        </section>

        <section className="menu-section">
          <h2>Открытые столы</h2>
          {openRooms === null ? (
            <p className="menu-empty">Загрузка…</p>
          ) : openRooms.length === 0 ? (
            <p className="menu-empty">Открытых столов нет — создайте свой.</p>
          ) : (
            <ul className="room-list">
              {openRooms.map(room => (
                <RoomRow
                  key={room.code}
                  room={room}
                  action={room.seated ? 'Вернуться' : 'Сесть'}
                  disabled={!room.seated && room.players >= room.maxPlayers}
                  onClick={() => onOpenRoom(room)}
                />
              ))}
            </ul>
          )}
        </section>

        <CreateRoom onOpenRoom={onOpenRoom} />
        <JoinByCode onOpenRoom={onOpenRoom} />
        <Profile user={user} onUserChange={onUserChange} onError={onError} />
      </div>
    </div>
  );
};

interface RoomRowProps {
  room: RoomInfo;
  action: string;
  disabled?: boolean;
  onClick: () => void;
}

const RoomRow: React.FC<RoomRowProps> = ({ room, action, disabled, onClick }) => (
  <li className="room-row">
    <div className="room-row-info">
      <span className="room-row-blinds">{room.smallBlind}/{room.bigBlind}</span>
      <span className="room-row-sub">
        бай-ин {room.minBuyIn}–{room.maxBuyIn} · {room.players}/{room.maxPlayers} · {room.code}
      </span>
    </div>
    <button className="menu-btn" onClick={onClick} disabled={disabled}>
      {disabled ? 'Мест нет' : action}
    </button>
  </li>
);

const CreateRoom: React.FC<{ onOpenRoom: (room: RoomInfo) => void }> = ({ onOpenRoom }) => {
  const [smallBlind, setSmallBlind] = useState('5');
  const [bigBlind, setBigBlind] = useState('10');
  const [minBuyIn, setMinBuyIn] = useState('200');
  const [maxBuyIn, setMaxBuyIn] = useState('1000');
  const [isPublic, setIsPublic] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    const settings = {
      smallBlind: Number(smallBlind),
      bigBlind: Number(bigBlind),
      minBuyIn: Number(minBuyIn),
      maxBuyIn: Number(maxBuyIn),
    };
    const problem = validateRoomSettings(settings);
    if (problem) {
      setError(problem);
      return;
    }
    setLoading(true);
    setError(null);
    try {
      const { code } = await api.createRoom({ ...settings, public: isPublic });
      onOpenRoom(await api.room(code));
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Нет связи с сервером');
    } finally {
      setLoading(false);
    }
  };

  const field = (id: string, label: string, value: string, set: (v: string) => void) => (
    <div className="form-group">
      <label htmlFor={id}>{label}</label>
      <input id={id} type="number" className="form-control" min={1} step={1} value={value} onChange={e => set(e.target.value)} />
    </div>
  );

  return (
    <section className="menu-section">
      <h2>Создать стол</h2>
      <form onSubmit={submit}>
        <div className="form-pair">
          {field('create-sb', 'Малый блайнд', smallBlind, setSmallBlind)}
          {field('create-bb', 'Большой блайнд', bigBlind, setBigBlind)}
        </div>
        <div className="form-pair">
          {field('create-min', 'Бай-ин от', minBuyIn, setMinBuyIn)}
          {field('create-max', 'Бай-ин до', maxBuyIn, setMaxBuyIn)}
        </div>
        <label className="form-check">
          <input type="checkbox" checked={isPublic} onChange={e => setIsPublic(e.target.checked)} />
          Открытый (виден в списке)
        </label>
        {error && <div className="form-error">{error}</div>}
        <button className="btn" type="submit" disabled={loading}>
          {loading ? 'Создание…' : 'Создать'}
        </button>
      </form>
    </section>
  );
};

const JoinByCode: React.FC<{ onOpenRoom: (room: RoomInfo) => void }> = ({ onOpenRoom }) => {
  const [input, setInput] = useState('');
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    const code = normalizeRoomCode(input);
    if (!code) {
      setError('Код — 6 букв и цифр, например K7MQ2X');
      return;
    }
    setLoading(true);
    setError(null);
    try {
      onOpenRoom(await api.room(code));
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Нет связи с сервером');
    } finally {
      setLoading(false);
    }
  };

  return (
    <section className="menu-section">
      <h2>Войти по коду</h2>
      <form onSubmit={submit} className="code-form">
        <input
          type="text"
          className="form-control"
          aria-label="Код стола"
          value={input}
          onChange={e => setInput(e.target.value)}
          placeholder="K7MQ2X"
          maxLength={6}
          autoCapitalize="characters"
        />
        <button className="btn" type="submit" disabled={loading}>
          {loading ? '…' : 'Найти'}
        </button>
      </form>
      {error && <div className="form-error">{error}</div>}
    </section>
  );
};

interface ProfileProps {
  user: User;
  onUserChange: (user: User) => void;
  onError: (err: unknown) => void;
}

const Profile: React.FC<ProfileProps> = ({ user, onUserChange, onError }) => {
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);

  const claim = async () => {
    setLoading(true);
    setError(null);
    try {
      onUserChange(await api.bonus());
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Нет связи с сервером');
      onError(err);
    } finally {
      setLoading(false);
    }
  };

  return (
    <section className="menu-section">
      <h2>Профиль</h2>
      <dl className="buyin-facts">
        <dt>Баланс</dt>
        <dd>{user.balance} 🪙</dd>
        <dt>Сыграно раздач</dt>
        <dd>{user.stats.hands}</dd>
        <dt>Выиграно раздач</dt>
        <dd>{user.stats.handsWon}</dd>
      </dl>
      <button className="btn" onClick={claim} disabled={!user.bonusAvailable || loading}>
        Забрать бонус 1 000 🪙
      </button>
      {!user.bonusAvailable && (
        <p className="form-hint">Бонус дают, когда на балансе меньше 1 000, не чаще раза в сутки.</p>
      )}
      {error && <div className="form-error">{error}</div>}
    </section>
  );
};
