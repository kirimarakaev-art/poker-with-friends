import React, { useState, useEffect, useRef } from 'react';
import { Lobby } from './components/Lobby';
import { Table } from './components/Table';
import { TableState, ChatMessage } from './types';
import { diffStates, NO_EVENTS, TableEvents } from './lib/animations';
import { closeOutcome, reconnectDelay } from './lib/connection';

interface TableView {
  state: TableState;
  events: TableEvents;
  seq: number;
}

const tokenKey = (code: string) => `poker_token_${code}`;

export function App() {
  const [roomCode, setRoomCode] = useState<string | null>(null);
  const [token, setToken] = useState<string | null>(null);
  const [initialUrlRoom, setInitialUrlRoom] = useState<string | null>(null);
  const [lobbyError, setLobbyError] = useState<string | null>(null);

  const [view, setView] = useState<TableView | null>(null);
  const [messages, setMessages] = useState<ChatMessage[]>([]);
  const [errorMessage, setErrorMessage] = useState<string | null>(null);
  const [isReplaced, setIsReplaced] = useState<boolean>(false);
  const [reconnectKey, setReconnectKey] = useState<number>(0);

  const wsRef = useRef<WebSocket | null>(null);
  const reconnectAttemptsRef = useRef<number>(0);
  const reconnectTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const errorTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  // Parse room code from URL and auto-login if token exists in localStorage
  useEffect(() => {
    const params = new URLSearchParams(window.location.search);
    const room = params.get('room');
    if (room) {
      const code = room.toUpperCase();
      setInitialUrlRoom(code);
      const tkn = localStorage.getItem(tokenKey(code));
      if (tkn) {
        setRoomCode(code);
        setToken(tkn);
      }
    }
  }, []);

  const handleEnterRoom = (code: string, tkn: string) => {
    setRoomCode(code);
    setToken(tkn);
    setIsReplaced(false);
    setLobbyError(null);
    setMessages([]);
    window.history.replaceState({}, '', `/?room=${code}`);
  };

  // leaveRoom drops a room the server no longer accepts and returns to the lobby.
  const leaveRoom = (code: string, message: string, keepCode: boolean) => {
    localStorage.removeItem(tokenKey(code));
    setRoomCode(null);
    setToken(null);
    setView(null);
    setErrorMessage(null);
    setLobbyError(message);
    setInitialUrlRoom(keepCode ? code : null);
    window.history.replaceState({}, '', keepCode ? `/?room=${code}` : '/');
  };

  useEffect(() => {
    if (!roomCode || !token || isReplaced) return;

    const protocol = location.protocol === 'https:' ? 'wss' : 'ws';
    const wsUrl = `${protocol}://${location.host}/ws?room=${roomCode}&token=${token}`;

    const ws = new WebSocket(wsUrl);
    wsRef.current = ws;
    let firstState = true; // the first state after connecting is shown without animations
    let closedByUs = false;

    ws.onopen = () => {
      setErrorMessage(null);
      reconnectAttemptsRef.current = 0;
    };

    ws.onmessage = (event) => {
      try {
        const data = JSON.parse(event.data);
        if (data.type === 'state') {
          reconnectAttemptsRef.current = 0;
          setErrorMessage(null);
          const next: TableState = data.state;
          const fresh = firstState;
          firstState = false;
          setView(prev => ({
            state: next,
            events: fresh || !prev ? NO_EVENTS : diffStates(prev.state, next),
            seq: (prev?.seq ?? 0) + 1,
          }));
        } else if (data.type === 'chat') {
          setMessages(prev => [...prev, { sender: data.from || 'Игрок', text: data.text }]);
        } else if (data.type === 'error') {
          console.warn('Server error:', data.error);
          if (errorTimerRef.current) clearTimeout(errorTimerRef.current);
          setErrorMessage('Ход не принят');
          errorTimerRef.current = setTimeout(() => {
            setErrorMessage(null);
          }, 4000);
        }
      } catch (err) {
        console.error('Failed to parse WS message:', err);
      }
    };

    ws.onerror = (e) => {
      console.error('WebSocket error:', e);
    };

    ws.onclose = (e) => {
      if (closedByUs) return;
      switch (closeOutcome(e.code)) {
        case 'replaced':
          setIsReplaced(true);
          return;
        case 'left':
          leaveRoom(roomCode, 'Вы вышли из-за стола.', true);
          return;
        case 'token-invalid':
          leaveRoom(roomCode, 'Место за столом больше недействительно. Войдите заново.', true);
          return;
        case 'room-gone':
          leaveRoom(roomCode, 'Комната не найдена. Возможно, сервер перезапускали — создайте новую.', false);
          return;
      }
      setErrorMessage('Соединение с сервером потеряно. Переподключение...');
      const delay = reconnectDelay(reconnectAttemptsRef.current);
      reconnectAttemptsRef.current += 1;
      reconnectTimerRef.current = setTimeout(() => {
        setReconnectKey(prev => prev + 1);
      }, delay);
    };

    return () => {
      closedByUs = true;
      if (reconnectTimerRef.current) clearTimeout(reconnectTimerRef.current);
      if (errorTimerRef.current) clearTimeout(errorTimerRef.current);
      ws.close();
    };
  }, [roomCode, token, isReplaced, reconnectKey]); // eslint-disable-line react-hooks/exhaustive-deps

  const handlePlayHere = () => {
    setIsReplaced(false);
    reconnectAttemptsRef.current = 0;
    setReconnectKey(prev => prev + 1);
  };

  const handleSendAction = (action: string, amount?: number) => {
    if (!wsRef.current || wsRef.current.readyState !== WebSocket.OPEN) return;
    const payload: { type: 'act'; action: string; amount?: number } = { type: 'act', action };
    if (amount !== undefined) {
      payload.amount = amount;
    }
    wsRef.current.send(JSON.stringify(payload));
  };

  const handleSendMessage = (text: string) => {
    if (!wsRef.current || wsRef.current.readyState !== WebSocket.OPEN) return;
    wsRef.current.send(JSON.stringify({ type: 'chat', text }));
  };

  // The server answers by closing the connection with CLOSE_LEFT, now or when the hand ends.
  const handleLeave = () => {
    if (!wsRef.current || wsRef.current.readyState !== WebSocket.OPEN) return;
    wsRef.current.send(JSON.stringify({ type: 'leave' }));
  };

  if (isReplaced) {
    return (
      <div className="app-container">
        <div className="lobby-card">
          <h1>⚠️ Сессия прервана</h1>
          <p style={{ marginBottom: '24px', color: '#bdc3c7', fontSize: '15px' }}>
            Вы открыли игру в другом окне или на другом устройстве.
          </p>
          <button className="btn" onClick={handlePlayHere}>
            Играть здесь
          </button>
        </div>
      </div>
    );
  }

  return (
    <div style={{ display: 'flex', flexDirection: 'column', flex: 1 }}>
      {!roomCode || !token || !view ? (
        <Lobby
          key={`${initialUrlRoom ?? ''}-${lobbyError ?? ''}`}
          onEnterRoom={handleEnterRoom}
          initialRoomCode={initialUrlRoom || undefined}
          initialError={lobbyError || undefined}
        />
      ) : (
        <Table
          roomCode={roomCode}
          tableState={view.state}
          events={view.events}
          eventsSeq={view.seq}
          onSendAction={handleSendAction}
          onSendMessage={handleSendMessage}
          onLeave={handleLeave}
          messages={messages}
          errorMessage={errorMessage}
        />
      )}
    </div>
  );
}

export default App;
