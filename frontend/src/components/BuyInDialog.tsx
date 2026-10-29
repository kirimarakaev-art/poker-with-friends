import React, { useState } from 'react';
import { RoomInfo } from '../types';
import { buyInBounds, clampBuyIn } from '../lib/rooms';

interface BuyInDialogProps {
  room: RoomInfo;
  balance: number;
  notice?: string;
  onJoin: (buyIn: number) => Promise<void>;
  onCancel: () => void;
}

// BuyInDialog asks how many chips to bring to the table.
export const BuyInDialog: React.FC<BuyInDialogProps> = ({ room, balance, notice, onJoin, onCancel }) => {
  const bounds = buyInBounds(room, balance);
  const [input, setInput] = useState(String(bounds.initial));
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);

  const amount = clampBuyIn(Number(input), bounds);
  const full = room.players >= room.maxPlayers;

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setInput(String(amount));
    setLoading(true);
    setError(null);
    try {
      await onJoin(amount);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Нет связи с сервером');
      setLoading(false);
    }
  };

  return (
    <div className="modal-backdrop" onClick={onCancel}>
      <form
        className="lobby-card buyin-card"
        role="dialog"
        aria-modal="true"
        aria-labelledby="buyin-title"
        onClick={e => e.stopPropagation()}
        onSubmit={submit}
      >
        <h1 id="buyin-title">Стол {room.code}</h1>
        {notice && <div className="form-notice">{notice}</div>}
        <dl className="buyin-facts">
          <dt>Блайнды</dt>
          <dd>{room.smallBlind}/{room.bigBlind}</dd>
          <dt>Бай-ин</dt>
          <dd>{room.minBuyIn}–{room.maxBuyIn}</dd>
          <dt>Игроков</dt>
          <dd>{room.players}/{room.maxPlayers}</dd>
          <dt>Ваш баланс</dt>
          <dd>{balance} 🪙</dd>
        </dl>

        {!bounds.canAfford ? (
          <div className="form-error">
            Не хватает фишек: нужно хотя бы {room.minBuyIn} 🪙.
            <br />
            Если на балансе меньше 1 000, раз в сутки в меню можно забрать бонус.
          </div>
        ) : (
          <div className="form-group">
            <label htmlFor="buyin-amount">Сколько фишек взять за стол</label>
            <input
              id="buyin-amount"
              type="number"
              className="form-control"
              min={bounds.min}
              max={bounds.max}
              step={1}
              value={input}
              onChange={e => setInput(e.target.value)}
              onBlur={() => setInput(String(amount))}
            />
            <input
              type="range"
              className="buyin-slider"
              aria-label="Бай-ин"
              min={bounds.min}
              max={bounds.max}
              step={1}
              value={amount}
              onChange={e => setInput(e.target.value)}
              disabled={bounds.min === bounds.max}
            />
          </div>
        )}

        {full && <div className="form-error">За столом нет мест</div>}
        {error && <div className="form-error">{error}</div>}

        <button className="btn" type="submit" disabled={!bounds.canAfford || full || loading}>
          {loading ? 'Садимся…' : bounds.canAfford ? `Сесть с ${amount} 🪙` : 'Не хватает фишек'}
        </button>
        <button className="btn btn-secondary" type="button" onClick={onCancel}>
          Отмена
        </button>
      </form>
    </div>
  );
};
