<script lang="ts">
  import { onMount } from "svelte";
  import { getLatestLoadgenRuns, type LoadgenRun } from "../lib/api";
  import { isAbort } from "../lib/poll";
  import { dict, locale } from "../i18n";
  import { formatNumber } from "../lib/format";
  import PageHeader from "./PageHeader.svelte";
  import CopyButton from "./CopyButton.svelte";
  import TimeAgo from "./TimeAgo.svelte";
  import EmptyState from "./EmptyState.svelte";
  import ErrorState from "./ErrorState.svelte";
  import LoadingBlock from "./LoadingBlock.svelte";

  interface ReferenceRow {
    route: string;
    sent: string;
    accepted: string;
    duplicates: string;
    errors: string;
    latency: string;
  }

  const reference: ReferenceRow[] = [
    {
      route: "guarded",
      sent: "1000",
      accepted: "500",
      duplicates: "500",
      errors: "0",
      latency: "35.9 / 285.7 / 531.1 ms",
    },
    {
      route: "naive",
      sent: "1000",
      accepted: "500",
      duplicates: "0",
      errors: "500",
      latency: "19.6 / 195.5 / 395.6 ms",
    },
  ];

  let liveRuns: LoadgenRun[] = [];
  let liveLoading = true;
  let liveError: unknown = null;
  let controller: AbortController | undefined;
  let requestId = 0;

  async function loadLive(): Promise<void> {
    const id = ++requestId;
    controller?.abort();
    controller = new AbortController();
    liveLoading = true;
    liveError = null;
    try {
      const data = await getLatestLoadgenRuns(controller.signal);
      if (id !== requestId) return;
      liveRuns = data.items;
    } catch (err) {
      if (isAbort(err) || id !== requestId) return;
      liveError = err;
    } finally {
      if (id === requestId) liveLoading = false;
    }
  }

  onMount(() => {
    void loadLive();
    return () => controller?.abort();
  });
</script>

<PageHeader title={$dict.compare.title} description={$dict.compare.subtitle} code={$dict.compare.bay} />

<div class="stack">
  <div class="callout">
    <p style="margin:0">{$dict.compare.intro}</p>
  </div>

  <div class="grid-2">
    <section class="route-strip" data-route="guarded">
      <div class="card-header">
        <h2 class="card-title">{$dict.compare.guardedTitle}</h2>
        <span class="badge" data-tone="success">{$dict.compare.defaultBadge}</span>
      </div>
      <div class="card-body">
        <p style="margin:0">{$dict.compare.guardedBody}</p>
      </div>
    </section>

    <section class="route-strip" data-route="naive">
      <div class="card-header">
        <h2 class="card-title">{$dict.compare.naiveTitle}</h2>
        <span class="badge" data-tone="violet">naive</span>
      </div>
      <div class="card-body">
        <p style="margin:0">{$dict.compare.naiveBody}</p>
      </div>
    </section>
  </div>

  <section class="card">
    <div class="card-header">
      <h2 class="card-title">{$dict.compare.commandsTitle}</h2>
    </div>
    <div class="card-body stack">
      <div>
        <div class="card-header" style="padding-left:0;padding-right:0;border:0;padding-bottom:.35rem">
          <h3 class="card-title">{$dict.compare.guardedTitle}</h3>
          <CopyButton value={$dict.compare.guardedCommand} />
        </div>
        <pre class="code">{ $dict.compare.guardedCommand }</pre>
      </div>
      <div>
        <div class="card-header" style="padding-left:0;padding-right:0;border:0;padding-bottom:.35rem">
          <h3 class="card-title">{$dict.compare.naiveTitle}</h3>
          <CopyButton value={$dict.compare.naiveCommand} />
        </div>
        <pre class="code">{ $dict.compare.naiveCommand }</pre>
      </div>
    </div>
  </section>

  <section class="card" aria-busy={liveLoading}>
    <div class="card-header">
      <div>
        <h2 class="card-title">{$dict.compare.liveRunsTitle}</h2>
        <p class="card-subtitle">{$dict.compare.liveRunsNote}</p>
      </div>
      <button type="button" class="btn btn-sm" on:click={() => void loadLive()} disabled={liveLoading}>
        {$dict.common.refresh}
      </button>
    </div>

    {#if liveError && liveRuns.length === 0}
      <ErrorState error={liveError} retry={() => void loadLive()} />
    {:else if liveLoading && liveRuns.length === 0}
      <div class="card-body"><LoadingBlock rows={4} /></div>
    {:else if liveRuns.length === 0}
      <EmptyState title={$dict.compare.noLiveRunsTitle} body={$dict.compare.noLiveRunsBody} />
    {:else}
      <div class="card-body" style="padding:0">
        <!-- svelte-ignore a11y_no_noninteractive_tabindex -->
        <div class="table-wrap" tabindex="0" role="region" aria-label={$dict.compare.liveRunsTitle}>
          <table class="strip-board">
            <caption class="visually-hidden">{$dict.compare.liveRunsTitle}</caption>
            <thead>
              <tr>
                <th scope="col">{$dict.compare.route}</th>
                <th scope="col">{$dict.compare.runMode}</th>
                <th scope="col" class="num">{$dict.compare.runSeed}</th>
                <th scope="col" class="num">{$dict.compare.sent}</th>
                <th scope="col" class="num">{$dict.compare.accepted}</th>
                <th scope="col" class="num">{$dict.compare.duplicates}</th>
                <th scope="col" class="num">{$dict.compare.serverErrors}</th>
                <th scope="col">{$dict.compare.latency}</th>
                <th scope="col">{$dict.compare.runWhen}</th>
              </tr>
            </thead>
            <tbody>
              {#each liveRuns as run (run.id)}
                <tr class="strip-row">
                  <td>
                    <span class="badge" data-tone={run.route === "guarded" ? "success" : "violet"}>
                      {run.route === "guarded" ? $dict.compare.guardedRoute : $dict.compare.naiveRoute}
                    </span>
                  </td>
                  <td class="mono">{run.mode}</td>
                  <td class="num mono">{formatNumber(run.seed, $locale)}</td>
                  <td class="num">{formatNumber(run.sent, $locale)}</td>
                  <td class="num">{formatNumber(run.accepted, $locale)}</td>
                  <td class="num">{formatNumber(run.duplicates, $locale)}</td>
                  <td class="num">{formatNumber(run.server_errors, $locale)}</td>
                  <td class="mono">
                    {formatNumber(run.p50_ms, $locale)} / {formatNumber(run.p95_ms, $locale)} / {formatNumber(
                      run.p99_ms,
                      $locale,
                    )} ms
                  </td>
                  <td><TimeAgo iso={run.created_at} /></td>
                </tr>
              {/each}
            </tbody>
          </table>
        </div>
      </div>
    {/if}
  </section>

  <section class="card">
    <div class="card-header">
      <div>
        <h2 class="card-title">{$dict.compare.referenceTitle}</h2>
        <p class="card-subtitle">{$dict.compare.referenceNote}</p>
      </div>
    </div>
    <div class="card-body" style="padding:0">
      <!-- svelte-ignore a11y_no_noninteractive_tabindex -->
      <div class="table-wrap" tabindex="0" role="region" aria-label={$dict.compare.referenceTitle}>
        <table class="strip-board">
          <caption class="visually-hidden">{$dict.compare.referenceTitle}</caption>
          <thead>
            <tr>
              <th scope="col">{$dict.compare.route}</th>
              <th scope="col" class="num">{$dict.compare.sent}</th>
              <th scope="col" class="num">{$dict.compare.accepted}</th>
              <th scope="col" class="num">{$dict.compare.duplicates}</th>
              <th scope="col" class="num">{$dict.compare.serverErrors}</th>
              <th scope="col">{$dict.compare.latency}</th>
            </tr>
          </thead>
          <tbody>
            {#each reference as row}
              <tr class="strip-row">
                <td>
                  <span class="badge" data-tone={row.route === "guarded" ? "success" : "violet"}>
                    {row.route === "guarded" ? $dict.compare.guardedRoute : $dict.compare.naiveRoute}
                  </span>
                </td>
                <td class="num">{row.sent}</td>
                <td class="num">{row.accepted}</td>
                <td class="num">{row.duplicates}</td>
                <td class="num">{row.errors}</td>
                <td class="mono">{row.latency}</td>
              </tr>
            {/each}
          </tbody>
        </table>
      </div>
    </div>
  </section>

  <div class="grid-2">
    <section class="card">
      <div class="card-header">
        <h2 class="card-title">{$dict.compare.whyTitle}</h2>
      </div>
      <div class="card-body">
        <p style="margin:0">{$dict.compare.whyBody}</p>
      </div>
    </section>

    <section class="card">
      <div class="card-header">
        <h2 class="card-title">{$dict.compare.liveTitle}</h2>
        <span class="badge badge-neutral">{$dict.common.readOnly}</span>
      </div>
      <div class="card-body">
        <p style="margin:0">{$dict.compare.liveBody}</p>
      </div>
    </section>
  </div>
</div>
