<script lang="ts">
  import type { MetricPoint } from "../lib/api";
  import { formatDateTimeShort, formatTime } from "../lib/format";
  import { dict, locale } from "../i18n";

  export let points: MetricPoint[] = [];
  export let names: string[] = [];

  const SERIES: Record<string, { color: string; dash: string }> = {
    webhooks_received: { color: "var(--series-1)", dash: "" },
    webhooks_duplicate: { color: "var(--series-2)", dash: "7 3" },
    webhooks_rejected_signature: { color: "var(--series-3)", dash: "2 3" },
    jobs_applied: { color: "var(--series-4)", dash: "10 3 2 3" },
    jobs_rejected_transition: { color: "var(--series-5)", dash: "5 2" },
    late_events_total: { color: "var(--series-6)", dash: "1 3" },
  };

  const PAD_R = 10;
  const PAD_T = 10;
  const PAD_B = 24;

  let hostWidth = 960;
  $: width = Math.max(300, Math.round(hostWidth || 960));
  $: height = width < 560 ? 184 : 220;
  $: padL = width < 560 ? 32 : 40;
  $: narrow = width < 560;

  interface Series {
    name: string;
    color: string;
    dash: string;
    points: { x: number; y: number; value: number }[];
    last: number;
    total: number;
  }

  interface TableRow {
    timestamp: number;
    values: Record<string, number>;
  }

  interface Chart {
    series: Series[];
    ticks: number[];
    y: (value: number) => number;
    xLabels: { x: number; label: string; anchor: string }[];
    tableRows: TableRow[];
    hasData: boolean;
    width: number;
    height: number;
    padL: number;
  }

  function niceStep(max: number): number {
    const rough = max / 4 || 1;
    const power = Math.pow(10, Math.floor(Math.log10(rough)));
    const normalized = rough / power;
    const factor = normalized <= 1 ? 1 : normalized <= 2 ? 2 : normalized <= 5 ? 5 : 10;
    return factor * power;
  }

  function build(
    points: MetricPoint[],
    names: string[],
    localeValue: string,
    widthValue: number,
    heightValue: number,
    padLValue: number,
  ): Chart {
    const observed = [...new Set(points.map((point) => +new Date(point.window_start)))].sort(
      (a, b) => a - b,
    );
    const byTimestamp = new Map<number, Map<string, number>>();
    for (const point of points) {
      const timestamp = +new Date(point.window_start);
      let bucket = byTimestamp.get(timestamp);
      if (!bucket) {
        bucket = new Map();
        byTimestamp.set(timestamp, bucket);
      }
      bucket.set(point.name, (bucket.get(point.name) ?? 0) + point.count);
    }

    const bucketStep = points[0]?.window_seconds ? points[0].window_seconds * 1000 : 60000;
    const firstTime = observed[0] ?? 0;
    const lastTime = observed[observed.length - 1] ?? 0;
    const chartTimestamps: number[] = [];
    if (observed.length > 0) {
      const step = bucketStep > 0 ? bucketStep : 60000;
      for (let timestamp = firstTime; timestamp <= lastTime; timestamp += step) {
        chartTimestamps.push(timestamp);
      }
      if (chartTimestamps[chartTimestamps.length - 1] !== lastTime) {
        chartTimestamps.push(lastTime);
      }
    }

    const values = names.map((name) => ({
      name,
      color: SERIES[name]?.color ?? "var(--chalk-muted)",
      dash: SERIES[name]?.dash ?? "",
      values: chartTimestamps.map((timestamp) => byTimestamp.get(timestamp)?.get(name) ?? 0),
    }));
    const max = Math.max(1, ...values.flatMap((series) => series.values));
    const step = niceStep(max);
    const top = Math.ceil(max / step) * step;
    const ticks = Array.from({ length: Math.round(top / step) + 1 }, (_, index) => index * step);
    const span = lastTime - firstTime;
    const x = (index: number) =>
      chartTimestamps.length <= 1 || span <= 0
        ? padLValue
        : padLValue +
          ((chartTimestamps[index] - firstTime) / span) * (widthValue - padLValue - PAD_R);
    const y = (value: number) =>
      heightValue - PAD_B - (value / top) * (heightValue - PAD_T - PAD_B);

    const series: Series[] = values.map((item) => ({
      name: item.name,
      color: item.color,
      dash: item.dash,
      last: item.values[item.values.length - 1] ?? 0,
      total: item.values.reduce((total, value) => total + value, 0),
      points: item.values.map((value, index) => ({ x: x(index), y: y(value), value })),
    }));

    const indexChoices =
      widthValue < 560
        ? [0, chartTimestamps.length - 1]
        : [0, Math.floor((chartTimestamps.length - 1) / 2), chartTimestamps.length - 1];
    const labelIndexes = chartTimestamps.length
      ? indexChoices.filter((value, index, array) => array.indexOf(value) === index)
      : [];
    const longSpan = span > 6 * 3600 * 1000;
    const xLabels = labelIndexes.map((index) => ({
      x: x(index),
      label: longSpan
        ? formatDateTimeShort(new Date(chartTimestamps[index]).toISOString(), localeValue)
        : formatTime(new Date(chartTimestamps[index]).toISOString(), localeValue),
      anchor: index === 0 ? "start" : index === chartTimestamps.length - 1 ? "end" : "middle",
    }));

    const tableRows: TableRow[] = observed.map((timestamp) => ({
      timestamp,
      values: Object.fromEntries(
        names.map((name) => [name, byTimestamp.get(timestamp)?.get(name) ?? 0]),
      ),
    }));

    return {
      series,
      ticks,
      y,
      xLabels,
      tableRows,
      hasData: observed.length > 0,
      width: widthValue,
      height: heightValue,
      padL: padLValue,
    };
  }

  $: chart = build(points, names, $locale, width, height, padL);
</script>

<div class="chart-wrap" bind:clientWidth={hostWidth}>
  {#if !chart.hasData}
    <div class="empty">
      <p class="empty-body">{$dict.overview.noMetrics}</p>
    </div>
  {:else}
    <svg
      viewBox={`0 0 ${chart.width} ${chart.height}`}
      width="100%"
      role="img"
      aria-label={`${$dict.overview.metricActivity}: ${chart.series
        .map((item) => `${$dict.metrics[item.name] ?? item.name} ${item.total}`)
        .join(", ")}`}
      style="display:block"
    >
      {#each chart.ticks as tick}
        <line
          x1={chart.padL}
          x2={chart.width - PAD_R}
          y1={chart.y(tick)}
          y2={chart.y(tick)}
          stroke="var(--rule)"
          stroke-width="1"
          stroke-dasharray={tick === 0 ? "" : "2 4"}
        />
        <text
          x={chart.padL - 8}
          y={chart.y(tick) + 4}
          text-anchor="end"
          font-size={narrow ? "9" : "10"}
          font-family="var(--font-mono)"
          fill="var(--chalk-muted)"
        >
          {tick}
        </text>
      {/each}
      {#each chart.xLabels as label}
        <text
          x={label.x}
          y={chart.height - 8}
          text-anchor={label.anchor}
          font-size={narrow ? "9" : "10"}
          font-family="var(--font-mono)"
          fill="var(--chalk-muted)"
        >
          {label.label}
        </text>
      {/each}
      {#each chart.series as item}
        <polyline
          points={item.points.map((point) => `${point.x},${point.y}`).join(" ")}
          fill="none"
          stroke={item.color}
          stroke-width="2"
          stroke-linecap="round"
          stroke-linejoin="round"
          stroke-dasharray={item.dash}
        />
        {#each item.points as point}
          {#if point.value > 0}
            <circle cx={point.x} cy={point.y} r="1.7" fill={item.color} />
          {/if}
        {/each}
      {/each}
    </svg>

    <div class="legend" style="margin-top:.7rem">
      {#each chart.series as item}
        <span class="legend-item">
          <span class="legend-swatch" style={`background:${item.color}`}></span>
          {$dict.metrics[item.name] ?? item.name}
          <span class="mono" style="color:var(--chalk-muted)">{item.last}</span>
        </span>
      {/each}
    </div>

    <details class="chart-data">
      <summary>{$dict.overview.chartData}</summary>
      <div class="table-wrap">
        <table class="table">
          <thead>
            <tr>
              <th scope="col">{$dict.overview.windowStart}</th>
              {#each chart.series as item}
                <th scope="col" class="num">{$dict.metrics[item.name] ?? item.name}</th>
              {/each}
            </tr>
          </thead>
          <tbody>
            {#each chart.tableRows as row}
              <tr>
                <td class="mono">
                  {formatDateTimeShort(new Date(row.timestamp).toISOString(), $locale)}
                </td>
                {#each chart.series as item}
                  <td class="num mono">{row.values[item.name] ?? 0}</td>
                {/each}
              </tr>
            {/each}
          </tbody>
        </table>
      </div>
    </details>
  {/if}
</div>

<style>
  .chart-wrap {
    width: 100%;
    min-width: 0;
  }

  .chart-data {
    margin-top: 0.75rem;
    border-top: 1px solid var(--rule);
    padding-top: 0.5rem;
  }

  .chart-data summary {
    cursor: pointer;
    color: var(--chalk-muted);
    font-family: var(--font-narrow);
    font-size: 0.64rem;
    letter-spacing: 0.08em;
    text-transform: uppercase;
  }

  .chart-data summary:hover {
    color: var(--chalk);
  }

  .chart-data[open] summary {
    margin-bottom: 0.5rem;
  }
</style>
