import React, { useState } from 'react';

interface LobbyProps {
  onEnterRoom: (code: string, token: string, nickname: string) => void;
  initialRoomCode?: string;
  initialError?: string;
}

export const Lobby: React.FC<LobbyProps> = ({ onEnterRoom, initialRoomCode, initialError }) => {
  const [nickname, setNickname] = useState('');
  const [roomId, setRoomId] = useState(initialRoomCode || '');
  const [mode, setMode] = useState<'home' | 'create' | 'join'>(initialRoomCode ? 'join' : 'home');
  const [smallBlind, setSmallBlind] = useState('5');
  const [bigBlind, setBigBlind] = useState('10');
  const [stack, setStack] = useState('1000');
  const [error, setError] = useState<string | null>(initialError ?? null);
  const [loading, setLoading] = useState(false);

  const friendlyError = (msg: string) => {
    if (msg.includes('room not found')) return 'Комната не найдена';
    if (msg.includes('table is full')) return 'За столом нет мест';
    if (msg.includes('name must be')) return 'Имя должно быть от 1 до 20 символов';
    if (msg.includes('need') || msg.includes('blind') || msg.includes('stack')) return 'Неверные параметры комнаты: нужны 0 < малый блайнд ≤ большой блайнд ≤ стек ≤ 1 000 000';
    return msg;
  };

  const handleCreateSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!nickname.trim()) return;

    const sb = Number(smallBlind);
    const bb = Number(bigBlind);
    const stk = Number(stack);
    if (sb <= 0 || bb < sb || stk < bb || stk > 1000000) {
      setError('Неверные параметры комнаты: нужны 0 < малый блайнд ≤ большой блайнд ≤ стек ≤ 1 000 000');
      return;
    }

    setLoading(true);
    setError(null);

    try {
      // 1. Create room
      const res = await fetch('/api/rooms', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          smallBlind: sb,
          bigBlind: bb,
          stack: stk,
        }),
      });
      const data = await res.json();
      if (!res.ok) {
        throw new Error(data.error || 'Не удалось создать комнату');
      }
      const code = data.code;

      // 2. Join room
      const joinRes = await fetch(`/api/rooms/${code}/join`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ name: nickname.trim() }),
      });
      const joinData = await joinRes.json();
      if (!joinRes.ok) {
        throw new Error(joinData.error || 'Не удалось войти в комнату');
      }

      const token = joinData.token;
      localStorage.setItem(`poker_token_${code}`, token);
      onEnterRoom(code, token, nickname.trim());
    } catch (err: any) {
      setError(friendlyError(err.message));
    } finally {
      setLoading(false);
    }
  };

  const handleJoinSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!nickname.trim() || !roomId.trim()) return;
    setLoading(true);
    setError(null);

    const code = roomId.trim().toUpperCase();

    try {
      // Check if we already have a token saved
      let token = localStorage.getItem(`poker_token_${code}`);

      if (!token) {
        const joinRes = await fetch(`/api/rooms/${code}/join`, {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ name: nickname.trim() }),
        });
        const joinData = await joinRes.json();
        if (!joinRes.ok) {
          throw new Error(joinData.error || 'Не удалось войти в комнату');
        }
        token = joinData.token;
        localStorage.setItem(`poker_token_${code}`, token!);
      }

      onEnterRoom(code, token!, nickname.trim());
    } catch (err: any) {
      setError(friendlyError(err.message));
    } finally {
      setLoading(false);
    }
  };

  return (
    <div className="app-container">
      <div className="lobby-card">
        <h1>♠ Покер с друзьями ♥</h1>

        {error && (
          <div style={{ color: '#e74c3c', marginBottom: '16px', fontSize: '14px' }}>
            {error}
          </div>
        )}

        {mode === 'home' && (
          <div style={{ display: 'flex', flexDirection: 'column', gap: '16px' }}>
            <button className="btn" onClick={() => setMode('create')}>
              Создать комнату
            </button>
            <button className="btn btn-secondary" onClick={() => setMode('join')}>
              Войти по коду
            </button>
          </div>
        )}

        {mode === 'create' && (
          <form onSubmit={handleCreateSubmit}>
            <div className="form-group">
              <label>Ваш никнейм (до 20 символов)</label>
              <input
                type="text"
                className="form-control"
                value={nickname}
                onChange={(e) => setNickname(e.target.value)}
                placeholder="Игрок"
                required
                maxLength={20}
              />
            </div>
            <div className="form-group">
              <label>Малый блайнд</label>
              <input
                type="number"
                className="form-control"
                value={smallBlind}
                onChange={(e) => setSmallBlind(e.target.value)}
                min={1}
              />
            </div>
            <div className="form-group">
              <label>Большой блайнд</label>
              <input
                type="number"
                className="form-control"
                value={bigBlind}
                onChange={(e) => setBigBlind(e.target.value)}
                min={1}
              />
            </div>
            <div className="form-group">
              <label>Стартовый стек фишек</label>
              <input
                type="number"
                className="form-control"
                value={stack}
                onChange={(e) => setStack(e.target.value)}
                min={10}
              />
            </div>
            <button type="submit" className="btn" disabled={loading}>
              {loading ? 'Создание...' : 'Создать и войти'}
            </button>
            <button type="button" className="btn btn-secondary" onClick={() => setMode('home')}>
              Назад
            </button>
          </form>
        )}

        {mode === 'join' && (
          <form onSubmit={handleJoinSubmit}>
            <div className="form-group">
              <label>Ваш никнейм (до 20 символов)</label>
              <input
                type="text"
                className="form-control"
                value={nickname}
                onChange={(e) => setNickname(e.target.value)}
                placeholder="Игрок"
                required
                maxLength={20}
              />
            </div>
            <div className="form-group">
              <label>Код комнаты</label>
              <input
                type="text"
                className="form-control"
                value={roomId}
                onChange={(e) => setRoomId(e.target.value)}
                placeholder="ABC123"
                required
              />
            </div>
            <button type="submit" className="btn" disabled={loading}>
              {loading ? 'Вход...' : 'Войти за стол'}
            </button>
            <button type="button" className="btn btn-secondary" onClick={() => setMode('home')}>
              Назад
            </button>
          </form>
        )}
      </div>
    </div>
  );
};
