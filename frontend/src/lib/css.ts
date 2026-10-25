import React from 'react';

// cssVars passes CSS custom properties (like --dx) through React's style prop.
export function cssVars(vars: Record<string, string | number>): React.CSSProperties {
  return vars as React.CSSProperties;
}
