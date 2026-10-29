import React, { useEffect, useRef, useState } from 'react';
import { Table } from './Table';
import { TableState, ChatMessage } from '../types';
import { diffStates, NO_EVENTS, TableEvents } from '../lib/animations';
import { closeOutcome, reconnectDelay } from '../lib/connection';

interface TableView {
  state: TableState;
  events: TableEvents;
  seq: number;
}

// Why the player is back in the menu: they asked, or the server closed the table for them.
export type TableExit = 'menu' | 'left' | 'not-seated' | 'room-gone';

interface TableScreenProps {
  roomCode: string;
  onExit: (reason: TableExit) => void;
}

// TableScreen keeps the WebSocket to one table. The player is known by the session cookie.
export const TableScreen: React.FC<TableScreenProps> = ({ roomCode, onExit }) => {
  const [view, setView] = useState<TableView | null>(null);
  const [messages, setMessages] = useState<ChatMessage[]>([]);
  const [errorMessage, setErrorMessage] = useState<string | null>(null);
  const [isReplaced, setIsReplaced] = useState<boolean>(false);
  const [reconnectKey, setReconnectKey] = useState<number>(0);

  const wsRef = useRef<WebSocket | null>(null);
  const reconnectAttemptsRef = useRef<number>(0);
  const reconnectTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const errorTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  // The socket handlers live across renders, so they read the latest onExit from a ref.
  const onExitRef = useRef(onExit);
  useEffect(() => {
    onExitRef.current = onExit;
  }, [onExit]);

  useEffect(() => {
    if (isReplaced) return;

    const protocol = location.protocol === 'https:' ? 'wss' : 'ws';
    const ws = new WebSocket(`${protocol}://${location.host}/ws?room=${encodeURIComponent(roomCode)}`);
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
      const outcome = closeOutcome(e.code);
      if (outcome === 'replaced') {
        setIsReplaced(true);
        return;
      }
      if (outcome !== 'reconnect') {
        onExitRef.current(outcome);
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
  }, [roomCode, isReplaced, reconnectKey]); // eslint-disable-line react-hooks/exhaustive-deps

  const handlePlayHere = () => {
    setIsReplaced(false);
    reconnectAttemptsRef.current = 0;
    setReconnectKey(prev => prev + 1);
  };

  const send = (msg: object) => {
    if (!wsRef.current || wsRef.current.readyState !== WebSocket.OPEN) return;
    wsRef.current.send(JSON.stringify(msg));
  };

  const handleSendAction = (action: string, amount?: number) => {
    send(amount === undefined ? { type: 'act', action } : { type: 'act', action, amount });
  };

  const handleSendMessage = (text: string) => send({ type: 'chat', text });

  // The server answers by closing the connection with CLOSE_LEFT, now or when the hand ends.
  const handleLeave = () => send({ type: 'leave' });

  // Back to the menu keeps the seat: unmounting closes the socket, the seat waits 5 minutes.
  const handleMenu = () => onExit('menu');

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
          <button className="btn btn-secondary" onClick={handleMenu}>
            ← Меню
          </button>
        </div>
      </div>
    );
  }

  if (!view) {
    return (
      <div className="app-container">
        <div className="lobby-card">
          <h1>Стол {roomCode}</h1>
          <p className="menu-empty">{errorMessage ?? 'Подключение…'}</p>
          <button className="btn btn-secondary" onClick={handleMenu}>
            ← Меню
          </button>
        </div>
      </div>
    );
  }

  return (
    <Table
      roomCode={roomCode}
      tableState={view.state}
      events={view.events}
      eventsSeq={view.seq}
      onSendAction={handleSendAction}
      onSendMessage={handleSendMessage}
      onLeave={handleLeave}
      onMenu={handleMenu}
      messages={messages}
      errorMessage={errorMessage}
    />
  );
};
