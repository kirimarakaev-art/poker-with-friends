import React, { useState, useEffect, useRef } from 'react';
import { Lobby } from './components/Lobby';
import { Table } from './components/Table';
import { TableState, ChatMessage } from './types';

export function App() {
  const [roomCode, setRoomCode] = useState<string | null>(null);
  const [token, setToken] = useState<string | null>(null);
  const [nickname, setNickname] = useState<string>('');
  const [initialUrlRoom, setInitialUrlRoom] = useState<string | null>(null);

  const [tableState, setTableState] = useState<TableState | null>(null);
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
      const tkn = localStorage.getItem(`poker_token_${code}`);
      if (tkn) {
        setRoomCode(code);
        setToken(tkn);
      }
    }
  }, []);

  const handleEnterRoom = (code: string, tkn: string, nick: string) => {
    setRoomCode(code);
    setToken(tkn);
    setNickname(nick);
    setIsReplaced(false);
    window.history.replaceState({}, '', `/?room=${code}`);
  };

  useEffect(() => {
    if (!roomCode || !token || isReplaced) return;

    const protocol = location.protocol === 'https:' ? 'wss' : 'ws';
    const wsUrl = `${protocol}://${location.host}/ws?room=${roomCode}&token=${token}`;

    const ws = new WebSocket(wsUrl);
    wsRef.current = ws;

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
          setTableState(data.state);
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
      if (e.code === 4001) {
        setIsReplaced(true);
      } else if (e.code === 1008) {
        setErrorMessage('Токен устарел или недействителен. Войдите заново.');
        localStorage.removeItem(`poker_token_${roomCode}`);
      } else {
        setErrorMessage('Соединение с сервером потеряно. Переподключение...');
        const attempts = reconnectAttemptsRef.current;
        const delays = [1000, 2000, 5000, 10000];
        const delay = delays[Math.min(attempts, delays.length - 1)];
        reconnectAttemptsRef.current = attempts + 1;
        reconnectTimerRef.current = setTimeout(() => {
          setReconnectKey(prev => prev + 1);
        }, delay);
      }
    };

    return () => {
      if (reconnectTimerRef.current) clearTimeout(reconnectTimerRef.current);
      if (errorTimerRef.current) clearTimeout(errorTimerRef.current);
      ws.close();
    };
  }, [roomCode, token, isReplaced, reconnectKey]);

  const handlePlayHere = () => {
    setIsReplaced(false);
    reconnectAttemptsRef.current = 0;
    setReconnectKey(prev => prev + 1);
  };

  const handleSendAction = (action: string, amount?: number) => {
    if (!wsRef.current || wsRef.current.readyState !== WebSocket.OPEN) return;
    const payload: any = { type: 'act', action };
    if (amount !== undefined) {
      payload.amount = amount;
    }
    wsRef.current.send(JSON.stringify(payload));
  };

  const handleSendMessage = (text: string) => {
    if (!wsRef.current || wsRef.current.readyState !== WebSocket.OPEN) return;
    wsRef.current.send(JSON.stringify({ type: 'chat', text }));
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
      {!roomCode || !token || !tableState ? (
        <Lobby onEnterRoom={handleEnterRoom} initialRoomCode={initialUrlRoom || undefined} />
      ) : (
        <Table
          roomCode={roomCode}
          tableState={tableState}
          onSendAction={handleSendAction}
          onSendMessage={handleSendMessage}
          messages={messages}
          errorMessage={errorMessage}
        />
      )}
    </div>
  );
}

export default App;
