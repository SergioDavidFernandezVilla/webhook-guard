<script lang="ts">
  import { onMount } from "svelte";
  import {
    getCharge,
    listCharges,
    type Charge,
    type ChargeDetail,
  } from "../lib/api";
  import { isAbort } from "../lib/poll";
  import { dict, locale } from "../i18n";
  import { formatAmount, formatNumber } from "../lib/format";
  import PageHeader from "./PageHeader.svelte";
  import StateBadge from "./StateBadge.svelte";
  import TimeAgo from "./TimeAgo.svelte";
  import CopyButton from "./CopyButton.svelte";
  import EmptyState from "./EmptyState.svelte";
  import ErrorState from "./ErrorState.svelte";
  import LoadingBlock from "./LoadingBlock.svelte";
  import Pagination from "./Pagination.svelte";
  import Drawer from "./Drawer.svelte";
  import RevisionStrip from "./RevisionStrip.svelte";

  const STATES = ["created", "authorized", "captured", "failed", "refunded"];

  let query = "";
  let stateFilter = "";
  let limit = 25;
  let offset = 0;
  let items: Charge[] = [];
  let total = 0;
  let loading = false;
  let error: unknown = null;

  let detail: ChargeDetail | null = null;
  let detailId = "";
  let detailLoading = false;
  let detailError: unknown = null;

  let listController: AbortController | undefined;
  let detailController: AbortController | undefined;
  let requestId = 0;
  let debounce: ReturnType<typeof setTimeout> | undefined;

  async function load(): Promise<void> {
    const id = ++requestId;
    listController?.abort();
    listController = new AbortController();
    loading = true;
    error = null;
    try {
      const data = await listCharges(
        { q: query.trim(), state: stateFilter, limit, offset },
        listController.signal,
      );
      if (id !== requestId) return;
      items = data.items;
      total = data.total;
    } catch (err) {
      if (isAbort(err) || id !== requestId) return;
      error = err;
    } finally {
      if (id === requestId) loading = false;
    }
  }

  function drawerUrl(chargeId: string | null): string {
    const url = new URL(window.location.href);
    if (chargeId) url.searchParams.set("charge", chargeId);
    else url.searchParams.delete("charge");
    return `${url.pathname}${url.search}${url.hash}`;
  }

  async function openCharge(chargeId: string, push = true): Promise<void> {
    detailId = chargeId;
    detailLoading = true;
    detailError = null;
    detailController?.abort();
    detailController = new AbortController();
    if (push) {
      const current = new URLSearchParams(window.location.search).get("charge");
      if (current !== chargeId) {
        window.history.pushState({ wgDrawer: chargeId }, "", drawerUrl(chargeId));
      }
    }
    try {
      detail = await getCharge(chargeId, detailController.signal);
    } catch (err) {
      if (isAbort(err)) return;
      detail = null;
      detailError = err;
    } finally {
      detailLoading = false;
    }
  }

  function resetDetail(): void {
    detail = null;
    detailError = null;
    detailLoading = false;
    detailController?.abort();
  }

  function closeDetail(): void {
    if (window.history.state && window.history.state.wgDrawer) {
      window.history.back();
      return;
    }
    window.history.replaceState(null, "", drawerUrl(null));
    resetDetail();
  }

  function search(): void {
    if (debounce) clearTimeout(debounce);
    debounce = setTimeout(() => {
      offset = 0;
      void load();
    }, 300);
  }

  function applyFilters(): void {
    offset = 0;
    void load();
  }

  function resetFilters(): void {
    query = "";
    stateFilter = "";
    offset = 0;
    void load();
  }

  function changePage(nextOffset: number): void {
    offset = nextOffset;
    void load();
  }

  function retryDetail(): void {
    if (detailId) void openCharge(detailId, false);
  }

  onMount(() => {
    void load();
    const fromUrl = new URLSearchParams(window.location.search).get("charge");
    if (fromUrl) void openCharge(fromUrl, false);

    const onPopState = () => {
      const selected = new URLSearchParams(window.location.search).get("charge");
      if (selected) void openCharge(selected, false);
      else resetDetail();
    };
    window.addEventListener("popstate", onPopState);
    return () => {
      window.removeEventListener("popstate", onPopState);
      listController?.abort();
      detailController?.abort();
    };
  });
</script>

<PageHeader title={$dict.charges.title} description={$dict.charges.subtitle} code={$dict.charges.bay}>
  <span class="meta-line">
    {#if !loading}{$dict.charges.totalCharges.replace("{total}", formatNumber(total, $locale))}{/if}
  </span>
  <button type="button" class="btn" on:click={() => void load()} disabled={loading}>
    {$dict.common.refresh}
  </button>
</PageHeader>

<div class="stack">
  <section class="card" aria-busy={loading}>
    <div class="card-header">
      <div class="filters">
        <label class="visually-hidden" for="charge-search">{$dict.charges.searchPlaceholder}</label>
        <input
          id="charge-search"
          class="input"
          type="search"
          placeholder={$dict.charges.searchPlaceholder}
          bind:value={query}
          on:input={search}
        />
        <label class="visually-hidden" for="charge-state">{$dict.charges.stateFilter}</label>
        <select id="charge-state" class="select" bind:value={stateFilter} on:change={applyFilters}>
          <option value="">{$dict.charges.allStates}</option>
          {#each STATES as state}
            <option value={state}>{$dict.states[state] ?? state}</option>
          {/each}
        </select>
        <button type="button" class="btn btn-ghost btn-sm" on:click={resetFilters}>
          {$dict.common.clear}
        </button>
      </div>
    </div>

    {#if error && items.length === 0}
      <ErrorState error={error} retry={() => void load()} />
    {:else if loading && items.length === 0}
      <div class="card-body"><LoadingBlock rows={7} /></div>
    {:else if items.length === 0}
      <EmptyState title={$dict.charges.emptyTitle} body={$dict.charges.emptyBody} />
    {:else}
      <!-- svelte-ignore a11y_no_noninteractive_tabindex -->
      <div class="table-wrap hide-mobile" role="region" aria-label={$dict.charges.title} tabindex="0">
        <table class="strip-board">
          <caption class="visually-hidden">{$dict.charges.title}</caption>
          <thead>
            <tr>
              <th scope="col">{$dict.common.revisionShort}</th>
              <th scope="col">{$dict.common.charge}</th>
              <th scope="col">{$dict.common.state}</th>
              <th scope="col">{$dict.common.revisionChain}</th>
              <th scope="col" class="num">{$dict.common.amount}</th>
              <th scope="col">{$dict.charges.updated}</th>
              <th scope="col"><span class="visually-hidden">{$dict.common.details}</span></th>
            </tr>
          </thead>
          <tbody>
            {#each items as item (item.charge_id)}
              <tr class="strip-row">
                <td>
                  <span class="strip-rev">
                    {item.state === "created" ? "A" : item.state === "authorized" ? "B" : item.state === "captured" ? "C" : item.state === "refunded" ? "D" : "R"}
                  </span>
                </td>
                <td>
                  <div class="strip-id">
                    <span class="mono">{item.charge_id}</span>
                    <CopyButton value={item.charge_id} />
                  </div>
                </td>
                <td><StateBadge state={item.state} /></td>
                <td style="min-width:7rem"><RevisionStrip state={item.state} /></td>
                <td class="num">{formatAmount(item.amount_minor, item.currency, $locale)}</td>
                <td><TimeAgo iso={item.updated_at} /></td>
                <td class="num">
                  <button type="button" class="btn btn-sm" on:click={() => void openCharge(item.charge_id)}>
                    {$dict.common.details}
                  </button>
                </td>
              </tr>
            {/each}
          </tbody>
        </table>
      </div>

      <div class="card-list show-mobile">
        {#each items as item (item.charge_id)}
          <button type="button" class="card-list-item" on:click={() => void openCharge(item.charge_id)}>
            <span class="card-list-row">
              <span class="mono">{item.charge_id}</span>
              <StateBadge state={item.state} />
            </span>
            <span class="card-list-row">
              <span class="card-list-meta">{$dict.charges.updated} <TimeAgo iso={item.updated_at} /></span>
              <span class="card-list-meta">{formatAmount(item.amount_minor, item.currency, $locale)}</span>
            </span>
            <RevisionStrip state={item.state} labeled />
          </button>
        {/each}
      </div>

      <Pagination {offset} {limit} {total} onchange={changePage} />
    {/if}
  </section>
</div>

{#if detail || detailLoading || detailError}
  <Drawer title={$dict.charges.detailTitle} onclose={closeDetail}>
    {#if detailLoading}
      <LoadingBlock rows={8} />
    {:else if detailError}
      <ErrorState error={detailError} retry={retryDetail} />
    {:else if detail}
      <div class="stack">
        <dl class="kv">
          <dt>{$dict.common.charge}</dt>
          <dd>
            <span class="inline-copy">
              <span class="mono">{detail.charge.charge_id}</span>
              <CopyButton value={detail.charge.charge_id} />
            </span>
          </dd>
          <dt>{$dict.common.state}</dt>
          <dd><StateBadge state={detail.charge.state} /></dd>
          <dt>{$dict.common.amount}</dt>
          <dd>
            {formatAmount(detail.charge.amount_minor, detail.charge.currency, $locale)}
            <div class="kpi-foot">{$dict.charges.amountHint}</div>
          </dd>
          <dt>{$dict.charges.updated}</dt>
          <dd><TimeAgo iso={detail.charge.updated_at} /></dd>
          <dt>{$dict.common.lastEvent}</dt>
          <dd class="mono">{detail.charge.last_event_id || $dict.common.notAvailable}</dd>
        </dl>

        <section>
          <h3 class="card-title" style="margin-bottom:.5rem">{$dict.common.revision}</h3>
          <RevisionStrip state={detail.charge.state} labeled />
        </section>

        <section>
          <div class="card-header" style="padding-left:0;padding-right:0">
            <h3 class="card-title">{$dict.charges.ledgerTitle}</h3>
  <span class="meta-line" aria-live="polite">
              {$dict.charges.ledgerTotal}:
              {formatAmount(detail.ledger_total, detail.charge.currency, $locale)}
            </span>
          </div>
          {#if detail.ledger.length === 0}
            <p class="empty-body">{$dict.charges.noLedger}</p>
          {:else}
            <!-- svelte-ignore a11y_no_noninteractive_tabindex -->
            <div class="table-wrap hide-mobile" tabindex="0" role="region" aria-label={$dict.charges.ledgerTitle}>
              <table class="table">
                <thead>
                  <tr>
                    <th scope="col">{$dict.common.type}</th>
                    <th scope="col" class="num">{$dict.common.amount}</th>
                    <th scope="col">{$dict.common.event}</th>
                    <th scope="col">{$dict.common.received}</th>
                  </tr>
                </thead>
                <tbody>
                  {#each detail.ledger as entry (entry.id)}
                    <tr>
                      <td><StateBadge state={entry.entry_type} /></td>
                      <td class="num">{formatAmount(entry.amount_minor, entry.currency, $locale)}</td>
                      <td class="mono">{entry.source_event_id}</td>
                      <td><TimeAgo iso={entry.created_at} /></td>
                    </tr>
                  {/each}
                </tbody>
              </table>
            </div>
            <div class="slip-list show-mobile">
              {#each detail.ledger as entry (entry.id)}
                <div class="slip-item">
                  <span class="slip-row">
                    <StateBadge state={entry.entry_type} />
                    <span class="mono">{formatAmount(entry.amount_minor, entry.currency, $locale)}</span>
                  </span>
                  <span class="slip-row">
                    <span class="mono">{entry.source_event_id}</span>
                    <span class="slip-meta"><TimeAgo iso={entry.created_at} /></span>
                  </span>
                </div>
              {/each}
            </div>
          {/if}
        </section>

        <section>
          <div class="card-header" style="padding-left:0;padding-right:0">
            <h3 class="card-title">{$dict.charges.eventsTitle}</h3>
          </div>
          {#if detail.events.length === 0}
            {#if detail.ledger.length > 0}
              <div class="callout" data-tone="warning" style="margin:.35rem 0">
                <p style="margin:0">{$dict.charges.eventsMissing}</p>
              </div>
            {:else}
              <p class="empty-body">{$dict.charges.noEvents}</p>
            {/if}
          {:else}
            <!-- svelte-ignore a11y_no_noninteractive_tabindex -->
            <div class="table-wrap hide-mobile" tabindex="0" role="region" aria-label={$dict.charges.eventsTitle}>
              <table class="table">
                <thead>
                  <tr>
                    <th scope="col">{$dict.common.event}</th>
                    <th scope="col">{$dict.common.type}</th>
                    <th scope="col">{$dict.common.revision}</th>
                    <th scope="col">{$dict.common.status}</th>
                    <th scope="col">{$dict.common.received}</th>
                  </tr>
                </thead>
                <tbody>
                  {#each detail.events as event (event.event_id)}
                    <tr>
                      <td>
                        <a class="mono" href={`/events?event=${encodeURIComponent(event.event_id)}`}>
                          {event.event_id}
                        </a>
                      </td>
                      <td>{$dict.eventTypes[event.type] ?? event.type}</td>
                      <td style="min-width:7rem">
                        <RevisionStrip
                          kind="event"
                          state={event.type}
                          status={event.applied_at ? "applied" : "pending"}
                        />
                      </td>
                      <td>
                        <StateBadge
                          kind="status"
                          state={event.applied_at ? "applied" : "pending"}
                        />
                      </td>
                      <td><TimeAgo iso={event.received_at} /></td>
                    </tr>
                  {/each}
                </tbody>
              </table>
            </div>
            <div class="slip-list show-mobile">
              {#each detail.events as event (event.event_id)}
                <a class="slip-item" href={`/events?event=${encodeURIComponent(event.event_id)}`}>
                  <span class="slip-row">
                    <span class="mono">{event.event_id}</span>
                    <StateBadge kind="status" state={event.applied_at ? "applied" : "pending"} />
                  </span>
                  <span class="slip-row">
                    <span>{$dict.eventTypes[event.type] ?? event.type}</span>
                    <span class="slip-meta"><TimeAgo iso={event.received_at} /></span>
                  </span>
                  <RevisionStrip
                    kind="event"
                    state={event.type}
                    status={event.applied_at ? "applied" : "pending"}
                    labeled
                  />
                </a>
              {/each}
            </div>
          {/if}
        </section>

        <section>
          <div class="card-header" style="padding-left:0;padding-right:0">
            <h3 class="card-title">{$dict.charges.lateTitle}</h3>
          </div>
          {#if detail.late_events.length === 0}
            <p class="empty-body">{$dict.charges.noLate}</p>
          {:else}
            <!-- svelte-ignore a11y_no_noninteractive_tabindex -->
            <div class="table-wrap hide-mobile" tabindex="0" role="region" aria-label={$dict.charges.lateTitle}>
              <table class="table">
                <thead>
                  <tr>
                    <th scope="col">{$dict.common.event}</th>
                    <th scope="col">{$dict.common.reason}</th>
                    <th scope="col">{$dict.common.received}</th>
                    <th scope="col">{$dict.common.status}</th>
                  </tr>
                </thead>
                <tbody>
                  {#each detail.late_events as event (event.event_id)}
                    <tr>
                      <td class="mono">{event.event_id}</td>
                      <td>{$dict.reasons[event.reason] ?? event.reason}</td>
                      <td><TimeAgo iso={event.received_at} /></td>
                      <td>
                        <span class="badge" data-tone={event.resolved_at ? "success" : "warning"}>
                          {event.resolved_at ? $dict.common.resolved : $dict.common.unresolved}
                        </span>
                      </td>
                    </tr>
                  {/each}
                </tbody>
              </table>
            </div>
            <div class="slip-list show-mobile">
              {#each detail.late_events as event (event.event_id)}
                <div class="slip-item">
                  <span class="slip-row">
                    <span class="mono">{event.event_id}</span>
                    <span class="badge" data-tone={event.resolved_at ? "success" : "warning"}>
                      {event.resolved_at ? $dict.common.resolved : $dict.common.unresolved}
                    </span>
                  </span>
                  <span class="slip-row">
                    <span>{$dict.reasons[event.reason] ?? event.reason}</span>
                    <span class="slip-meta"><TimeAgo iso={event.received_at} /></span>
                  </span>
                </div>
              {/each}
            </div>
          {/if}
        </section>
      </div>
    {/if}
  </Drawer>
{/if}
