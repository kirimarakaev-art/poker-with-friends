import React, { useState } from 'react';
import { clampBuyIn, rebuyBounds } from '../lib/rooms';

interface RebuyDialogProps {
  maxBuyIn: number;
  stack: number;
  balance: number;
  inHand: boolean; // the chips arrive when the hand ends
  onRebuy: (amount: number) => Promise<void>;
  onCancel: () => void;
}

// RebuyDialog asks how many chips to add to the stack from the balance.
export const RebuyDialog: React.FC<RebuyDialogProps> = ({ maxBuyIn, stack, balance, inHand, onRebuy, onCancel }) => {
  const bounds = rebuyBounds(maxBuyIn, stack, balance);
  const [input, setInput] = useState(String(bounds.initial));
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);

  const amount = clampBuyIn(Number(input), bounds);
  const full = stack >= maxBuyIn;

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setInput(String(amount));
    setLoading(true);
    setError(null);
    try {
      await onRebuy(amount);
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
        aria-labelledby="rebuy-title"
        onClick={e => e.stopPropagation()}
        onSubmit={submit}
      >
        <h1 id="rebuy-title">Докупить фишки</h1>
        <dl className="buyin-facts">
          <dt>Ваш стек</dt>
          <dd>{stack} 🪙</dd>
          <dt>Макс. бай-ин</dt>
          <dd>{maxBuyIn} 🪙</dd>
          <dt>Ваш баланс</dt>
          <dd>{balance} 🪙</dd>
        </dl>

        {full ? (
          <div className="form-error">Стек уже максимальный для этого стола</div>
        ) : !bounds.canAfford ? (
          <div className="form-error">
            На балансе нет фишек.
            <br />
            Если на балансе меньше 1 000, раз в сутки в меню можно забрать бонус.
          </div>
        ) : (
          <div className="form-group">
            <label htmlFor="rebuy-amount">Сколько добавить</label>
            <input
              id="rebuy-amount"
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
              aria-label="Докупка"
              min={bounds.min}
              max={bounds.max}
              step={1}
              value={amount}
              onChange={e => setInput(e.target.value)}
              disabled={bounds.min === bounds.max}
            />
          </div>
        )}

        {inHand && bounds.canAfford && (
          <div className="form-notice">Вы в раздаче: фишки добавятся, когда она закончится.</div>
        )}
        {error && <div className="form-error">{error}</div>}

        <button className="btn" type="submit" disabled={!bounds.canAfford || loading}>
          {loading ? 'Докупаем…' : `Докупить ${bounds.canAfford ? amount : ''} 🪙`}
        </button>
        <button className="btn btn-secondary" type="button" onClick={onCancel}>
          Отмена
        </button>
      </form>
    </div>
  );
};
