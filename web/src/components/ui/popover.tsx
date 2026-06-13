import { Popover as KumoPopover } from "@cloudflare/kumo/components/popover";

export const Popover = KumoPopover;
export const PopoverTrigger = KumoPopover.Trigger;
export const PopoverContent = KumoPopover.Content;
export const PopoverAnchor = ({ children }: { children?: React.ReactNode }) => <>{children}</>;
