# Contributing

Thanks for helping. Small fixes can go straight to a pull request; for anything larger, open an issue first so we can agree on the approach.

## Setup

```sh
git clone https://github.com/ysinghc/snowflake-lite
cd snowflake-lite
go vet ./...
go test -race ./...
```

Go 1.23 or newer. The module has no third-party dependencies, and the core `snowflake` package should keep it that way.

## Pull requests

1. Branch from `main` and keep the change focused.
2. Match the surrounding code's style and comment density.
3. Add or update tests for any behavior change. Run `go test -race ./...`.
4. Explain what changed and why in the description.

## Compatibility

The bit layout and epoch of the core package are a compatibility promise: changing them invalidates existing IDs, so they only change in a new major version.

## Documentation site

The site lives in `site/`. Pages are fragments in `site/content/`; `site/build.sh` builds the WebAssembly playground, the generated code map and the HTML. Run `wrangler dev` from `site/` to preview.

## Reporting security issues

Do not open a public issue. See [SECURITY.md](SECURITY.md).

By contributing you agree your contribution is licensed under the [MIT License](LICENSE).
