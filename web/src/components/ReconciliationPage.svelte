<script lang="ts">
  import { onMount } from "svelte";
  import {
    getLatestReport,
    listReports,
    type LatestReport,
    type Report,
    type StoredReport,
  } from "../lib/api";
  import { isAbort } from "../lib/poll";
  import { dict, format, locale } from "../i18n";
  import { formatAmount, formatNumber } from "../lib/format";
  import PageHeader from "./PageHeader.svelte";
  import TimeAgo from "./TimeAgo.svelte";
  import EmptyState from "./EmptyState.svelte";
  import ErrorState from "./ErrorState.svelte";
  import LoadingBlock from "./LoadingBlock.svelte";

  const CATEGORIES = [
    "ok",
    "missing_local",
    "missing_remote",
    "amount_mismatch",
    "state_mismatch",
    "currency_mismatch",
  ] as const;
  const DIFF_CATEGORIES = [
    "missing_local",
    "missing_remote",
    "amount_mismatch",
    "state_mismatch",
    "currency_mismatch",
  ] as const;
  const DIFF_TONES: Record<string, string> = {
    missing_local: "warning",
    missing_remote: "warning",
    amount_mismatch: "danger",
    state_mismatch: "info",
    currency_mismatch: "violet",
  };
  const MAX_DIFFS = 50;

  let latest: LatestReport | null = null;
  let reports: StoredReport[] = [];
  let selected: StoredReport | null = null;
  let loading = false;
  let error: unknown = null;
  let controller: AbortController | undefined;
  let reportEl: HTMLElement | null = null;

  interface Shown {
    batch_id: string;
    created_at: string;
    report: Report;
  }

  $: shown = selected
    ? { batch_id: selected.batch_id, created_at: selected.created_at, report: selected.report }
    : latest?.report
      ? {
          batch_id: latest.report.batch_id,
          created_at: latest.report.created_at,
          report: latest.report.report,
        }
      : null;
  $: hasDiffs = shown
    ? (shown.report.has_diffs ??
      DIFF_CATEGORIES.reduce((total, category) => total + (shown?.report.counts[category] ?? 0), 0) > 0)
    : false;
  $: diffs = shown?.report.diffs ?? [];
  $: visibleDiffs = diffs.slice(0, MAX_DIFFS);
  $: diffTotal = DIFF_CATEGORIES.reduce(
    (total, category) => total + (shown?.report.counts[category] ?? 0),
    0,
  );
  $: truncated = visibleDiffs.length < diffTotal;

  function diffText(value: unknown, localeValue: string, states: Record<string, string>): string {
    if (value === null || value === undefined) return "—";
    if (typeof value === "object") {
      const record = value as Record<string, unknown>;
      const state = record.State ?? record.state;
      const amount = record.AmountMinor ?? record.amount_minor;
      const currency = record.Currency ?? record.currency;
      if (typeof state === "string" && typeof amount === "number") {
        return `${states[state] ?? state} · ${formatAmount(amount, String(currency ?? ""), localeValue)}`;
      }
      const text = JSON.stringify(value);
      return text.length > 64 ? `${text.slice(0, 63)}…` : text;
    }
    return String(value);
  }

  function diffTitle(value: unknown): string {
    if (value === null || value === undefined) return "";
    return typeof value === "string" ? value : JSON.stringify(value);
  }

  function revealReport(): void {
    if (!reportEl) return;
    const reduce = window.matchMedia("(prefers-reduced-motion: reduce)").matches;
    reportEl.scrollIntoView({ behavior: reduce ? "auto" : "smooth", block: "start" });
  }

  async function load(): Promise<void> {
    loading = true;
    error = null;
    controller?.abort();
    controller = new AbortController();
    try {
      const [latestData, history] = await Promise.all([
        getLatestReport(controller.signal),
        listReports(20, controller.signal),
      ]);
      latest = latestData;
      reports = history.items;
      if (!selected) selected = null;
    } catch (err) {
      if (isAbort(err)) return;
      error = err;
    } finally {
      loading = false;
    }
  }

  function select(report: StoredReport): void {
    selected = report;
    revealReport();
  }

  function showLatest(): void {
    selected = null;
    revealReport();
  }

  onMount(() => {
    void load();
    return () => controller?.abort();
  });
</script>

<PageHeader title={$dict.reconciliation.title} description={$dict.reconciliation.subtitle} code={$dict.reconciliation.bay}>
  {#if selected}
    <button type="button" class="btn btn-sm" on:click={showLatest}>
      {$dict.reconciliation.latest}
    </button>
  {/if}
  <button type="button" class="btn" on:click={() => void load()} disabled={loading}>
    {$dict.common.refresh}
  </button>
</PageHeader>

<div class="stack">
  <div class="callout">
    <p style="margin:0">{$dict.reconciliation.exportHint}</p>
  </div>

  {#if error && !shown}
    <ErrorState error={error} retry={() => void load()} />
  {:else if loading && !shown}
    <div class="card"><div class="card-body"><LoadingBlock rows={8} /></div></div>
  {:else if !shown}
    <div class="card">
      <EmptyState title={$dict.reconciliation.emptyTitle} body={$dict.reconciliation.emptyBody} />
    </div>
  {:else}
    <section class="clearance-sheet" aria-live="polite" bind:this={reportEl}>
      <div class="clearance-head">
        <div>
          <h2 class="card-title">
            {selected ? $dict.reconciliation.history : $dict.reconciliation.latest}
          </h2>
          <p class="card-subtitle mono">{shown.batch_id}</p>
        </div>
        <span class="clearance-stamp" data-tone={hasDiffs ? "warning" : "success"}>
          {hasDiffs ? $dict.reconciliation.hasDiffs : $dict.reconciliation.clean}
          <small>
            {hasDiffs
              ? `${formatNumber(diffTotal, $locale)} ${$dict.reconciliation.diffs}`
              : `${formatNumber(shown.report.checked, $locale)} ${$dict.reconciliation.checked}`}
          </small>
        </span>
      </div>

      <dl class="kv" style="margin-top:.9rem">
        <dt>{$dict.reconciliation.batch}</dt>
        <dd class="mono">{shown.batch_id}</dd>
        <dt>{$dict.reconciliation.createdAt}</dt>
        <dd><TimeAgo iso={shown.created_at} /></dd>
        <dt>{$dict.reconciliation.checked}</dt>
        <dd>{formatNumber(shown.report.checked, $locale)}</dd>
        <dt>{$dict.reconciliation.diffs}</dt>
        <dd>{formatNumber(diffTotal, $locale)}</dd>
      </dl>

      <div style="margin-top:.9rem">
        <h3 class="kpi-label" style="margin-bottom:.35rem">{$dict.reconciliation.counts}</h3>
        <div class="counts-grid">
          {#each CATEGORIES as category}
            {@const count = shown.report.counts[category] ?? 0}
            <div class="count-cell">
              <span class="kpi-label">{$dict.reconciliation[category]}</span>
              <span class="count-value">{formatNumber(count, $locale)}</span>
            </div>
          {/each}
        </div>
      </div>
    </section>

    <section class="card">
      <div class="card-header">
        <h2 class="card-title">{$dict.reconciliation.diffs}</h2>
        <span class="meta-line">
          {$dict.common.showing} {Math.min(visibleDiffs.length, diffTotal)}
          {$dict.common.of} {formatNumber(diffTotal, $locale)}
        </span>
      </div>
      {#if visibleDiffs.length === 0}
        <EmptyState title={$dict.reconciliation.clean} body={$dict.reconciliation.noDiffs} />
      {:else}
        {#if truncated}
          <div class="callout" data-tone="warning" style="margin:.75rem">
            <p style="margin:0">
              {format($dict.reconciliation.diffLimit, {
                shown: visibleDiffs.length,
                total: formatNumber(diffTotal, $locale),
              })}
            </p>
          </div>
        {/if}
        <ul class="diff-list" style="padding:.35rem">
          {#each visibleDiffs as diff (diff.charge_id + diff.category)}
            <li class="diff-item">
              <div class="diff-item-head">
                <a class="mono" href={`/charges?charge=${encodeURIComponent(diff.charge_id)}`}>
                  {diff.charge_id}
                </a>
                <span class="badge" data-tone={DIFF_TONES[diff.category] ?? "warning"}>
                  {$dict.reconciliation[diff.category as keyof typeof $dict.reconciliation] ??
                    diff.category}
                </span>
              </div>
              <div class="diff-values">
                <span class="diff-label">{$dict.reconciliation.expected}</span>
                <span class="mono" title={diffTitle(diff.expected)}>
                  {diffText(diff.expected, $locale, $dict.states)}
                </span>
                <span class="diff-label">{$dict.reconciliation.actual}</span>
                <span class="mono" title={diffTitle(diff.actual)}>
                  {diffText(diff.actual, $locale, $dict.states)}
                </span>
              </div>
            </li>
          {/each}
        </ul>
      {/if}
    </section>

    <section class="card">
      <div class="card-header">
        <h2 class="card-title">{$dict.reconciliation.history}</h2>
      </div>
      {#if reports.length === 0}
        <div class="card-body"><p class="empty-body">{$dict.reconciliation.historyEmpty}</p></div>
      {:else}
        <!-- svelte-ignore a11y_no_noninteractive_tabindex -->
        <div class="table-wrap hide-mobile" role="region" aria-label={$dict.reconciliation.history} tabindex="0">
          <table class="strip-board">
            <caption class="visually-hidden">{$dict.reconciliation.history}</caption>
            <thead>
              <tr>
                <th scope="col">{$dict.reconciliation.batch}</th>
                <th scope="col">{$dict.reconciliation.createdAt}</th>
                <th scope="col" class="num">{$dict.reconciliation.checked}</th>
                <th scope="col" class="num">{$dict.reconciliation.diffs}</th>
                <th scope="col"><span class="visually-hidden">{$dict.common.details}</span></th>
              </tr>
            </thead>
            <tbody>
              {#each reports as report (report.id)}
                {@const reportDiffTotal = DIFF_CATEGORIES.reduce(
                  (total, category) => total + (report.report.counts[category] ?? 0),
                  0,
                )}
                <tr class="strip-row">
                  <td class="mono">{report.batch_id}</td>
                  <td><TimeAgo iso={report.created_at} /></td>
                  <td class="num">{formatNumber(report.report.checked, $locale)}</td>
                  <td class="num">
                    <span class="badge" data-tone={reportDiffTotal > 0 ? "danger" : "success"}>
                      {formatNumber(reportDiffTotal, $locale)}
                    </span>
                  </td>
                  <td class="num">
                    <button
                      type="button"
                      class="btn btn-sm"
                      aria-pressed={selected?.id === report.id}
                      on:click={() => select(report)}
                    >
                      {$dict.common.details}
                    </button>
                  </td>
                </tr>
              {/each}
            </tbody>
          </table>
        </div>

        <div class="card-list show-mobile">
          {#each reports as report (report.id)}
            {@const reportDiffTotal = DIFF_CATEGORIES.reduce(
              (total, category) => total + (report.report.counts[category] ?? 0),
              0,
            )}
            <button
              type="button"
              class="card-list-item"
              aria-pressed={selected?.id === report.id}
              on:click={() => select(report)}
            >
              <span class="card-list-row">
                <span class="mono">{report.batch_id}</span>
                <span class="badge" data-tone={reportDiffTotal > 0 ? "danger" : "success"}>
                  {formatNumber(reportDiffTotal, $locale)} {$dict.reconciliation.diffs}
                </span>
              </span>
              <span class="card-list-row">
                <span class="card-list-meta">
                  {$dict.reconciliation.checked}: {formatNumber(report.report.checked, $locale)}
                </span>
                <span class="card-list-meta"><TimeAgo iso={report.created_at} /></span>
              </span>
            </button>
          {/each}
        </div>
      {/if}
    </section>
  {/if}
</div>
