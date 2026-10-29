import React, { useCallback, useEffect, useState } from 'react';
import { AuthScreen } from './components/AuthScreen';
import { Menu } from './components/Menu';
import { BuyInDialog } from './components/BuyInDialog';
import { TableExit, TableScreen } from './components/TableScreen';
import { RoomInfo, User } from './types';
import { api, isUnauthorized } from './lib/api';
import { normalizeRoomCode } from './lib/rooms';

interface BuyIn {
  room: RoomInfo;
  notice?: string;
}

// The room from an invite link (/?room=CODE), if any.
function roomFromUrl(): string | null {
  const room = new URLSearchParams(window.location.search).get('room');
  return room ? normalizeRoomCode(room) : null;
}

const SESSION_LOST = 'Сессия закончилась, войдите снова';

export function App() {
  const [user, setUser] = useState<User | null | undefined>(undefined); // undefined: still asking the server
  const [authNotice, setAuthNotice] = useState<string | undefined>(undefined);
  const [pendingRoom, setPendingRoom] = useState<string | null>(roomFromUrl);
  const [table, setTable] = useState<string | null>(null);
  const [buyIn, setBuyIn] = useState<BuyIn | null>(null);
  const [menuNotice, setMenuNotice] = useState<string | null>(null);

  // reportError sends the player to the login screen when the session is gone.
  const reportError = useCallback((err: unknown) => {
    if (!isUnauthorized(err)) return;
    setUser(null);
    setTable(null);
    setBuyIn(null);
    setAuthNotice(SESSION_LOST);
  }, []);

  const refreshUser = useCallback(async () => {
    try {
      setUser(await api.me());
    } catch (err) {
      reportError(err);
    }
  }, [reportError]);

  const openRoom = useCallback((room: RoomInfo, notice?: string) => {
    setMenuNotice(null);
    if (room.seated) {
      setBuyIn(null);
      setTable(room.code);
    } else {
      setBuyIn({ room, notice });
    }
  }, []);

  // goToRoom opens a room by code: the table if the player sits there, otherwise the buy-in.
  const goToRoom = useCallback(async (code: string, notice?: string) => {
    try {
      openRoom(await api.room(code), notice);
    } catch (err) {
      if (isUnauthorized(err)) {
        setPendingRoom(code);
        reportError(err);
        return;
      }
      setMenuNotice(err instanceof Error ? err.message : 'Нет связи с сервером');
    }
  }, [openRoom, reportError]);

  const handleAuth = (next: User) => {
    setUser(next);
    setAuthNotice(undefined);
    if (pendingRoom) goToRoom(pendingRoom);
    setPendingRoom(null);
  };

  // On start: am I logged in? Then follow the invite link, if there is one.
  useEffect(() => {
    let cancelled = false;
    api.me().then(
      me => {
        if (cancelled) return;
        setUser(me);
        const code = roomFromUrl();
        if (code) goToRoom(code);
        setPendingRoom(null);
      },
      err => {
        if (cancelled) return;
        setUser(null);
        if (!isUnauthorized(err)) setAuthNotice(err instanceof Error ? err.message : 'Нет связи с сервером');
      },
    );
    return () => {
      cancelled = true;
    };
  }, []); // eslint-disable-line react-hooks/exhaustive-deps

  // Keep the address in sync: /?room=CODE for a table (or its buy-in), / in the menu.
  const urlRoom = table ?? buyIn?.room.code ?? null;
  useEffect(() => {
    if (!user) return; // the login screen keeps the invite link
    const want = urlRoom ? `/?room=${urlRoom}` : '/';
    if (window.location.pathname + window.location.search !== want) {
      window.history.replaceState({}, '', want);
    }
  }, [user, urlRoom]);

  const handleExit = useCallback((reason: TableExit) => {
    const code = table;
    setTable(null);
    switch (reason) {
      case 'menu':
        setMenuNotice(null);
        break;
      case 'left':
        setMenuNotice('Вы вышли из-за стола, фишки вернулись на баланс');
        break;
      case 'room-gone':
        setMenuNotice('Комната не найдена');
        break;
      case 'not-seated':
        // Either the session is gone (refreshUser sends to login) or the seat was freed.
        if (code) goToRoom(code, 'Сядьте за стол заново');
        break;
    }
    refreshUser();
  }, [table, goToRoom, refreshUser]);

  const handleJoin = async (buyInAmount: number) => {
    if (!buyIn) return;
    const { room } = buyIn;
    try {
      const { balance } = await api.join(room.code, buyInAmount);
      setUser(u => (u ? { ...u, balance } : u));
      setBuyIn(null);
      setMenuNotice(null);
      setTable(room.code);
    } catch (err) {
      reportError(err);
      throw err;
    }
  };

  const handleLogout = async () => {
    try {
      await api.logout();
    } catch (err) {
      console.warn('Logout failed:', err);
    }
    setUser(null);
    setTable(null);
    setBuyIn(null);
    setMenuNotice(null);
    setAuthNotice(undefined);
    window.history.replaceState({}, '', '/');
  };

  if (user === undefined) {
    return (
      <div className="app-container">
        <div className="lobby-card">
          <h1>♠ Покер с друзьями ♥</h1>
          <p className="menu-empty">Загрузка…</p>
        </div>
      </div>
    );
  }

  if (user === null) {
    return <AuthScreen onAuth={handleAuth} notice={authNotice} />;
  }

  if (table) {
    return <TableScreen key={table} roomCode={table} onExit={handleExit} />;
  }

  return (
    <>
      <Menu
        user={user}
        notice={menuNotice}
        onUserChange={setUser}
        onOpenRoom={openRoom}
        onLogout={handleLogout}
        onError={reportError}
      />
      {buyIn && (
        <BuyInDialog
          key={buyIn.room.code}
          room={buyIn.room}
          balance={user.balance}
          notice={buyIn.notice}
          onJoin={handleJoin}
          onCancel={() => setBuyIn(null)}
        />
      )}
    </>
  );
}

export default App;
