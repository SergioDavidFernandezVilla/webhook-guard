<script lang="ts">
  import { onMount } from "svelte";
  import { getMetrics, getOverview, listEvents, type EventItem } from "../lib/api";
  import { polling } from "../lib/poll";
  import { dict, format, locale } from "../i18n";
  import { formatNumber } from "../lib/format";
  import PageHeader from "./PageHeader.svelte";
  import StatCard from "./StatCard.svelte";
  import MetricsChart from "./MetricsChart.svelte";
  import TimeAgo from "./TimeAgo.svelte";
  import ErrorState from "./ErrorState.svelte";
  import EmptyState from "./EmptyState.svelte";
  import LoadingBlock from "./LoadingBlock.svelte";
  import RevisionStrip from "./RevisionStrip.svelte";
  import StateBadge from "./StateBadge.svelte";

  const METRICS = [
    "webhooks_received",
    "webhooks_duplicate",
    "webhooks_rejected_signature",
    "jobs_applied",
    "jobs_rejected_transition",
    "late_events_total",
  ];
  const STATE_ORDER = ["created", "authorized", "captured", "failed", "refunded"];
  const STATE_COLORS: Record<string, string> = {
    created: "var(--signal-blue)",
    authorized: "var(--signal-blue)",
    captured: "var(--signal-green)",
    failed: "var(--signal-red)",
    refunded: "var(--signal-violet)",
  };
  const REV_LETTER: Record<string, string> = {
    "payment.created": "A",
    "payment.authorized": "B",
    "payment.captured": "C",
    "payment.refunded": "D",
    "payment.failed": "R",
  };
  const DIFF_CATEGORIES = [
    "missing_local",
    "missing_remote",
    "amount_mismatch",
    "state_mismatch",
    "currency_mismatch",
  ];

  let minutes = 60;
  let initialWindow = true;
  const overviewPoller = polling(
    (signal) =>
      Promise.all([
        getOverview(signal),
        getMetrics(minutes, signal),
        listEvents({ limit: 8 }, signal),
      ]),
    5000,
  );

  $: overview = $overviewPoller.data?.[0] ?? null;
  $: metrics = $overviewPoller.data?.[1]?.items ?? [];
  $: recentEvents = ($overviewPoller.data?.[2]?.items ?? []) as EventItem[];
  $: maxState = overview
    ? Math.max(1, ...STATE_ORDER.map((state) => overview.charges.by_state[state] ?? 0))
    : 1;
  $: reportDiffs =
    overview?.latest_report
      ? DIFF_CATEGORIES.reduce(
          (total, category) => total + (overview.latest_report?.counts[category] ?? 0),
          0,
        )
      : 0;
  $: statusText = $overviewPoller.paused ? $dict.common.paused : $dict.common.live;
  $: if (initialWindow && overview) {
    if (metrics.length > 0) {
      initialWindow = false;
    } else if (minutes === 60) {
      minutes = 1440;
      refresh();
    } else {
      initialWindow = false;
    }
  }

  onMount(() => {
    overviewPoller.start();
    return overviewPoller.destroy;
  });

  function refresh(): void {
    void overviewPoller.refresh();
  }

  function changeWindow(): void {
    initialWindow = false;
    refresh();
  }

  function revLetter(type: string): string {
    return REV_LETTER[type] ?? "—";
  }
</script>

<PageHeader title={$dict.overview.title} description={$dict.overview.subtitle} code={$dict.overview.bay}>
  <span class="meta-line">
    <span class="live-dot" data-paused={$overviewPoller.paused}></span>
    <span>{statusText}</span>
    {#if $overviewPoller.updatedAt}
      <span>· {$dict.common.updated} <TimeAgo iso={$overviewPoller.updatedAt.toISOString()} /></span>
    {/if}
  </span>
  <label class="visually-hidden" for="overview-window">{$dict.overview.window}</label>
  <select id="overview-window" class="select" bind:value={minutes} on:change={changeWindow}>
    {#each [15, 60, 180, 1440] as value}
      <option value={value}>{format($dict.overview.lastMinutes, { minutes: value })}</option>
    {/each}
  </select>
  <button type="button" class="btn" on:click={refresh} disabled={$overviewPoller.loading}>
    {$dict.common.refresh}
  </button>
</PageHeader>

{#if $overviewPoller.error && !overview}
  <ErrorState error={$overviewPoller.error} retry={refresh} />
{:else if !overview}
  <div class="card">
    <div class="card-body"><LoadingBlock rows={8} /></div>
  </div>
{:else}
  <div class="stack">
    {#if $overviewPoller.error}
      <div class="callout" data-tone="warning">
        <div>
          <strong>{$dict.common.errorTitle}</strong>
          <p style="margin:.25rem 0 .5rem">{$dict.common.errorBody}</p>
          <button type="button" class="btn btn-sm" on:click={refresh}>{$dict.common.retry}</button>
        </div>
      </div>
    {/if}

    <section class="register" aria-label={$dict.overview.title}>
      <StatCard
        label={$dict.overview.eventsTotal}
        value={formatNumber(overview.events.total, $locale)}
        hint={`${formatNumber(overview.events.applied, $locale)} ${$dict.common.appliedPlural} · ${formatNumber(overview.events.pending, $locale)} ${$dict.common.pendingPlural}`}
      />
      <StatCard
        label={$dict.overview.eventsPending}
        value={formatNumber(overview.events.pending, $locale)}
        hint={$dict.overview.eventsPendingHint}
      />
      <StatCard
        label={$dict.overview.chargesTotal}
        value={formatNumber(overview.charges.total, $locale)}
      />
      <StatCard
        label={$dict.overview.ledgerEntries}
        value={formatNumber(overview.ledger_entries, $locale)}
      />
    </section>

    <span class="visually-hidden" aria-live="polite">
      {$dict.overview.eventsTotal}: {formatNumber(overview.events.total, $locale)}.
      {$dict.overview.eventsPending}: {formatNumber(overview.events.pending, $locale)}.
      {$dict.overview.chargesTotal}: {formatNumber(overview.charges.total, $locale)}.
      {$dict.overview.ledgerEntries}: {formatNumber(overview.ledger_entries, $locale)}.
    </span>

    <div class="grid-2">
      <section class="card">
        <div class="card-header">
          <div>
            <h2 class="card-title">{$dict.overview.eventRegister}</h2>
            <p class="card-subtitle">{$dict.overview.eventRegisterHint}</p>
          </div>
          <a class="btn btn-sm" href="/events">{$dict.overview.openRegister}</a>
        </div>
        {#if recentEvents.length === 0}
          <EmptyState title={$dict.events.emptyTitle} body={$dict.events.emptyBody} />
        {:else}
          <!-- svelte-ignore a11y_no_noninteractive_tabindex -->
          <div class="table-wrap hide-mobile" role="region" aria-label={$dict.overview.eventRegister} tabindex="0">
            <table class="strip-board">
              <caption class="visually-hidden">{$dict.overview.eventRegister}</caption>
              <thead>
                <tr>
                  <th scope="col">{$dict.common.revisionShort}</th>
                  <th scope="col">{$dict.common.event}</th>
                  <th scope="col">{$dict.common.type}</th>
                  <th scope="col">{$dict.common.charge}</th>
                  <th scope="col">{$dict.common.status}</th>
                  <th scope="col">{$dict.common.revisionChain}</th>
                  <th scope="col">{$dict.common.received}</th>
                </tr>
              </thead>
              <tbody>
                {#each recentEvents as item (item.event_id)}
                  <tr class="strip-row">
                    <td><span class="strip-rev">{revLetter(item.type)}</span></td>
                    <td>
                      <a class="mono" href={`/events?event=${encodeURIComponent(item.event_id)}`}>
                        {item.event_id}
                      </a>
                    </td>
                    <td>{$dict.eventTypes[item.type] ?? item.type}</td>
                    <td class="mono">
                      {#if item.charge_id}
                        <a href={`/charges?charge=${encodeURIComponent(item.charge_id)}`}>{item.charge_id}</a>
                      {:else}
                        {$dict.common.notAvailable}
                      {/if}
                    </td>
                    <td>
                      <StateBadge kind="status" state={item.applied_at ? "applied" : "pending"} />
                    </td>
                    <td style="min-width:7rem">
                      <RevisionStrip
                        kind="event"
                        state={item.type}
                        status={item.applied_at ? "applied" : "pending"}
                      />
                    </td>
                    <td><TimeAgo iso={item.received_at} /></td>
                  </tr>
                {/each}
              </tbody>
            </table>
          </div>

          <div class="card-list show-mobile">
            {#each recentEvents as item (item.event_id)}
              <a class="card-list-item" href={`/events?event=${encodeURIComponent(item.event_id)}`}>
                <span class="card-list-row">
                  <span class="mono">{item.event_id}</span>
                  <StateBadge kind="status" state={item.applied_at ? "applied" : "pending"} />
                </span>
                <span class="card-list-row">
                  <span>{$dict.eventTypes[item.type] ?? item.type}</span>
                  <span class="card-list-meta"><TimeAgo iso={item.received_at} /></span>
                </span>
                <RevisionStrip
                  kind="event"
                  state={item.type}
                  status={item.applied_at ? "applied" : "pending"}
                  labeled
                />
              </a>
            {/each}
          </div>
        {/if}
      </section>

      <aside class="rail" aria-label={$dict.overview.database}>
        <div class="rail-head">
          <span>{$dict.overview.bayStatus}</span>
          <span class="live-dot" data-paused={!overview.database.ready}></span>
        </div>

        <section class="rail-section">
          <h2 class="rail-label">{$dict.overview.database}</h2>
          <p class="rail-body" style="display:flex;align-items:center;gap:.4rem">
            <span class="live-dot" data-paused={!overview.database.ready}></span>
            <strong>{overview.database.ready ? $dict.overview.ready : $dict.overview.notReady}</strong>
          </p>
          <p class="rail-body mono" style="margin-top:.35rem;color:var(--chalk-muted)">
            {$dict.overview.latency}: {overview.database.latency_ms.toFixed(1)} ms<br />
            {$dict.overview.generated} <TimeAgo iso={overview.generated_at} />
          </p>
        </section>

        <section class="rail-section">
          <h2 class="rail-label">{$dict.overview.latestReport}</h2>
          {#if !overview.latest_report}
            <p class="rail-body">{$dict.overview.noReportBody}</p>
          {:else}
            <p class="rail-body">
              <span class="mono">{overview.latest_report.batch_id}</span><br />
              {$dict.overview.reportChecked}: {formatNumber(overview.latest_report.checked, $locale)} ·
              {$dict.overview.reportDiffs}: {formatNumber(reportDiffs, $locale)}
            </p>
            <p style="margin:.5rem 0 0">
              <span class="badge" data-tone={reportDiffs > 0 ? "danger" : "success"}>
                {reportDiffs > 0 ? $dict.overview.reportHasDiffs : $dict.overview.reportClean}
              </span>
            </p>
            <a class="btn btn-sm" style="margin-top:.55rem" href="/reconciliation">
              {$dict.overview.viewReport}
            </a>
          {/if}
        </section>

        <section class="rail-section">
          <h2 class="rail-label">{$dict.overview.chargesByState}</h2>
          <div class="bars">
            {#each STATE_ORDER as state}
              {@const count = overview.charges.by_state[state] ?? 0}
              <div class="bar-row">
                <span>{$dict.states[state] ?? state}</span>
                <span class="bar-track">
                  <span
                    class="bar-fill"
                    style={`width:${(count / maxState) * 100}%;background:${STATE_COLORS[state]}`}
                  ></span>
                </span>
                <span class="bar-value">{formatNumber(count, $locale)}</span>
              </div>
            {/each}
          </div>
        </section>

        <section class="rail-section">
          <h2 class="rail-label">{$dict.overview.latePending}</h2>
          <p class="rail-body">
            <strong>{formatNumber(overview.late_events.pending, $locale)}</strong> {$dict.common.pendingPlural} ·
            {formatNumber(overview.late_events.resolved, $locale)} {$dict.common.resolvedPlural}
          </p>
          <p class="rail-note" style="margin-top:.55rem">{$dict.late.note}</p>
        </section>
      </aside>
    </div>

    <section class="card chart-panel">
      <div class="card-header">
        <div>
          <h2 class="card-title">{$dict.overview.metricActivity}</h2>
          <p class="card-subtitle">{$dict.overview.metricsHint}</p>
        </div>
        <span class="meta-line">{format($dict.overview.lastMinutes, { minutes })}</span>
      </div>
      <div class="card-body">
        <MetricsChart points={metrics} names={METRICS} />
      </div>
    </section>
  </div>
{/if}
