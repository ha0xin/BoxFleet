import { ChartPalette } from "@cloudflare/kumo";

/**
 * Series colours are pinned to two slots of Kumo's categorical palette, chosen
 * by running a contrast/lightness/colour-vision check over every pairing rather
 * than by eye:
 *
 * - slot 0 `#4290F0` + slot 5 `#D37536` pass all checks in light *and* dark mode
 * - slot 1 (yellow) misses the lightness band and lands at 1.75:1 in light mode
 * - slot 3 (purple) misses the normal-vision separation floor against slot 0
 * - slots 2 (pink) and 4 (teal) miss the lightness band in dark mode
 *
 * Do not use slots 1-4 for a two-series chart. Chart marks are never sourced
 * from the UI tokens (`--color-kumo-danger` and friends): those are tuned for a
 * 16px badge, and a canvas cannot read CSS variables anyway.
 */
const PRIMARY_SLOT = 0;
const SECONDARY_SLOT = 5;

/** Ordered colours for a one- or two-series chart, primary first. */
export function seriesColors(isDark: boolean): [string, string] {
  return [
    ChartPalette.categorical(PRIMARY_SLOT, isDark),
    ChartPalette.categorical(SECONDARY_SLOT, isDark)
  ];
}

/** Colour for a single-series chart or a standalone legend swatch. */
export function primaryColor(isDark: boolean): string {
  return ChartPalette.categorical(PRIMARY_SLOT, isDark);
}
