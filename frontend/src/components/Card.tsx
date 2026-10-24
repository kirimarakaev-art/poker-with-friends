import React from 'react';

interface CardProps {
  cardStr?: string; // e.g., "A♠", "10♥", "2♦"
  back?: boolean;
}

export const Card: React.FC<CardProps> = ({ cardStr, back }) => {
  if (!cardStr && !back) {
    return <div className="poker-card empty" />;
  }

  if (back || !cardStr || cardStr.length < 2) {
    return <div className="poker-card back" />;
  }

  const suit = cardStr.slice(-1); // Last char is suit (♠, ♥, ♦, ♣)
  let rank = cardStr.slice(0, -1); // Everything before is rank (2..10, J, Q, K, A)
  if (rank === 'T') {
    rank = '10';
  }

  const isRed = suit === '♥' || suit === '♦';
  const colorClass = isRed ? 'red' : 'black';

  return (
    <div className={`poker-card ${colorClass}`}>
      <div className="card-top">
        <div>{rank}</div>
        <div>{suit}</div>
      </div>
      <div className="card-center">{suit}</div>
      <div className="card-bottom">
        <div>{rank}</div>
        <div>{suit}</div>
      </div>
    </div>
  );
};
