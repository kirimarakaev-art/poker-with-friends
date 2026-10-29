import React, { useEffect, useState } from 'react';
import { TableState } from '../types';
import { isWatching } from '../lib/seats';

interface ActionBarProps {
  tableState: TableState;
  onSendAction: (action: string, amount?: number) => void;
  onLeave: () => void;
}

export const ActionBar: React.FC<ActionBarProps> = ({ tableState, onSendAction, onLeave }) => {
  const me = tableState.players[tableState.you];
  const options = tableState.options;
  const isMyTurn = tableState.toAct === tableState.you && options !== undefined;
  const myStack = me?.stack ?? 0;
  const minTo = options?.minRaiseTo ?? 0;
  const maxTo = options?.maxRaiseTo ?? 0;

  const [raiseAmount, setRaiseAmount] = useState<number>(minTo);

  // Reset the amount for each new decision, but not on unrelated updates
  // (someone connecting, a chat message), so typing is never lost.
  useEffect(() => {
    if (isMyTurn) setRaiseAmount(minTo);
  }, [tableState.hand, tableState.street, isMyTurn]); // eslint-disable-line react-hooks/exhaustive-deps

  useEffect(() => {
    if (isMyTurn && (raiseAmount < minTo || raiseAmount > maxTo)) setRaiseAmount(minTo);
  }, [minTo, maxTo]); // eslint-disable-line react-hooks/exhaustive-deps

  const clamp = (n: number) => Math.min(maxTo, Math.max(minTo, Math.floor(n)));
  const isBet = tableState.currentBet === 0;
  const canAllIn = options !== undefined && (options.canRaise || options.callAmount === myStack);

  if (me && isWatching(me, tableState.street)) {
    return (
      <div className="action-bar action-bar--watch">
        <span>Фишки закончились — вы наблюдаете за игрой</span>
        <button className="action-btn btn-fold" onClick={onLeave}>
          Выйти из-за стола
        </button>
      </div>
    );
  }

  return (
    <div className="action-bar">
      <button className="action-btn btn-fold" disabled={!isMyTurn} onClick={() => onSendAction('fold')}>
        Фолд
      </button>
      <button className="action-btn btn-check" disabled={!isMyTurn || !options?.canCheck} onClick={() => onSendAction('check')}>
        Чек
      </button>
      <button
        className="action-btn btn-call"
        disabled={!isMyTurn || !options || options.callAmount === 0}
        onClick={() => onSendAction('call')}
      >
        Колл{options && options.callAmount > 0 ? ` ${options.callAmount}` : ''}
      </button>

      <div className="raise-controls">
        <div className="raise-row">
          <input
            type="number"
            className="raise-input"
            aria-label="Сумма ставки"
            value={isMyTurn && options?.canRaise ? raiseAmount : ''}
            placeholder="сумма"
            onChange={e => setRaiseAmount(Number(e.target.value))}
            min={minTo}
            max={maxTo}
            disabled={!isMyTurn || !options?.canRaise}
          />
          <button
            className="action-btn btn-raise"
            disabled={!isMyTurn || !options?.canRaise}
            onClick={() => onSendAction(isBet ? 'bet' : 'raise', raiseAmount)}
          >
            {isBet ? 'Бет' : 'Рейз'} до
          </button>
        </div>
        {isMyTurn && options?.canRaise && (
          <div className="raise-presets">
            <button type="button" onClick={() => setRaiseAmount(minTo)}>мин</button>
            <button type="button" onClick={() => setRaiseAmount(clamp(tableState.pot / 2))}>½ банка</button>
            <button type="button" onClick={() => setRaiseAmount(clamp(tableState.pot))}>банк</button>
            <button type="button" onClick={() => setRaiseAmount(maxTo)}>макс</button>
          </div>
        )}
      </div>

      <button className="action-btn btn-allin" disabled={!isMyTurn || !canAllIn} onClick={() => onSendAction('allin')}>
        Олл-ин {myStack}
      </button>
    </div>
  );
};
