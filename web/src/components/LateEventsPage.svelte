<script lang="ts">
  import { onMount } from "svelte";
  import { listLateEvents, type LateEvent } from "../lib/api";
  import { isAbort } from "../lib/poll";
  import { dict, locale } from "../i18n";
  import { formatNumber } from "../lib/format";
  import PageHeader from "./PageHeader.svelte";
  import TimeAgo from "./TimeAgo.svelte";
  import CopyButton from "./CopyButton.svelte";
  import EmptyState from "./EmptyState.svelte";
  import ErrorState from "./ErrorState.svelte";
  import LoadingBlock from "./LoadingBlock.svelte";
  import Pagination from "./Pagination.svelte";

  let resolvedFilter: "all" | "unresolved" | "resolved" = "unresolved";
  let limit = 25;
  let offset = 0;
  let items: LateEvent[] = [];
  let total = 0;
  let loading = false;
  let error: unknown = null;
  let controller: AbortController | undefined;
  let requestId = 0;

  async function load(): Promise<void> {
    const id = ++requestId;
    controller?.abort();
    controller = new AbortController();
    loading = true;
    error = null;
    try {
      const data = await listLateEvents(
        {
          resolved: resolvedFilter === "all" ? undefined : resolvedFilter === "resolved",
          limit,
          offset,
        },
        controller.signal,
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

  function applyFilter(): void {
    offset = 0;
    void load();
  }

  function changePage(nextOffset: number): void {
    offset = nextOffset;
    void load();
  }

  onMount(() => {
    void load();
    return () => controller?.abort();
  });
</script>

<PageHeader title={$dict.late.title} description={$dict.late.subtitle} code={$dict.late.bay}>
  <span class="meta-line" aria-live="polite">
    {formatNumber(total, $locale)} {$dict.common.results}
  </span>
  <button type="button" class="btn" on:click={() => void load()} disabled={loading}>
    {$dict.common.refresh}
  </button>
</PageHeader>

<div class="stack">
  <div class="callout" data-tone="warning">
    <p style="margin:0">{$dict.late.note}</p>
  </div>

  <section class="card" aria-busy={loading}>
    <div class="card-header">
      <div class="filters">
        <label class="visually-hidden" for="late-filter">{$dict.late.filter}</label>
        <select id="late-filter" class="select" bind:value={resolvedFilter} on:change={applyFilter}>
          <option value="unresolved">{$dict.late.unresolved}</option>
          <option value="resolved">{$dict.late.resolved}</option>
          <option value="all">{$dict.late.all}</option>
        </select>
      </div>
    </div>

    {#if error && items.length === 0}
      <ErrorState error={error} retry={() => void load()} />
    {:else if loading && items.length === 0}
      <div class="card-body"><LoadingBlock rows={7} /></div>
    {:else if items.length === 0}
      <EmptyState title={$dict.late.emptyTitle} body={$dict.late.emptyBody} />
    {:else}
      <!-- svelte-ignore a11y_no_noninteractive_tabindex -->
      <div class="table-wrap hide-mobile" role="region" aria-label={$dict.late.title} tabindex="0">
        <table class="strip-board">
          <caption class="visually-hidden">{$dict.late.title}</caption>
          <thead>
            <tr>
              <th scope="col">{$dict.common.event}</th>
              <th scope="col">{$dict.common.charge}</th>
              <th scope="col">{$dict.common.reason}</th>
              <th scope="col">{$dict.common.received}</th>
              <th scope="col">{$dict.common.status}</th>
            </tr>
          </thead>
          <tbody>
            {#each items as item (item.event_id)}
              <tr class="strip-row">
                <td>
                  <div class="strip-id">
                    <span class="mono">{item.event_id}</span>
                    <CopyButton value={item.event_id} />
                  </div>
                </td>
                <td>
                  <a class="mono" href={`/charges?charge=${encodeURIComponent(item.charge_id)}`}>
                    {item.charge_id}
                  </a>
                </td>
                <td>{$dict.reasons[item.reason] ?? item.reason}</td>
                <td><TimeAgo iso={item.received_at} /></td>
                <td>
                  <span class="badge" data-tone={item.resolved_at ? "success" : "warning"}>
                    {item.resolved_at ? $dict.common.resolved : $dict.common.unresolved}
                  </span>
                </td>
              </tr>
            {/each}
          </tbody>
        </table>
      </div>

      <div class="card-list show-mobile">
        {#each items as item (item.event_id)}
          <a class="card-list-item" href={`/charges?charge=${encodeURIComponent(item.charge_id)}`}>
            <span class="card-list-row">
              <span class="mono">{item.event_id}</span>
              <span class="badge" data-tone={item.resolved_at ? "success" : "warning"}>
                {item.resolved_at ? $dict.common.resolved : $dict.common.unresolved}
              </span>
            </span>
            <span class="card-list-row">
              <span>{$dict.reasons[item.reason] ?? item.reason}</span>
              <span class="card-list-meta"><TimeAgo iso={item.received_at} /></span>
            </span>
          </a>
        {/each}
      </div>

      <Pagination {offset} {limit} {total} onchange={changePage} />
    {/if}
  </section>
</div>
