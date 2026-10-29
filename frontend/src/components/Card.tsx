import React from 'react';
import { parseCard } from '../lib/cards';

export type CardSize = 'lg' | 'sm';

interface CardProps {
  cardStr?: string; // e.g. "A♠", "T♥"
  back?: boolean;
  size?: CardSize;
  className?: string;
  style?: React.CSSProperties;
}

export const Card: React.FC<CardProps> = ({ cardStr, back, size = 'lg', className = '', style }) => {
  const base = `poker-card poker-card--${size} ${className}`;
  if (back) {
    return <div className={`${base} back`} style={style} aria-label="Закрытая карта" />;
  }
  const card = cardStr ? parseCard(cardStr) : null;
  if (!card) {
    return <div className={`${base} empty`} style={style} aria-hidden="true" />;
  }
  // U+FE0E asks for the plain glyph: iOS otherwise may draw the suit as a wider emoji.
  const suit = `${card.suit}︎`;
  return (
    <div className={`${base} ${card.isRed ? 'red' : 'black'}`} style={style} aria-label={`${card.rank}${card.suit}`}>
      <span className="card-corner card-corner--top">
        {card.rank}
        <br />
        {suit}
      </span>
      <span className="card-center">{suit}</span>
      <span className="card-corner card-corner--bottom">
        {card.rank}
        <br />
        {suit}
      </span>
    </div>
  );
};
