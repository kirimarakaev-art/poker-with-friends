import React from 'react';
import { Card } from './Card';
import { cssVars } from '../lib/css';

export interface Flight {
  id: number;
  kind: 'chips' | 'cards';
  from: { x: number; y: number }; // px inside the felt
  to: { x: number; y: number };
  label?: string;
  delayMs: number;
}

export const FLIGHT_MS = 400;

// Loose chips around the labelled stack: offset in px and a small delay.
const LOOSE_CHIPS = [
  { x: -16, y: 10, delayMs: 30 },
  { x: 14, y: -12, delayMs: 60 },
  { x: 6, y: 16, delayMs: 90 },
];

// FlightLayer draws short-lived chips and cards that travel between two
// points on the felt (bets to the pot, the pot to the winner, folded cards).
export const FlightLayer: React.FC<{ flights: Flight[] }> = ({ flights }) => (
  <div className="flight-layer" aria-hidden="true">
    {flights.map(f => (
      <div key={f.id} className="flight-anchor" style={{ left: f.to.x, top: f.to.y }}>
        {f.kind === 'chips' &&
          LOOSE_CHIPS.map(c => (
            <div
              key={c.delayMs}
              className="flight flight--loose"
              style={cssVars({
                '--dx': `${f.from.x - f.to.x + c.x}px`,
                '--dy': `${f.from.y - f.to.y + c.y}px`,
                animationDuration: `${FLIGHT_MS}ms`,
                animationDelay: `${f.delayMs + c.delayMs}ms`,
              })}
            >
              <span className="loose-chip" />
            </div>
          ))}
        <div
          className={`flight flight--${f.kind}`}
          style={cssVars({
            '--dx': `${f.from.x - f.to.x}px`,
            '--dy': `${f.from.y - f.to.y}px`,
            animationDuration: `${FLIGHT_MS}ms`,
            animationDelay: `${f.delayMs}ms`,
          })}
        >
          {f.kind === 'cards' ? (
            <>
              <Card back size="sm" />
              <Card back size="sm" />
            </>
          ) : (
            <span className="chip-stack">{f.label}</span>
          )}
        </div>
      </div>
    ))}
  </div>
);
