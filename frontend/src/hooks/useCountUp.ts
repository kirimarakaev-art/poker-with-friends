import { useEffect, useRef, useState } from 'react';
import { countStep } from '../lib/animations';

const COUNT_MS = 300;

// useCountUp returns value, counting smoothly to each new one. Only the digits
// on screen lag behind, and never for longer than COUNT_MS.
export function useCountUp(value: number): number {
  const [shown, setShown] = useState(value);
  const shownRef = useRef(value);
  shownRef.current = shown;

  useEffect(() => {
    const from = shownRef.current;
    if (from === value) return;
    if (window.matchMedia('(prefers-reduced-motion: reduce)').matches) {
      setShown(value);
      return;
    }
    let frame = 0;
    const start = performance.now();
    const tick = (now: number) => {
      const t = (now - start) / COUNT_MS;
      setShown(countStep(from, value, t));
      if (t < 1) frame = requestAnimationFrame(tick);
    };
    frame = requestAnimationFrame(tick);
    return () => cancelAnimationFrame(frame);
  }, [value]);

  return shown;
}
