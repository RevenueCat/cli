# Changelog

All notable changes to the RevenueCat CLI are documented here. This project follows [Keep a Changelog](https://keepachangelog.com) and [Semantic Versioning](https://semver.org).

## [0.1.0] - 2026-08-25

Initial public release of `rc`, the RevenueCat command line interface. It's built to be driven equally well by people and by AI coding agents.

### Added
- **One-command setup**: `rc setup` guides RevenueCat setup for an app, and `rc setup apple` / `rc setup google` configure App Store Connect and Google Play credentials locally, with no manual key downloads or console clicking.
- **Project management**: create and manage projects, apps, products, entitlements, offerings, and packages.
- **Customers and subscriptions**: inspect customers, grant and revoke entitlements, simulate purchases, and manage subscriptions.
- **Paywalls**: list and publish paywalls, and generate or edit them with AI (`rc paywalls generate` / `rc paywalls edit`).
- **Rico**: an interactive AI assistant (`rc rico`) with streaming output and tool approvals.
- **Charts**: view subscription metrics as interactive terminal charts.
- **Store-state plans**: review and apply App Store / Google Play catalog changes as auditable plans (`rc products store plan` / `apply`).
- **Agent-native interface**: stable `--json` output, `--no-input` for CI, full command and flag discovery via `rc commands` and `rc schema`, installable AI Toolkit skills (`rc skills install`), and raw API access with `rc api`.
- **Authentication**: browser OAuth (`rc auth login`), API-key auth, and headless account signup.
- **Profiles**: separate credentials per environment with `--profile`.

### Install
- Homebrew: `brew install RevenueCat/tap/rc`
- npm: `npm install -g @revenuecat/cli`, or run without installing via `npx @revenuecat/cli`
- Prebuilt binaries on the [releases page](https://github.com/RevenueCat/revenuecat-cli/releases)

[0.1.0]: https://github.com/RevenueCat/revenuecat-cli/releases/tag/v0.1.0
