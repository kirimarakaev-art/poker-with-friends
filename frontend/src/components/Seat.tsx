import React from 'react';
import { PlayerState } from '../types';
import { Card } from './Card';
import { cssVars } from '../lib/css';

export const DEAL_STEP_MS = 70;

interface SeatProps {
  player: PlayerState;
  isMe: boolean;
  isTurn: boolean;
  secondsLeft: number | null; // only for the seat that is to act
  timeFraction: number; // 0..1 of the turn left
  showWaiting: boolean;
  watching: boolean; // out of chips, stays to watch
  position: { left: string; top: string };
  // Animations for this render; all optional.
  deal?: { dx: number; dy: number; order: number; seats: number };
  reveal?: boolean;
  // Compact layout shows the bet and the dealer button on the plate.
  plateBet?: number;
  plateDealer?: boolean;
}

export const Seat: React.FC<SeatProps> = ({
  player,
  isMe,
  isTurn,
  secondsLeft,
  timeFraction,
  showWaiting,
  watching,
  position,
  deal,
  reveal,
  plateBet,
  plateDealer,
}) => {
  const size = isMe ? 'lg' : 'sm';
  const cardAnim = (i: number) => {
    if (deal) {
      return {
        className: 'anim-deal',
        style: cssVars({
          '--dx': `${deal.dx}px`,
          '--dy': `${deal.dy}px`,
          animationDelay: `${(i * deal.seats + deal.order) * DEAL_STEP_MS}ms`,
        }),
      };
    }
    return reveal ? { className: 'anim-flip', style: cssVars({ animationDelay: `${i * 90}ms` }) } : {};
  };

  let cards: React.ReactNode = null;
  if (player.cards && player.cards.length > 0) {
    cards = player.cards.map((c, i) => <Card key={c} cardStr={c} size={size} {...cardAnim(i)} />);
  } else if (player.inHand && !player.folded) {
    cards = [0, 1].map(i => <Card key={`back-${i}`} back size={size} {...cardAnim(i)} />);
  }

  const classes = [
    'player-seat',
    isMe && 'player-seat--me',
    isTurn && 'active-turn',
    player.folded && 'folded',
    !player.connected && 'offline',
  ]
    .filter(Boolean)
    .join(' ');

  return (
    <div className={classes} style={position}>
      <div className="player-hole-cards">{cards}</div>
      <div className="player-plate">
        <div className="player-name" title={player.name}>
          {!player.connected && <span className="offline-dot" aria-label="Не в сети" />}
          {player.name}
          {isMe && <span className="you-tag">вы</span>}
        </div>
        <div className="player-stack">
          {plateDealer && <span className="plate-dealer">D</span>}
          {player.stack} 🪙
        </div>
        {plateBet !== undefined && plateBet > 0 && <div className="plate-bet">ставка {plateBet}</div>}
        {player.allIn && !watching && <div className="seat-badge seat-badge--allin">Олл-ин</div>}
        {showWaiting && <div className="seat-badge">Ждёт раздачи</div>}
        {watching && <div className="seat-badge seat-badge--watch">Наблюдает</div>}
        {isTurn && secondsLeft !== null && (
          <div className={`seat-timer ${secondsLeft < 5 ? 'urgent' : ''}`} aria-label={`Осталось ${secondsLeft} с`}>
            <div className="seat-timer-fill" style={{ transform: `scaleX(${timeFraction})` }} />
          </div>
        )}
      </div>
      {(player.won ?? 0) > 0 && <div className="seat-won">+{player.won}</div>}
    </div>
  );
};
