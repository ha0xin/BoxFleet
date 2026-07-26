import { BarChart, LineChart } from "echarts/charts";
import {
  AriaComponent,
  AxisPointerComponent,
  BrushComponent,
  GridComponent,
  ToolboxComponent,
  TooltipComponent
} from "echarts/components";
import * as echarts from "echarts/core";
import { CanvasRenderer } from "echarts/renderers";

/**
 * The single ECharts registration point for the app. Kumo's chart components
 * take the core instance as a prop instead of importing it so the consumer owns
 * tree shaking; every module registered below is load-bearing for
 * `TimeseriesChart`:
 *
 * - `Brush` and `Toolbox` back `onTimeRangeChange`
 * - `Aria` backs `ariaDescription`
 * - `AxisPointer` backs the shadow pointer and `tooltipFollowCursor`
 *
 * A missing registration fails silently at runtime, so import `echarts` from
 * here rather than from `echarts/core` directly.
 */
echarts.use([
  BarChart,
  LineChart,
  GridComponent,
  TooltipComponent,
  AxisPointerComponent,
  AriaComponent,
  BrushComponent,
  ToolboxComponent,
  CanvasRenderer
]);

export { echarts };
