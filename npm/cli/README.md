# @revenuecat/cli

The [RevenueCat CLI](https://github.com/RevenueCat/revenuecat-cli), installable via npm.

```bash
npx @revenuecat/cli --help
npx @revenuecat/cli login
npx @revenuecat/cli skills install
```

This package is a thin launcher: it selects the prebuilt `rc` binary for your
platform (published as `@revenuecat/cli-<platform>-<arch>` optional
dependencies) and executes it directly. The CLI itself is a native Go binary —
Node is only used to dispatch.

Also available via Homebrew (`brew install RevenueCat/tap/rc`) and
[GitHub Releases](https://github.com/RevenueCat/revenuecat-cli/releases).
