# Portfolio Observatory - GitHub Pages edition

This is the **existing 20-holding interactive portfolio dashboard**, configured for **GitHub Pages only**. No local app, desktop processes, paid server, manual CSV imports or API key. **This is not hosted yet**; it becomes a website when you publish it to your GitHub account. The entire 20-holding snapshot and research are already in `index.html`.

## Three setup steps (once)

1. **Create a new GitHub repository**. On a free plan you can use a public repository. **Privacy warning:** GitHub Pages is a **public website**, even if the repository itself is private on a qualifying plan; the HTML includes the names, amounts, costs and quantities of your personal positions. Only publish if you accept that these details will be visible to anyone who knows the URL (and possibly indexed). GitHub Pages does *not* provide a private-password feature.
2. **Unzip this package, upload its contents** to the root of the new repository on the `main` branch, including the hidden `.github/workflows/publish.yml` file. Do not upload the ZIP itself as a single file. Your repository must show `index.html`, `.github/workflows/publish.yml`, `tools/` and `data/`. You do not need to install Go or Python; GitHub installs the tools inside its own Actions runner.
3. In the repository, go to **Settings > Pages > Build and deployment > Source > GitHub Actions**. Open **Actions > Update Portfolio Observatory** to confirm a run succeeded; if the first deployment happened before Pages was enabled, click **Run workflow** once. Then open the address displayed under **Settings > Pages**, normally `https://YOUR_USERNAME.github.io/YOUR_REPOSITORY/`.

From then on, just use that link. GitHub runs the refresh job automatically every day at **07:17, 13:17 and 20:17 UTC**, and on each push to `main`. Data appears in the website without copying files or refreshing it manually; when already open, the browser checks for changes every 15 minutes or when the tab becomes active.

**There is no way for a purely static `index.html` to fetch reliably from the complete set of market data providers** because browser cross-origin restrictions block unofficial historical endpoints. GitHub Actions is the lightweight, no-server alternative included in this repository. `index.html` *alone* works as an interactive snapshot but cannot perform the automatic market-history updates.

## What's refreshed

- Up to ten years of split- and dividend-adjusted historical prices for all 20 supplied securities where the provider makes them available; **no guaranteed 20/20 coverage**.
- USD/GBP and EUR/GBP historical exchange rates and conversion to GBP. Pence versus pounds are handled explicitly. Corporate-action continuity for Alzchem is explicitly checked.
- GBP global equity (SWDA.L) and bond (IGLH.L) **ETF proxies**, and Bank of England SONIA cash return proxy.
- Real-yield, Treasury-yield, credit-spread, oil-price, dollar-index and VIX time series from FRED.
- Official headline feeds from the Federal Reserve, ECB, Bank of England and UK Ministry of Defence.
- Live site displays data refresh date and all source errors; past successful cache is kept when a feed fails, if the GitHub cache remains available.

The browser performs your portfolio correlations, risk statistics, attribution approximations, optimiser, return charts, scenarios and labs using the actual available histories. It does not infer investor transactions or reconstruct your historical account return. The exact **MSCI World Net Total Return GBP** is a licensed index and is **not** supplied: SWDA.L is labelled as a proxy. The initial **16 November 2026** holdings, costs and research remain fixed; closing market marks use those quantities without assuming trades. Qualitative investment research and catalysts are not automatically re-underwritten from new announcements.

## Availability, data rights and limitations

- **A public Pages link is not private.** Your holdings and research are visible to anyone who opens it. Do not add credentials, broker statements, access tokens or non-public company material.
- The Yahoo Finance chart endpoint is undocumented/unofficial and may block scheduled GitHub runners, throttle requests, alter symbols, change data, or impose usage restrictions. No data vendor has authorised or guaranteed access. Official sources may also fail. If no provider data are available, analytics that require observations stay blank rather than showing invented output. **Successful live downloads have not been verified in the build environment.**
- GitHub Actions scheduled jobs can run late, be skipped under high load, or be automatically disabled after **60 days of repository inactivity** in public repositories. You can re-enable the workflow from the Actions tab if GitHub disables it. This is a GitHub policy limitation, not a hidden server charge.
- GitHub Pages schedules and limits depend on your plan and account policy. This package uses no separate hosting provider and no stored secrets.
- Published market history is public alongside the website. GitHub's Actions cache is used to retain historical data on transient vendor failures; a cleared or expired cache removes that fallback.
- No trading, broker sync, price streaming, order routing or guaranteed intraday quotes. Historical return calculations use end-of-day prices; new closes depend on provider timeliness.

## Files

- `index.html`: the **complete interactive application** (HTML/CSS/JS, no CDN).
- `.github/workflows/publish.yml`: refresh and publish the site automatically, on GitHub's infrastructure.
- `tools/export.go`, `tools/fetch.go`, `tools/data/provider_map.json`, `tools/go.mod`: data collector. Standard Go library only. No API keys, Python packages or separate hosting.
- `data/market.json`: empty-but-truthful initial placeholder (replaced by the workflow after its first run).
- `.nojekyll`: no Jekyll processing needed.

**If a calculation says a history is missing:** open the site's **Sources & Methodology / Automatic GitHub Pages market data** panel. It shows the last retrieval date and per-symbol provider errors. Do not assume a completed GitHub Actions job implies a complete, error-free market feed.
