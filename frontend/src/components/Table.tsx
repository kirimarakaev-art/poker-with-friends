import React, { useEffect, useRef, useState } from 'react';
import { ChatMessage, TableState } from '../types';
import { TableEvents } from '../lib/animations';
import { CENTER, clearDistance, HalfSize, isWatching, Point, seatPoint, sideStep, stepToward } from '../lib/seats';
import { cssVars } from '../lib/css';
import { useElementSize } from '../hooks/useElementSize';
import { useNow } from '../hooks/useNow';
import { useCountUp } from '../hooks/useCountUp';
import { Card } from './Card';
import { Seat } from './Seat';
import { ActionBar } from './ActionBar';
import { Chat } from './Chat';
import { Flight, FlightLayer, FLIGHT_MS } from './FlightLayer';

const TURN_MS = 30_000; // server default turn timeout, see docs/API.md
const BOARD_STEP_MS = 140;
const DEALER_SIDE_PX = 36; // the button sits beside the seat's chips
const CHIP_GAP_PX = 6;

// Half sizes of a seat and of a chip stack, matching index.css (measured).
const SIZES = {
  wide: { me: { hw: 80, hh: 82 }, other: { hw: 80, hh: 62 }, chips: { hw: 30, hh: 10 } },
  compact: { me: { hw: 42, hh: 60 }, other: { hw: 42, hh: 41 }, chips: { hw: 22, hh: 8 } },
};

const STREET_NAMES: Record<string, string> = {
  Waiting: 'Ожидание',
  Preflop: 'Префлоп',
  Flop: 'Флоп',
  Turn: 'Тёрн',
  River: 'Ривер',
  Showdown: 'Вскрытие',
};

interface TableProps {
  roomCode: string;
  tableState: TableState;
  events: TableEvents;
  eventsSeq: number;
  onSendAction: (action: string, amount?: number) => void;
  onSendMessage: (text: string) => void;
  onLeave: () => void;
  onRebuy: () => void;
  onMenu: () => void;
  messages: ChatMessage[];
  errorMessage: string | null;
}

export const Table: React.FC<TableProps> = ({
  roomCode,
  tableState,
  events,
  eventsSeq,
  onSendAction,
  onSendMessage,
  onLeave,
  onRebuy,
  onMenu,
  messages,
  errorMessage,
}) => {
  const feltRef = useRef<HTMLDivElement>(null);
  const felt = useElementSize(feltRef);
  const now = useNow(tableState.toAct >= 0);
  const [flights, setFlights] = useState<Flight[]>([]);
  const [copied, setCopied] = useState(false);
  const [chatOpen, setChatOpen] = useState(false);
  const [leaving, setLeaving] = useState(false);

  const pot = useCountUp(tableState.pot);
  const n = tableState.players.length;
  const px = (p: Point) => ({ x: p.x * felt.width, y: p.y * felt.height });
  const seatAt = (seat: number) => seatPoint(seat, n, tableState.you);
  const centerPx = px(CENTER);
  const potPx = px({ x: 0.5, y: 0.36 });
  // Chips sit just inside the seat, towards the centre, clear of the seat box.
  // On phones there is no room on the felt: bets and the button go on the plates.
  const compact = window.matchMedia('(max-width: 600px)').matches;
  const sizes = compact ? SIZES.compact : SIZES.wide;
  const betPx = (seat: number) => {
    const from = px(seatAt(seat));
    const box: HalfSize = seat === tableState.you ? sizes.me : sizes.other;
    return stepToward(from, centerPx, clearDistance(from, centerPx, box, sizes.chips, CHIP_GAP_PX));
  };

  const secondsLeft = tableState.deadline ? Math.max(0, Math.ceil((tableState.deadline - now) / 1000)) : null;
  const timeFraction = tableState.deadline ? Math.min(1, Math.max(0, (tableState.deadline - now) / TURN_MS)) : 0;
  const actor = tableState.toAct >= 0 ? tableState.players[tableState.toAct] : undefined;

  // Chips and cards that travel between seats and the pot.
  useEffect(() => {
    if (felt.width === 0 || window.matchMedia('(prefers-reduced-motion: reduce)').matches) return;
    const added: Flight[] = [];
    let id = eventsSeq * 100;
    for (const seat of events.foldedSeats) {
      added.push({ id: id++, kind: 'cards', from: px(seatAt(seat)), to: centerPx, delayMs: 0 });
    }
    for (const { seat, amount } of events.betsToPot) {
      added.push({ id: id++, kind: 'chips', from: betPx(seat), to: potPx, label: String(amount), delayMs: 0 });
    }
    const winDelay = events.betsToPot.length > 0 ? FLIGHT_MS : 0;
    for (const { seat, amount } of events.wins) {
      added.push({ id: id++, kind: 'chips', from: potPx, to: px(seatAt(seat)), label: `+${amount}`, delayMs: winDelay });
    }
    if (added.length === 0) return;
    setFlights(f => [...f, ...added]);
    const ids = new Set(added.map(f => f.id));
    const timer = setTimeout(() => setFlights(f => f.filter(x => !ids.has(x.id))), FLIGHT_MS * 2 + winDelay);
    return () => clearTimeout(timer);
  }, [eventsSeq]); // eslint-disable-line react-hooks/exhaustive-deps

  const copyInvite = async () => {
    const url = `${window.location.origin}${window.location.pathname}?room=${roomCode}`;
    try {
      await navigator.clipboard.writeText(url);
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    } catch {
      window.prompt('Скопируйте ссылку-приглашение:', url);
    }
  };

  // A player in the hand leaves when it ends; the server folds for them on their turn.
  const me = tableState.players[tableState.you];
  const inPlay = !!me && me.inHand && !me.folded && tableState.street !== 'Showdown';
  const leave = () => {
    if (inPlay && !window.confirm('Вы в раздаче: карты будут сброшены. Выйти из-за стола?')) return;
    setLeaving(true);
    onLeave();
  };

  const street = STREET_NAMES[tableState.street] ?? tableState.street;
  const status = actor ? `Ход: ${actor.name}${secondsLeft !== null ? ` · ${secondsLeft} с` : ''}` : street;
  // Chips and the dealer button are placed in pixels, so wait until the felt is measured.
  const measured = felt.width > 0;
  const dealerPx =
    measured && !compact && tableState.button >= 0
      ? sideStep(px(seatAt(tableState.button)), centerPx, betPx(tableState.button), DEALER_SIDE_PX)
      : null;
  const newBoard = new Set(events.newBoardCards);
  const potMoved = events.betsToPot.length > 0 || events.wins.length > 0;
  const winDelayMs = (events.betsToPot.length > 0 ? FLIGHT_MS : 0) + FLIGHT_MS;

  return (
    <div className={`table-view ${chatOpen ? 'chat-open' : ''}`}>
      <header className="table-header">
        <div className="room-info">
          <button
            className="menu-back-btn"
            onClick={onMenu}
            title="Место за столом сохранится. Если не вернуться за 5 минут, оно освободится, а фишки вернутся на баланс."
          >
            ← Меню
          </button>
          <span className="room-code">
            Комната <strong>{roomCode}</strong>
          </span>
          <button className="invite-btn" onClick={copyInvite}>
            {copied ? '✓ Скопировано' : 'Пригласить'}
          </button>
        </div>
        <div className={`turn-status ${secondsLeft !== null && secondsLeft < 5 ? 'urgent' : ''}`}>
          {tableState.hand > 0 && <span className="hand-no">Раздача {tableState.hand} · </span>}
          {status}
        </div>
        <div className="header-actions">
          <button className="chat-toggle" onClick={() => setChatOpen(o => !o)} aria-expanded={chatOpen}>
            Чат{messages.length > 0 ? ` (${messages.length})` : ''}
          </button>
          {me && !leaving && (
            <button className="rebuy-btn" onClick={onRebuy}>
              Докупить
            </button>
          )}
          <button className="leave-btn" onClick={leave} disabled={leaving}>
            {leaving ? 'Выход после раздачи…' : 'Выйти'}
          </button>
        </div>
      </header>

      {errorMessage && <div className="table-error">⚠️ {errorMessage}</div>}

      <main className="table-stage">
        <div className="table-felt" ref={feltRef}>
          <div className="felt-line" aria-hidden="true" />
          <div className="table-center">
            <div
              key={potMoved ? eventsSeq : 'pot'}
              className={`pots-display ${potMoved ? 'anim-pot' : ''}`}
              style={cssVars({ '--pot-delay': `${FLIGHT_MS}ms` })}
            >
              <span className="pot-chips" aria-hidden="true" />
              Банк {pot}
            </div>
            <div className="community-cards">
              {Array.from({ length: 5 }).map((_, i) => {
                const card = tableState.board[i];
                const animate = newBoard.has(i);
                const order = events.newBoardCards.indexOf(i);
                return (
                  <Card
                    key={`${tableState.hand}-${i}-${card ?? 'empty'}`}
                    cardStr={card}
                    className={animate ? 'anim-flip' : ''}
                    style={animate ? { animationDelay: `${order * BOARD_STEP_MS}ms` } : undefined}
                  />
                );
              })}
            </div>
          </div>

          {tableState.players.map((p, s) => {
            if (!p || p.bet <= 0 || !measured || compact) return null;
            const at = betPx(s);
            const from = px(seatAt(s));
            const animate = events.betsPlaced.some(b => b.seat === s);
            return (
              <div key={`bet-${s}-${p.bet}`} className="bet-anchor" style={{ left: at.x, top: at.y }}>
                <span
                  className={`chip-stack ${animate ? 'anim-chip-in' : ''}`}
                  style={cssVars({ '--dx': `${from.x - at.x}px`, '--dy': `${from.y - at.y}px` })}
                >
                  {p.bet}
                </span>
              </div>
            );
          })}

          {dealerPx && (
            <div className="dealer-btn" style={{ transform: `translate(${dealerPx.x}px, ${dealerPx.y}px) translate(-50%, -50%)` }}>
              D
            </div>
          )}

          {tableState.players.map((p, s) => {
            if (!p) return null;
            const at = seatAt(s);
            const watching = isWatching(p, tableState.street);
            const seatPx = px(at);
            return (
              <Seat
                key={p.seat}
                player={p}
                isMe={s === tableState.you}
                isTurn={s === tableState.toAct}
                secondsLeft={s === tableState.toAct ? secondsLeft : null}
                timeFraction={timeFraction}
                showWaiting={!p.inHand && tableState.hand > 0 && !watching}
                watching={watching}
                position={{ left: `${at.x * 100}%`, top: `${at.y * 100}%` }}
                deal={
                  events.newHand
                    ? { dx: centerPx.x - seatPx.x, dy: centerPx.y - seatPx.y, order: (s - tableState.button - 1 + n) % n, seats: n }
                    : undefined
                }
                reveal={events.revealedSeats.includes(s)}
                newHandName={events.newHandNames.includes(s)}
                celebrateDelayMs={events.wins.some(w => w.seat === s) ? winDelayMs : undefined}
                plateBet={compact ? p.bet : undefined}
                plateDealer={compact && s === tableState.button}
              />
            );
          })}

          <FlightLayer flights={flights} />
        </div>
      </main>

      <ActionBar tableState={tableState} onSendAction={onSendAction} onLeave={leave} />
      <Chat messages={messages} onSendMessage={onSendMessage} />
    </div>
  );
};
