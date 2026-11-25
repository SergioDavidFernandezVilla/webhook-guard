<script lang="ts">
  import { onMount } from "svelte";
  import { getCapabilities } from "../lib/api";
  import { dict, locale, setLocale, type Dict, type Locale } from "../i18n";
  import { applyTheme, detectTheme, type Theme } from "../lib/theme";

  export let initialPath = "";

  let path = initialPath;
  let theme: Theme = "light";
  let demoEnabled = false;

  const links: { href: string; key: keyof Dict["nav"] }[] = [
    { href: "/", key: "overview" },
    { href: "/charges", key: "charges" },
    { href: "/events", key: "events" },
    { href: "/late", key: "late" },
    { href: "/reconciliation", key: "reconciliation" },
    { href: "/compare", key: "compare" },
    { href: "/demo", key: "demo" },
  ];

  $: visibleLinks = links.filter((link) => link.key !== "demo" || demoEnabled);
  $: activeKey =
    links.find((link) =>
      link.href === "/"
        ? path === "/"
        : path === link.href || path.startsWith(`${link.href}/`),
    )?.key ?? "overview";

  $: if (typeof document !== "undefined" && path) {
    document.documentElement.lang = $locale;
    document.title = `${$dict.nav[activeKey]} · ${$dict.common.appName}`;
  }

  onMount(() => {
    path = window.location.pathname;
    theme = detectTheme();
    applyTheme(theme);
    requestAnimationFrame(() => {
      document
        .querySelector('.nav-link[aria-current="page"]')
        ?.scrollIntoView({ behavior: "auto", block: "nearest", inline: "center" });
    });
    void getCapabilities()
      .then((caps) => {
        demoEnabled = caps.demo_enabled;
      })
      .catch(() => {
        demoEnabled = false;
      });
  });

  function pick(next: Locale): void {
    setLocale(next);
  }

  function switchTheme(next: Theme): void {
    theme = next;
    applyTheme(next);
  }
</script>

<header class="topbar">
  <a class="brand" href="/" aria-label={$dict.common.appName}>
    <span class="brand-mark" aria-hidden="true">
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6">
        <rect x="2.5" y="3.5" width="19" height="7" rx="1.2" />
        <rect x="2.5" y="13.5" width="19" height="7" rx="1.2" />
        <path d="M6.5 7h.01M6.5 17h.01" stroke-width="2.4" stroke-linecap="round" />
        <path d="M10 5v4M10 15v4" opacity=".45" />
        <path d="M14 5v4M14 15v4" opacity=".45" />
        <path d="M18 5v4M18 15v4" opacity=".45" />
      </svg>
    </span>
    <span class="brand-name">{$dict.common.appName}</span>
    <span class="brand-sub">{$dict.common.tagline}</span>
  </a>

  <div class="nav-scroll">
    <nav aria-label={$dict.nav.main}>
      <ul class="nav-list">
        {#each visibleLinks as link}
          <li>
            <a
              class="nav-link"
              href={link.href}
              aria-current={activeKey === link.key ? "page" : undefined}
            >
              {$dict.nav[link.key]}
            </a>
          </li>
        {/each}
      </ul>
    </nav>
  </div>

  <div class="topbar-tools">
    <div class="toggle" role="group" aria-label={$dict.common.language}>
      <button type="button" aria-pressed={$locale === "en"} on:click={() => pick("en")}>EN</button>
      <button type="button" aria-pressed={$locale === "es"} on:click={() => pick("es")}>ES</button>
    </div>
    <div class="toggle" role="group" aria-label={$dict.common.theme}>
      <button type="button" aria-pressed={theme === "light"} on:click={() => switchTheme("light")}>
        {$dict.common.light}
      </button>
      <button type="button" aria-pressed={theme === "dark"} on:click={() => switchTheme("dark")}>
        {$dict.common.dark}
      </button>
    </div>
    <span class="local-badge"><span class="live-dot"></span>{$dict.common.local}</span>
  </div>
</header>
