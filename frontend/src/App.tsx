import React, { useState, useEffect, useRef } from 'react';
import { Lobby } from './components/Lobby';
import { Table } from './components/Table';
import { TableState, ChatMessage } from './types';

export function App() {
  const [roomCode, setRoomCode] = useState<string | null>(null);
  const [token, setToken] = useState<string | null>(null);
  const [nickname, setNickname] = useState<string>('');

  const [tableState, setTableState] = useState<TableState | null>(null);
  const [messages, setMessages] = useState<ChatMessage[]>([]);
  const [errorMessage, setErrorMessage] = useState<string | null>(null);

  const wsRef = useRef<WebSocket | null>(null);

  const handleEnterRoom = (code: string, tkn: string, nick: string) => {
    setRoomCode(code);
    setToken(tkn);
    setNickname(nick);
  };

  useEffect(() => {
    if (!roomCode || !token) return;

    const protocol = location.protocol === 'https:' ? 'wss' : 'ws';
    const wsUrl = `${protocol}://${location.host}/ws?room=${roomCode}&token=${token}`;

    const ws = new WebSocket(wsUrl);
    wsRef.current = ws;

    ws.onopen = () => {
      setErrorMessage(null);
    };

    ws.onmessage = (event) => {
      try {
        const data = JSON.parse(event.data);
        if (data.type === 'state') {
          setTableState(data.state);
        } else if (data.type === 'chat') {
          setMessages(prev => [...prev, { sender: data.from || 'Игрок', text: data.text }]);
        } else if (data.type === 'error') {
          setErrorMessage(data.error || 'Ошибка хода');
          setTimeout(() => setErrorMessage(null), 4000);
        }
      } catch (err) {
        console.error('Failed to parse WS message:', err);
      }
    };

    ws.onerror = (e) => {
      console.error('WebSocket error:', e);
      setErrorMessage('Ошибка соединения с сервером');
    };

    ws.onclose = (e) => {
      if (e.code === 1008) {
        setErrorMessage('Токен устарел или недействителен. Войдите заново.');
        localStorage.removeItem(`poker_token_${roomCode}`);
      } else {
        setErrorMessage('Соединение с сервером потеряно');
      }
    };

    return () => {
      ws.close();
    };
  }, [roomCode, token]);

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

  return (
    <div style={{ display: 'flex', flexDirection: 'column', flex: 1 }}>
      {!roomCode || !token || !tableState ? (
        <Lobby onEnterRoom={handleEnterRoom} />
      ) : (
        <Table
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
