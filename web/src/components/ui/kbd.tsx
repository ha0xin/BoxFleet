import * as React from "react";
import { cva } from "class-variance-authority";

const kbdVariants = cva(
  "text-gray-1000 text-sm bg-background-100 text-center space-x-1 inline-block rounded ml-1 shadow-border font-sans",
  {
    variants: {
      size: {
        small: "min-w-[20px] px-1",
        medium: "min-w-[24px] min-h-[24px] px-1.5"
      }
    },
    defaultVariants: { size: "medium" }
  }
);

interface KbdProps {
  meta?: boolean;
  shift?: boolean;
  alt?: boolean;
  ctrl?: boolean;
  small?: boolean;
  children?: React.ReactNode;
}

export function Kbd({ meta, shift, alt, ctrl, small, children }: KbdProps) {
  return (
    <kbd className={kbdVariants({ size: small ? "small" : "medium" })}>
      {meta ? <span>⌘</span> : null}
      {shift ? <span>⇧</span> : null}
      {alt ? <span>⌥</span> : null}
      {ctrl ? <span>⌃</span> : null}
      {children ? <span>{children}</span> : null}
    </kbd>
  );
}
