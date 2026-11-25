<script lang="ts">
  import { onMount } from "svelte";
  import {
    ApiError,
    getCapabilities,
    postDemo,
    type Capabilities,
    type DemoResult,
  } from "../lib/api";
  import { dict } from "../i18n";
  import PageHeader from "./PageHeader.svelte";
  import ErrorState from "./ErrorState.svelte";
  import LoadingBlock from "./LoadingBlock.svelte";

  const SCENARIOS = [
    "accepted",
    "duplicate",
    "invalid_transition",
    "invalid_signature",
    "expired",
  ] as const;
  type Scenario = (typeof SCENARIOS)[number];
  const PRIMARY: Scenario = "accepted";

  let capabilities: Capabilities | null = null;
  let loadingCaps = true;
  let capsError: unknown = null;
  let running: Scenario | null = null;
  let results: Partial<Record<Scenario, DemoResult>> = {};
  let errors: Partial<Record<Scenario, string>> = {};

  $: labels = {
    accepted: { title: $dict.demo.acceptedTitle, body: $dict.demo.acceptedBody },
    duplicate: { title: $dict.demo.duplicateTitle, body: $dict.demo.duplicateBody },
    invalid_signature: {
      title: $dict.demo.invalidSignatureTitle,
      body: $dict.demo.invalidSignatureBody,
    },
    expired: { title: $dict.demo.expiredTitle, body: $dict.demo.expiredBody },
    invalid_transition: {
      title: $dict.demo.invalidTransitionTitle,
      body: $dict.demo.invalidTransitionBody,
    },
  };

  async function loadCapabilities(): Promise<void> {
    loadingCaps = true;
    capsError = null;
    try {
      capabilities = await getCapabilities();
    } catch (err) {
      capsError = err;
    } finally {
      loadingCaps = false;
    }
  }

  async function run(scenario: Scenario): Promise<void> {
    running = scenario;
    errors = { ...errors, [scenario]: undefined };
    try {
      const result = await postDemo(scenario);
      results = { ...results, [scenario]: result };
      requestAnimationFrame(() => {
        const element = document.getElementById(`demo-${scenario}`);
        if (!element) return;
        const reduce = window.matchMedia("(prefers-reduced-motion: reduce)").matches;
        element.scrollIntoView({ behavior: reduce ? "auto" : "smooth", block: "start" });
      });
    } catch (err) {
      const message =
        err instanceof ApiError
          ? ($dict.errors[err.code as keyof typeof $dict.errors] ?? err.message)
          : err instanceof Error
            ? err.message
            : $dict.errors.unknown;
      errors = { ...errors, [scenario]: message };
    } finally {
      running = null;
    }
  }

  function stepTone(status: number): string {
    return status >= 400 ? "danger" : "success";
  }

  onMount(() => {
    void loadCapabilities();
  });
</script>

<PageHeader title={$dict.demo.title} description={$dict.demo.subtitle} code={$dict.demo.bay}>
  <span class="badge" data-tone={capabilities?.demo_enabled ? "success" : "neutral"}>
    {$dict.common.demoMode}: {capabilities?.demo_enabled ? $dict.common.yes : $dict.common.no}
  </span>
</PageHeader>

{#if loadingCaps}
  <div class="card"><div class="card-body"><LoadingBlock rows={5} /></div></div>
{:else if capsError}
  <ErrorState error={capsError} retry={() => void loadCapabilities()} />
{:else if !capabilities?.demo_enabled}
  <div class="stack">
    <section class="card">
      <div class="card-header">
        <h2 class="card-title">{$dict.demo.disabledTitle}</h2>
      </div>
      <div class="card-body stack">
        <p style="margin:0">{$dict.demo.disabledBody}</p>
        <pre class="code"># .env
DEMO_MODE=true

docker compose up -d --build app</pre>
      </div>
    </section>
    <div class="callout">
      <p style="margin:0">{$dict.demo.note}</p>
    </div>
  </div>
{:else}
  <div class="stack">
    <div class="callout">
      <p style="margin:0">{$dict.demo.note}</p>
    </div>

    <section class="card">
      <div class="card-header">
        <h2 class="card-title">{$dict.demo.scenariosTitle}</h2>
        <span class="meta-line">{$dict.demo.rateLimited}</span>
      </div>
      <ul class="scenario-list">
        {#each SCENARIOS as scenario}
          <li class="scenario-row">
            <div class="scenario-copy">
              <h3 class="card-title">{labels[scenario].title}</h3>
              <p>{labels[scenario].body}</p>
            </div>
            <button
              type="button"
              class:btn-primary={scenario === PRIMARY}
              class="btn"
              disabled={running !== null}
              on:click={() => void run(scenario)}
            >
              {running === scenario ? $dict.demo.running : $dict.demo.run}
            </button>
          </li>
        {/each}
      </ul>
    </section>

    <div role="status" aria-live="polite" class="stack">
      {#each SCENARIOS as scenario}
        {#if results[scenario]}
          {@const result = results[scenario]!}
          <section class="demo-result" id={`demo-${scenario}`}>
            <div class="card-header">
              <div>
                <h2 class="card-title">{$dict.demo.resultTitle} · {labels[scenario].title}</h2>
                <p class="card-subtitle mono">{scenario}</p>
              </div>
            </div>
            <div class="card-body stack">
              <dl class="kv">
                <dt>{$dict.demo.eventId}</dt>
                <dd>
                  <a class="mono" href={`/events?event=${encodeURIComponent(result.event_id)}`}>
                    {result.event_id}
                  </a>
                </dd>
                <dt>{$dict.demo.chargeId}</dt>
                <dd>
                  <a class="mono" href={`/charges?charge=${encodeURIComponent(result.charge_id)}`}>
                    {result.charge_id}
                  </a>
                </dd>
              </dl>
              <div>
                <h3 class="card-title" style="margin-bottom:.4rem">{$dict.demo.steps}</h3>
                <!-- svelte-ignore a11y_no_noninteractive_tabindex -->
<div class="table-wrap" tabindex="0">
                  <table class="table">
                    <thead>
                      <tr>
                        <th scope="col">#</th>
                        <th scope="col">{$dict.demo.httpStatus}</th>
                        <th scope="col">{$dict.demo.result}</th>
                      </tr>
                    </thead>
                    <tbody>
                      {#each result.steps as step, index}
                        <tr>
                          <td class="mono">{index + 1}</td>
                          <td>
                            <span class="badge" data-tone={stepTone(step.http_status)}>
                              {step.http_status || "—"}
                            </span>
                          </td>
                          <td>{step.status ?? step.error ?? $dict.common.notAvailable}</td>
                        </tr>
                      {/each}
                    </tbody>
                  </table>
                </div>
              </div>
              <div class="meta-line">
                <a class="btn btn-sm" href={`/charges?charge=${encodeURIComponent(result.charge_id)}`}>
                  {$dict.demo.openCharge}
                </a>
                <a class="btn btn-sm" href={`/events?event=${encodeURIComponent(result.event_id)}`}>
                  {$dict.demo.openEvent}
                </a>
              </div>
            </div>
          </section>
        {/if}
        {#if errors[scenario]}
          <div class="callout" data-tone="danger">
            <p style="margin:0"><strong>{labels[scenario].title}:</strong> {errors[scenario]}</p>
          </div>
        {/if}
      {/each}
    </div>
  </div>
{/if}

<style>
  .scenario-list {
    margin: 0;
    padding: 0;
    list-style: none;
  }

  .scenario-row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 1rem;
    padding: 0.75rem 1rem;
    border-bottom: 1px solid var(--slip-edge);
  }

  .scenario-row:last-child {
    border-bottom: 0;
  }

  .scenario-copy {
    min-width: 0;
  }

  .scenario-copy p {
    margin: 0.15rem 0 0;
    color: var(--press-muted);
    font-size: 0.76rem;
  }

  @media (max-width: 640px) {
    .scenario-row {
      align-items: flex-start;
      flex-direction: column;
    }
  }
</style>
