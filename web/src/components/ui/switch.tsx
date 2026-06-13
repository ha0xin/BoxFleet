import * as React from "react";
import { Switch as KumoSwitch, type SwitchProps as KumoSwitchProps } from "@cloudflare/kumo/components/switch";

export const Switch = React.forwardRef<HTMLButtonElement, KumoSwitchProps>(
  ({ "aria-label": ariaLabel = "Toggle", size = "sm", ...props }, ref) => (
    <KumoSwitch ref={ref} aria-label={ariaLabel} size={size} {...props} />
  )
);
Switch.displayName = "Switch";
