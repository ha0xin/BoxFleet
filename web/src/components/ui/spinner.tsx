import * as React from "react";

type SpinnerProps = { size?: number };

export function Spinner({ size }: SpinnerProps) {
  return (
    <div
      className="h-[var(--spinner-size)] w-[var(--spinner-size)] shrink-0"
      style={{ ["--spinner-size" as string]: size ? `${size}px` : "20px" } as React.CSSProperties}
    >
      <div className="relative left-1/2 top-1/2 h-[var(--spinner-size)] w-[var(--spinner-size)]">
        {Array.from({ length: 12 }).map((_, i) => (
          <div
            key={i}
            className="absolute left-[-10%] top-[-3.9%] h-[8%] w-[24%] animate-spinner-spin rounded-[5px] bg-gray-700"
            style={{
              transform: `rotate(${i * 30}deg) translate(146%)`,
              animationDelay: `-${1.2 - (i + 1) * 0.1}s`
            }}
          />
        ))}
      </div>
    </div>
  );
}
