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

// FlightLayer draws short-lived chips and cards that travel between two
// points on the felt (bets to the pot, the pot to the winner, folded cards).
export const FlightLayer: React.FC<{ flights: Flight[] }> = ({ flights }) => (
  <div className="flight-layer" aria-hidden="true">
    {flights.map(f => (
      <div key={f.id} className="flight-anchor" style={{ left: f.to.x, top: f.to.y }}>
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
