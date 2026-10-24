import React, { useState, useEffect } from 'react';
import { TableState } from '../types';
import { Card } from './Card';

interface TableProps {
  roomCode: string;
  tableState: TableState;
  onSendAction: (action: string, amount?: number) => void;
  onSendMessage: (text: string) => void;
  messages: { sender: string; text: string }[];
  errorMessage: string | null;
}

export const Table: React.FC<TableProps> = ({
  roomCode,
  tableState,
  onSendAction,
  onSendMessage,
  messages,
  errorMessage,
}) => {
  const options = tableState.options;
  const isMyTurn = tableState.toAct === tableState.you && options !== undefined;

  const minRaise = options?.minRaiseTo || (tableState.currentBet ? tableState.currentBet * 2 : 20);
  const maxRaise = options?.maxRaiseTo || 1000;
  const myPlayer = tableState.players[tableState.you];

  const [raiseAmount, setRaiseAmount] = useState<number>(minRaise);
  const [chatText, setChatText] = useState('');
  const [timeLeft, setTimeLeft] = useState<number | null>(null);

  useEffect(() => {
    if (options) {
      setRaiseAmount(options.minRaiseTo);
    }
  }, [tableState.hand, tableState.street, isMyTurn]);

  // Turn timer countdown
  useEffect(() => {
    if (!tableState.deadline || tableState.toAct === -1) {
      setTimeLeft(null);
      return;
    }

    const interval = setInterval(() => {
      const remaining = Math.max(0, Math.floor((tableState.deadline! - Date.now()) / 1000));
      setTimeLeft(remaining);
      if (remaining <= 0) {
        clearInterval(interval);
      }
    }, 250);

    return () => clearInterval(interval);
  }, [tableState.deadline, tableState.toAct]);

  const getSeatStyle = (index: number, totalSeats: number) => {
    const angle = (index / totalSeats) * 2 * Math.PI - Math.PI / 2;
    const rx = 42;
    const ry = 38;
    const x = 50 + rx * Math.cos(angle);
    const y = 50 + ry * Math.sin(angle);
    return {
      left: `${x}%`,
      top: `${y}%`,
      transform: 'translate(-50%, -50%)',
    };
  };

  const handleChatSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    if (!chatText.trim()) return;
    onSendMessage(chatText.trim());
    setChatText('');
  };

  return (
    <div className="table-view">
      <div className="table-header">
        <div style={{ display: 'flex', alignItems: 'center', gap: '16px' }}>
          <div><strong>Комната:</strong> {roomCode}</div>
          <button
            onClick={() => {
              const inviteUrl = `${window.location.origin}${window.location.pathname}?room=${roomCode}`;
              navigator.clipboard.writeText(inviteUrl);
              alert('Ссылка-приглашение скопирована!');
            }}
            style={{ background: 'var(--gold)', color: '#000', border: 'none', padding: '4px 10px', borderRadius: '6px', fontSize: '13px', fontWeight: 'bold', cursor: 'pointer' }}
          >
            📋 Копировать приглашение
          </button>
        </div>
        <div>
          <strong>Улица:</strong> {tableState.street} | <strong>Раздача:</strong> {tableState.hand}
        </div>
        {timeLeft !== null && (
          <div style={{ color: timeLeft < 5 ? '#e74c3c' : 'var(--gold)', fontWeight: 'bold' }}>
            ⏱ Время на ход: {timeLeft}с
          </div>
        )}
        <div>
          <strong>Ваш стек:</strong> {myPlayer ? myPlayer.stack : 0} 🪙
        </div>
      </div>

      {errorMessage && (
        <div style={{ background: 'rgba(231, 76, 60, 0.2)', border: '1px solid #e74c3c', padding: '8px 16px', borderRadius: '8px', marginBottom: '12px', textAlign: 'center', color: '#e74c3c' }}>
          ⚠️ {errorMessage}
        </div>
      )}

      <div className="table-felt">
        {/* Center: Pot & Community Cards */}
        <div className="table-center">
          <div className="pots-display">Банк: {tableState.pot} 🪙</div>
          <div className="community-cards">
            {Array.from({ length: 5 }).map((_, i) => {
              const cardStr = tableState.board[i];
              return <Card key={i} cardStr={cardStr} />;
            })}
          </div>
        </div>

        {/* Player Seats */}
        {tableState.players.map((player, idx) => {
          const style = getSeatStyle(idx, tableState.players.length);
          const isDealer = idx === tableState.button;
          const isTurn = idx === tableState.toAct;
          const isMe = idx === tableState.you;

          return (
            <div
              key={player.seat}
              className={`player-seat ${isTurn ? 'active-turn' : ''} ${
                player.folded ? 'folded' : ''
              }`}
              style={style}
            >
              {isDealer && <div className="dealer-btn" style={{ top: -10, right: -10 }}>D</div>}
              <div className="player-name">
                {player.name} {isMe ? '(Вы)' : ''} {!player.connected ? '🔌' : ''}
              </div>
              <div className="player-stack">{player.stack} 🪙</div>
              {player.bet > 0 && <div style={{ fontSize: '11px', color: '#f39c12' }}>Ставка: {player.bet}</div>}
              {player.allIn && <div style={{ fontSize: '11px', color: '#9b59b6', fontWeight: 'bold' }}>Олл-ин</div>}
              {!player.inHand && !player.folded && tableState.hand > 0 && (
                <div style={{ fontSize: '11px', color: '#95a5a6' }}>Ожидает раздачи</div>
              )}
              {player.won !== undefined && player.won > 0 && (
                <div style={{ fontSize: '12px', color: '#2ecc71', fontWeight: 'bold' }}>
                  +{player.won} 🪙 Выигрыш!
                </div>
              )}

              <div className="player-hole-cards">
                {player.cards && player.cards.length > 0 ? (
                  player.cards.map((c, ci) => <Card key={ci} cardStr={c} />)
                ) : (
                  // If cards not sent by server (hidden opponent cards)
                  player.inHand && !player.folded ? (
                    <>
                      <Card back />
                      <Card back />
                    </>
                  ) : null
                )}
              </div>
            </div>
          );
        })}
      </div>

      {/* Action Bar */}
      <div className="action-bar">
        <button
          className="action-btn btn-fold"
          disabled={!isMyTurn}
          onClick={() => onSendAction('fold')}
        >
          Фолд
        </button>

        <button
          className="action-btn btn-check"
          disabled={!isMyTurn || !options?.canCheck}
          onClick={() => onSendAction('check')}
        >
          Чек
        </button>

        <button
          className="action-btn btn-call"
          disabled={!isMyTurn || !options || options.callAmount === 0}
          onClick={() => onSendAction('call')}
        >
          Колл ({options?.callAmount || 0})
        </button>

        <div className="raise-controls" style={{ display: 'flex', flexDirection: 'column', gap: '4px' }}>
          <div style={{ display: 'flex', gap: '4px', alignItems: 'center' }}>
            <input
              type="number"
              className="raise-input"
              value={raiseAmount}
              onChange={(e) => setRaiseAmount(Number(e.target.value))}
              min={minRaise}
              max={maxRaise}
              disabled={!isMyTurn || !options?.canRaise}
            />
            <button
              className="action-btn btn-raise"
              disabled={!isMyTurn || !options?.canRaise}
              onClick={() => {
                const actName = tableState.currentBet === 0 ? 'bet' : 'raise';
                onSendAction(actName, Number(raiseAmount));
              }}
            >
              {tableState.currentBet === 0 ? 'Бет' : 'Рейз'} до
            </button>
          </div>
          {options?.canRaise && (
            <div style={{ display: 'flex', gap: '4px', justifyContent: 'center' }}>
              <button
                type="button"
                style={{ fontSize: '11px', padding: '2px 6px', background: '#34495e', color: '#fff', border: 'none', borderRadius: '4px', cursor: 'pointer' }}
                disabled={!isMyTurn}
                onClick={() => setRaiseAmount(options.minRaiseTo)}
              >
                мин
              </button>
              <button
                type="button"
                style={{ fontSize: '11px', padding: '2px 6px', background: '#34495e', color: '#fff', border: 'none', borderRadius: '4px', cursor: 'pointer' }}
                disabled={!isMyTurn}
                onClick={() => {
                  const halfPot = Math.min(options.maxRaiseTo, Math.max(options.minRaiseTo, Math.floor(tableState.pot / 2)));
                  setRaiseAmount(halfPot);
                }}
              >
                ½ банка
              </button>
              <button
                type="button"
                style={{ fontSize: '11px', padding: '2px 6px', background: '#34495e', color: '#fff', border: 'none', borderRadius: '4px', cursor: 'pointer' }}
                disabled={!isMyTurn}
                onClick={() => {
                  const potSize = Math.min(options.maxRaiseTo, Math.max(options.minRaiseTo, tableState.pot));
                  setRaiseAmount(potSize);
                }}
              >
                банк
              </button>
              <button
                type="button"
                style={{ fontSize: '11px', padding: '2px 6px', background: '#34495e', color: '#fff', border: 'none', borderRadius: '4px', cursor: 'pointer' }}
                disabled={!isMyTurn}
                onClick={() => setRaiseAmount(options.maxRaiseTo)}
              >
                макс
              </button>
            </div>
          )}
        </div>

        <button
          className="action-btn btn-allin"
          disabled={!isMyTurn || !options || (!options.canRaise && options.callAmount !== myPlayer?.stack)}
          onClick={() => onSendAction('allin')}
        >
          Олл-ин ({myPlayer?.stack || 0})
        </button>
      </div>

      {/* Chat */}
      <div className="chat-container">
        <div className="chat-messages">
          {messages.map((m, i) => (
            <div key={i} className="chat-msg">
              <span>{m.sender}:</span> {m.text}
            </div>
          ))}
        </div>
        <form onSubmit={handleChatSubmit} className="chat-input-row">
          <input
            type="text"
            className="form-control"
            style={{ padding: '8px 12px', fontSize: '14px' }}
            value={chatText}
            onChange={(e) => setChatText(e.target.value)}
            placeholder="Сообщение в чат (до 200 символов)..."
            maxLength={200}
          />
          <button type="submit" className="btn" style={{ width: '100px', padding: '8px' }}>
            Отправить
          </button>
        </form>
      </div>
    </div>
  );
};
