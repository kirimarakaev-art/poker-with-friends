import React from 'react';
import { PlayerState } from '../types';
import { Card } from './Card';
import { cssVars } from '../lib/css';
import { avatarColor, avatarLetter } from '../lib/avatar';
import { useCountUp } from '../hooks/useCountUp';

export const DEAL_STEP_MS = 70;

const SPARK_ANGLES = [0, 45, 90, 135, 180, 225, 270, 315];

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
  newHandName?: boolean;
  celebrateDelayMs?: number; // just won: the sparks start after this delay
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
  newHandName,
  celebrateDelayMs,
  plateBet,
  plateDealer,
}) => {
  const size = isMe ? 'lg' : 'sm';
  const stack = useCountUp(player.stack);
  const won = player.won ?? 0;
  const celebrating = celebrateDelayMs !== undefined;
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
    won > 0 && 'player-seat--winner',
    celebrating && 'celebrating',
  ]
    .filter(Boolean)
    .join(' ');

  return (
    <div className={classes} style={{ ...position, ...cssVars({ '--win-delay': `${celebrateDelayMs ?? 0}ms` }) }}>
      {won > 0 && <div className="seat-glow" aria-hidden="true" />}
      <div className="player-hole-cards">{cards}</div>
      <div className="player-plate">
        <div className="seat-avatar" style={{ background: avatarColor(player.name) }} aria-hidden="true">
          {avatarLetter(player.name)}
          {isTurn && (
            <svg className={`seat-ring ${secondsLeft !== null && secondsLeft < 5 ? 'urgent' : ''}`} viewBox="0 0 44 44">
              <circle cx="22" cy="22" r="20" pathLength={1} strokeDasharray={`${timeFraction} 1`} />
            </svg>
          )}
        </div>
        <div className="seat-info">
          <div className="player-name" title={player.name}>
            {!player.connected && <span className="offline-dot" aria-label="Не в сети" />}
            {player.name}
            {isMe && <span className="you-tag">вы</span>}
          </div>
          <div className="player-stack">
            {plateDealer && <span className="plate-dealer">D</span>}
            {stack}
          </div>
          {plateBet !== undefined && plateBet > 0 && <div className="plate-bet">ставка {plateBet}</div>}
          {player.handName && (
            <div className={`seat-badge seat-badge--hand ${newHandName ? 'anim-hand' : ''}`}>{player.handName}</div>
          )}
          {player.allIn && !watching && <div className="seat-badge seat-badge--allin">Олл-ин</div>}
          {showWaiting && <div className="seat-badge">Ждёт раздачи</div>}
          {watching && <div className="seat-badge seat-badge--watch">Наблюдает</div>}
          {isTurn && secondsLeft !== null && (
            <div className={`seat-timer ${secondsLeft < 5 ? 'urgent' : ''}`} aria-label={`Осталось ${secondsLeft} с`}>
              <div className="seat-timer-fill" style={{ transform: `scaleX(${timeFraction})` }} />
            </div>
          )}
        </div>
      </div>
      {won > 0 && <div className="seat-won">+{won}</div>}
      {celebrating && (
        <div className="seat-sparks" aria-hidden="true">
          {SPARK_ANGLES.map(angle => (
            <i key={angle} style={cssVars({ '--a': `${angle}deg` })} />
          ))}
        </div>
      )}
    </div>
  );
};
