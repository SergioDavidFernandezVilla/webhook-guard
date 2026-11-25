<script lang="ts">
  import { onMount } from "svelte";
  import { getEvent, listEvents, type EventDetail, type EventItem } from "../lib/api";
  import { isAbort } from "../lib/poll";
  import { dict, locale } from "../i18n";
  import { formatDateTime, formatAmount } from "../lib/format";
  import PageHeader from "./PageHeader.svelte";
  import StateBadge from "./StateBadge.svelte";
  import TimeAgo from "./TimeAgo.svelte";
  import CopyButton from "./CopyButton.svelte";
  import EmptyState from "./EmptyState.svelte";
  import ErrorState from "./ErrorState.svelte";
  import LoadingBlock from "./LoadingBlock.svelte";
  import Drawer from "./Drawer.svelte";
  import RevisionStrip from "./RevisionStrip.svelte";

  const TYPES = [
    "payment.created",
    "payment.authorized",
    "payment.captured",
    "payment.failed",
    "payment.refunded",
  ];
  const REV_LETTER: Record<string, string> = {
    "payment.created": "A",
    "payment.authorized": "B",
    "payment.captured": "C",
    "payment.refunded": "D",
    "payment.failed": "R",
  };

  let statusFilter = "";
  let typeFilter = "";
  let chargeQuery = "";
  let items: EventItem[] = [];
  let loading = false;
  let error: unknown = null;
  let beforeId = 0;
  let nextBeforeId: number | null = null;
  let history: number[] = [];

  let detail: EventDetail | null = null;
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
      const data = await listEvents(
        {
          status: statusFilter,
          type: typeFilter,
          charge_id: chargeQuery.trim(),
          before_id: beforeId || undefined,
          limit: 25,
        },
        listController.signal,
      );
      if (id !== requestId) return;
      items = data.items;
      nextBeforeId = data.next_before_id;
    } catch (err) {
      if (isAbort(err) || id !== requestId) return;
      error = err;
    } finally {
      if (id === requestId) loading = false;
    }
  }

  function drawerUrl(eventId: string | null): string {
    const url = new URL(window.location.href);
    if (eventId) url.searchParams.set("event", eventId);
    else url.searchParams.delete("event");
    return `${url.pathname}${url.search}${url.hash}`;
  }

  async function openEvent(eventId: string, push = true): Promise<void> {
    detailId = eventId;
    detailLoading = true;
    detailError = null;
    detailController?.abort();
    detailController = new AbortController();
    if (push) {
      const current = new URLSearchParams(window.location.search).get("event");
      if (current !== eventId) {
        window.history.pushState({ wgDrawer: eventId }, "", drawerUrl(eventId));
      }
    }
    try {
      detail = await getEvent(eventId, detailController.signal);
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

  function applyFilters(): void {
    beforeId = 0;
    nextBeforeId = null;
    history = [];
    void load();
  }

  function searchCharge(): void {
    if (debounce) clearTimeout(debounce);
    debounce = setTimeout(applyFilters, 300);
  }

  function older(): void {
    if (nextBeforeId === null) return;
    history = [...history, beforeId];
    beforeId = nextBeforeId;
    void load();
  }

  function newer(): void {
    if (history.length === 0) return;
    beforeId = history[history.length - 1];
    history = history.slice(0, -1);
    void load();
  }

  function retryDetail(): void {
    if (detailId) void openEvent(detailId, false);
  }

  $: prettyPayload = detail ? JSON.stringify(detail.event.payload, null, 2) : "";

  onMount(() => {
    void load();
    const fromUrl = new URLSearchParams(window.location.search).get("event");
    if (fromUrl) void openEvent(fromUrl, false);

    const onPopState = () => {
      const selected = new URLSearchParams(window.location.search).get("event");
      if (selected) void openEvent(selected, false);
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

<PageHeader title={$dict.events.title} description={$dict.events.subtitle} code={$dict.events.bay}>
  <span class="meta-line" aria-live="polite">
    {$dict.common.showing} {items.length} {$dict.common.results}
  </span>
  <button type="button" class="btn" on:click={() => void load()} disabled={loading}>
    {$dict.common.refresh}
  </button>
</PageHeader>

<div class="stack">
  <section class="card" aria-busy={loading}>
    <div class="card-header">
      <div class="filters">
        <label class="visually-hidden" for="event-status">{$dict.events.statusFilter}</label>
        <select id="event-status" class="select" bind:value={statusFilter} on:change={applyFilters}>
          <option value="">{$dict.events.allStatuses}</option>
          <option value="applied">{$dict.common.applied}</option>
          <option value="pending">{$dict.common.pending}</option>
        </select>
        <label class="visually-hidden" for="event-type">{$dict.events.typeFilter}</label>
        <select id="event-type" class="select" bind:value={typeFilter} on:change={applyFilters}>
          <option value="">{$dict.events.allTypes}</option>
          {#each TYPES as type}
            <option value={type}>{$dict.eventTypes[type] ?? type}</option>
          {/each}
        </select>
        <label class="visually-hidden" for="event-charge">{$dict.events.chargeFilter}</label>
        <input
          id="event-charge"
          class="input"
          type="search"
          placeholder={$dict.events.chargeFilter}
          bind:value={chargeQuery}
          on:input={searchCharge}
        />
      </div>
    </div>

    {#if error && items.length === 0}
      <ErrorState error={error} retry={() => void load()} />
    {:else if loading && items.length === 0}
      <div class="card-body"><LoadingBlock rows={7} /></div>
    {:else if items.length === 0}
      <EmptyState title={$dict.events.emptyTitle} body={$dict.events.emptyBody} />
    {:else}
      <!-- svelte-ignore a11y_no_noninteractive_tabindex -->
      <div class="table-wrap hide-mobile" role="region" aria-label={$dict.events.title} tabindex="0">
        <table class="strip-board">
          <caption class="visually-hidden">{$dict.events.title}</caption>
          <thead>
            <tr>
              <th scope="col">{$dict.common.revisionShort}</th>
              <th scope="col">{$dict.common.event}</th>
              <th scope="col">{$dict.common.type}</th>
              <th scope="col">{$dict.common.charge}</th>
              <th scope="col">{$dict.common.status}</th>
              <th scope="col">{$dict.common.revisionChain}</th>
              <th scope="col">{$dict.common.received}</th>
              <th scope="col"><span class="visually-hidden">{$dict.common.details}</span></th>
            </tr>
          </thead>
          <tbody>
            {#each items as item (item.event_id)}
              <tr class="strip-row">
                <td>
                  <span class="strip-rev">{REV_LETTER[item.type] ?? "—"}</span>
                </td>
                <td>
                  <div class="strip-id">
                    <span class="mono">{item.event_id}</span>
                    <CopyButton value={item.event_id} />
                  </div>
                </td>
                <td>{$dict.eventTypes[item.type] ?? item.type}</td>
                <td class="mono">
                  {#if item.charge_id}
                    <a href={`/charges?charge=${encodeURIComponent(item.charge_id)}`}>{item.charge_id}</a>
                  {:else}
                    {$dict.common.notAvailable}
                  {/if}
                </td>
                <td><StateBadge kind="status" state={item.applied_at ? "applied" : "pending"} /></td>
                <td style="min-width:7rem">
                  <RevisionStrip
                    kind="event"
                    state={item.type}
                    status={item.applied_at ? "applied" : "pending"}
                  />
                </td>
                <td><TimeAgo iso={item.received_at} /></td>
                <td class="num">
                  <button type="button" class="btn btn-sm" on:click={() => void openEvent(item.event_id)}>
                    {$dict.common.details}
                  </button>
                </td>
              </tr>
            {/each}
          </tbody>
        </table>
      </div>

      <div class="card-list show-mobile">
        {#each items as item (item.event_id)}
          <button type="button" class="card-list-item" on:click={() => void openEvent(item.event_id)}>
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
          </button>
        {/each}
      </div>

      <div class="pagination">
        <span class="pagination-info" role="status" aria-live="polite">
          {$dict.events.keysetHint}
        </span>
        <div class="toolbar" role="group" aria-label={$dict.common.page}>
          <button type="button" class="btn btn-sm" disabled={history.length === 0} on:click={newer}>
            {$dict.events.newer}
          </button>
          <button type="button" class="btn btn-sm" disabled={nextBeforeId === null} on:click={older}>
            {$dict.events.older}
          </button>
        </div>
      </div>
    {/if}
  </section>
</div>

{#if detail || detailLoading || detailError}
  <Drawer title={$dict.events.detailTitle} onclose={closeDetail}>
    {#if detailLoading}
      <LoadingBlock rows={8} />
    {:else if detailError}
      <ErrorState error={detailError} retry={retryDetail} />
    {:else if detail}
      <div class="stack">
        <dl class="kv">
          <dt>{$dict.common.event}</dt>
          <dd>
            <span class="inline-copy">
              <span class="mono">{detail.event.event_id}</span>
              <CopyButton value={detail.event.event_id} />
            </span>
          </dd>
          <dt>{$dict.common.type}</dt>
          <dd>{$dict.eventTypes[detail.event.type] ?? detail.event.type}</dd>
          <dt>{$dict.common.charge}</dt>
          <dd>
            {#if detail.event.charge_id}
              <a class="mono" href={`/charges?charge=${encodeURIComponent(detail.event.charge_id)}`}>
                {detail.event.charge_id}
              </a>
            {:else}
              {$dict.common.notAvailable}
            {/if}
          </dd>
          <dt>{$dict.common.status}</dt>
          <dd><StateBadge kind="status" state={detail.event.applied_at ? "applied" : "pending"} /></dd>
          <dt>{$dict.common.signatureTimestamp}</dt>
          <dd>{formatDateTime(new Date(detail.event.signature_timestamp * 1000).toISOString(), $locale)}</dd>
          <dt>{$dict.common.correlation}</dt>
          <dd>
            <span class="inline-copy">
              <span class="mono">{detail.event.correlation_id || $dict.common.notAvailable}</span>
              {#if detail.event.correlation_id}
                <CopyButton value={detail.event.correlation_id} />
              {/if}
            </span>
          </dd>
          <dt>{$dict.common.received}</dt>
          <dd><TimeAgo iso={detail.event.received_at} /></dd>
          <dt>{$dict.events.appliedAt}</dt>
          <dd><TimeAgo iso={detail.event.applied_at} /></dd>
        </dl>

        <section>
          <h3 class="card-title" style="margin-bottom:.5rem">{$dict.common.revision}</h3>
          <RevisionStrip
            kind="event"
            state={detail.event.type}
            status={detail.event.applied_at ? "applied" : "pending"}
            labeled
          />
        </section>

        <section>
          <h3 class="card-title" style="margin-bottom:.4rem">{$dict.events.payload}</h3>
          <pre class="code">{prettyPayload}</pre>
        </section>

        <section>
          <h3 class="card-title" style="margin-bottom:.4rem">{$dict.common.ledger}</h3>
          {#if !detail.ledger}
            <p class="empty-body">{$dict.events.noLedger}</p>
          {:else}
            <!-- svelte-ignore a11y_no_noninteractive_tabindex -->
            <div class="table-wrap hide-mobile" tabindex="0" role="region" aria-label={$dict.common.ledger}>
              <table class="table">
                <thead>
                  <tr>
                    <th scope="col">{$dict.common.type}</th>
                    <th scope="col" class="num">{$dict.common.amount}</th>
                    <th scope="col">{$dict.common.charge}</th>
                  </tr>
                </thead>
                <tbody>
                  <tr>
                    <td><StateBadge state={detail.ledger.entry_type} /></td>
                    <td class="num">
                      {formatAmount(detail.ledger.amount_minor, detail.ledger.currency, $locale)}
                    </td>
                    <td class="mono">{detail.ledger.charge_id}</td>
                  </tr>
                </tbody>
              </table>
            </div>
            <div class="slip-list show-mobile">
              <div class="slip-item">
                <span class="slip-row">
                  <StateBadge state={detail.ledger.entry_type} />
                  <span class="mono">
                    {formatAmount(detail.ledger.amount_minor, detail.ledger.currency, $locale)}
                  </span>
                </span>
                <span class="slip-row">
                  <span class="mono">{detail.ledger.charge_id}</span>
                </span>
              </div>
            </div>
          {/if}
        </section>

        <section>
          <h3 class="card-title" style="margin-bottom:.4rem">{$dict.common.lateEvents}</h3>
          {#if !detail.late}
            <p class="empty-body">{$dict.events.noLate}</p>
          {:else}
            <dl class="kv">
              <dt>{$dict.common.reason}</dt>
              <dd>{$dict.reasons[detail.late.reason] ?? detail.late.reason}</dd>
              <dt>{$dict.common.status}</dt>
              <dd>
                <span class="badge" data-tone={detail.late.resolved_at ? "success" : "warning"}>
                  {detail.late.resolved_at ? $dict.common.resolved : $dict.common.unresolved}
                </span>
              </dd>
            </dl>
          {/if}
        </section>
      </div>
    {/if}
  </Drawer>
{/if}
