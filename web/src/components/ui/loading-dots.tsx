import * as React from "react";

interface LoadingDotsProps {
  size?: number;
  children?: React.ReactNode;
}

export function LoadingDots({ size = 2, children }: LoadingDotsProps) {
  return (
    <span
      className="inline-flex items-center"
      style={{ ["--loading-dots-size" as string]: `${size}px` } as React.CSSProperties}
    >
      {children ? <span className="mr-3 inline-block">{children}</span> : null}
      <span className="mx-[1px] inline-block h-[var(--loading-dots-size)] w-[var(--loading-dots-size)] animate-loading-dots-blink rounded-full bg-gray-900" />
      <span className="mx-[1px] inline-block h-[var(--loading-dots-size)] w-[var(--loading-dots-size)] animate-loading-dots-blink rounded-full bg-gray-900 [animation-delay:200ms]" />
      <span className="mx-[1px] inline-block h-[var(--loading-dots-size)] w-[var(--loading-dots-size)] animate-loading-dots-blink rounded-full bg-gray-900 [animation-delay:400ms]" />
    </span>
  );
}
