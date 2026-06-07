import * as React from "react";
import { cva, type VariantProps } from "class-variance-authority";
import { Check, Copy } from "lucide-react";

import { cn } from "@/lib/utils";

const snippetVariants = cva("max-w-full relative rounded-md border py-2.5 pl-3 pr-12", {
  variants: {
    variant: {
      default: "bg-gray-200 border-gray-alpha-500 text-gray-1000",
      dark: "bg-gray-1000 border-gray-1000 text-gray-100",
      success: "bg-blue-100 border-blue-400 text-blue-900",
      error: "bg-red-100 border-red-400 text-red-900",
      warning: "bg-amber-100 border-amber-400 text-amber-900"
    }
  },
  defaultVariants: { variant: "default" }
});

interface SnippetProps extends VariantProps<typeof snippetVariants> {
  text: string | string[];
  width?: string;
  onCopy?: () => void;
  prompt?: boolean;
  wrap?: boolean;
  className?: string;
}

export function Snippet({ text, width, prompt = false, wrap = false, onCopy, variant = "default", className }: SnippetProps) {
  const lines = Array.isArray(text) ? text : [text];
  const [showCopy, setShowCopy] = React.useState(true);

  function copy() {
    if (!showCopy) return;
    setShowCopy(false);
    void navigator.clipboard.writeText(lines.join("\n")).then(() => onCopy?.());
    setTimeout(() => setShowCopy(true), 1200);
  }

  return (
    <div className={cn(snippetVariants({ variant }), "overflow-hidden", className)} style={{ width }}>
      <div className={cn("min-w-0", !wrap && "overflow-x-auto")}>
        {lines.map((line, index) => (
          <pre
            key={index}
            className={cn(
              "m-0 text-[13px] leading-5 font-mono",
              wrap ? "whitespace-pre-wrap break-all" : "whitespace-pre",
              prompt && "before:select-none before:content-['$_']"
            )}
          >
            {line}
          </pre>
        ))}
      </div>
      <button
        type="button"
        onClick={copy}
        className="absolute right-1 top-1/2 flex h-8 w-8 -translate-y-1/2 items-center justify-center rounded hover:bg-gray-alpha-200"
        aria-label="Copy"
      >
        <div
          className={cn(
            "absolute duration-150 ease-out",
            showCopy ? "animate-copy-button-fadeOut" : "animate-copy-button-fadeIn"
          )}
        >
          <Check className="h-4 w-4" />
        </div>
        <div
          className={cn(
            "absolute duration-150 ease-out",
            showCopy ? "animate-copy-button-fadeIn" : "animate-copy-button-fadeOut"
          )}
        >
          <Copy className="h-4 w-4" />
        </div>
      </button>
    </div>
  );
}
