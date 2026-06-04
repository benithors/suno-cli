# Suno CLI

Go source for `suno-pp-cli`, a compact Suno terminal client with local sync,
search, downloads, generation, and hCaptcha-aware queueing.

## Build

```bash
make build
ln -sf "$PWD/bin/suno-pp-cli" ~/.local/bin/suno-pp-cli
```

Or install directly:

```bash
go install github.com/benithors/suno-cli-only/cmd/suno-pp-cli@latest
```

## Use

```bash
suno-pp-cli auth login --chrome
suno-pp-cli doctor
suno-pp-cli sync --full
suno-pp-cli grep "chorus" --json
```

Generate with a local fallback queue:

```bash
suno-pp-cli generate describe "bright indie pop about rebuilding a CLI" \
  --no-captcha \
  --queue-on-captcha \
  --wait
```

Retry a queued request:

```bash
suno-pp-cli generate queue list
suno-pp-cli generate queue retry <queue_id> --wait --download ./out
```

The CLI queues challenged requests; it does not bypass hCaptcha, scrape browser
challenge state, or store hCaptcha tokens.

## Dev

```bash
go test ./...
make build
```

Main package: `./cmd/suno-pp-cli`
