import * as React from "react";
import { cva } from "class-variance-authority";

import { cn } from "@/lib/utils";

const gaugeVariants = cva("flex justify-center items-center relative", {
  variants: {
    size: {
      tiny: "[--circle-size-d:20px] [--stroke-width:15] [--text-size:0px] [--text-weight:0]",
      sm: "[--circle-size-d:32px] [--stroke-width:10] [--text-size:11px] [--text-weight:500]",
      md: "[--circle-size-d:64px] [--stroke-width:10] [--text-size:18px] [--text-weight:500]",
      lg: "[--circle-size-d:128px] [--stroke-width:10] [--text-size:32px] [--text-weight:600]"
    }
  },
  defaultVariants: { size: "md" }
});

const sizes = { tiny: 20, sm: 32, md: 64, lg: 128 } as const;

interface GaugeProps {
  size?: keyof typeof sizes;
  value: number;
  showValue?: boolean;
  tone?: "auto" | "green" | "amber" | "red" | "blue" | "teal" | "purple" | "pink";
}

const toneVar: Record<Exclude<GaugeProps["tone"], "auto" | undefined>, string> = {
  green: "var(--ds-green-700)",
  amber: "var(--ds-amber-700)",
  red: "var(--ds-red-800)",
  blue: "var(--ds-blue-700)",
  teal: "var(--ds-teal-700)",
  purple: "var(--ds-purple-700)",
  pink: "var(--ds-pink-700)"
};

export function Gauge({ size = "md", value, showValue, tone = "auto" }: GaugeProps) {
  const v = Math.max(0, Math.min(100, value));
  const primary =
    tone === "auto"
      ? v >= 68
        ? "hsl(var(--ds-green-700))"
        : v >= 34
          ? "hsl(var(--ds-amber-700))"
          : "hsl(var(--ds-red-800))"
      : `hsl(${toneVar[tone]})`;

  return (
    <div
      className={gaugeVariants({ size })}
      style={
        {
          "--circle-size": "100px",
          "--circumference": "282.7433388230814px",
          "--percent-to-px": "2.827433388230814px",
          "--gap-percent": "5",
          "--offset-factor": "0",
          "--secondary-color": "var(--ds-gray-alpha-400)",
          "--primary-color": primary
        } as React.CSSProperties
      }
    >
      <svg fill="none" viewBox="0 0 100 100" height={sizes[size]} width={sizes[size]} strokeWidth="2">
        <circle
          cx="50"
          cy="50"
          r="45"
          strokeWidth="10"
          strokeDashoffset="0"
          strokeLinecap="round"
          strokeLinejoin="round"
          className={cn(
            "stroke-[var(--secondary-color)] transition-all duration-1000 ease-in-out",
            "[stroke-dasharray:calc(var(--stroke-percent)_*_var(--percent-to-px))_var(--circumference)]",
            "[transform:rotate(calc(1turn_-_90deg_-(var(--gap-percent)_*_var(--percent-to-deg)_*_var(--offset-factor-secondary))))_scaleY(-1)]",
            "[transform-origin:calc(var(--circle-size)_/_2)_calc(var(--circle-size)_/_2)]"
          )}
          style={
            {
              opacity: 1,
              "--stroke-percent": Math.max(0, 100 - v - 10),
              "--percent-to-deg": "3.6deg",
              "--offset-factor-secondary": "calc(1 - var(--offset-factor))"
            } as React.CSSProperties
          }
        />
        <circle
          cx="50"
          cy="50"
          r="45"
          strokeWidth="10"
          strokeDashoffset="0"
          strokeLinecap="round"
          strokeLinejoin="round"
          className={cn(
            "stroke-[var(--primary-color)] transition-all duration-1000 ease-in-out",
            "[stroke-dasharray:calc(var(--stroke-percent)_*_var(--percent-to-px))_var(--circumference)]",
            "[transform:rotate(calc(-90deg_+_var(--gap-percent)_*_var(--offset-factor)_*_var(--percent-to-deg)))]",
            "[transform-origin:calc(var(--circle-size)_/_2)_calc(var(--circle-size)_/_2)]"
          )}
          style={
            {
              opacity: 1,
              "--stroke-percent": v,
              "--percent-to-deg": "3.6deg"
            } as React.CSSProperties
          }
        />
      </svg>
      {showValue && size !== "tiny" ? (
        <div className="absolute text-center">
          <p className="[font-size:var(--text-size)] [font-weight:var(--text-weight)]">{v}</p>
        </div>
      ) : null}
    </div>
  );
}
