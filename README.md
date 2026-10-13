# 🕸️ tsctl

Administer a Tailscale tailnet from your terminal: devices, tags, key expiry, the
policy file, auth keys, and any other API endpoint. Built with Go and Cobra.

The `tailscale` CLI controls **the machine it runs on**. tsctl does what the
**admin console** does, through the [Tailscale API](https://tailscale.com/api).

## 📦 Installation

```bash
brew install dorkitude/tap/tsctl
# or
go install github.com/dorkitude/tsctl@latest
```

## 🔑 Credentials

tsctl uses the first of these it finds, and never prints a secret (not in output,
errors, `--json`, or `--debug`):

| Source | Setup |
| --- | --- |
| `TS_API_KEY` | an API access token (`tskey-api-...`) |
| `TS_OAUTH_CLIENT_ID` + `TS_OAUTH_CLIENT_SECRET` | an OAuth client; scopes decide what works |
| **exe.dev workload identity** (keyless) | a Tailscale WIF integration attached to the VM, at `TSCTL_WIF_URL` (default `https://tailscale-wif.int.exe.xyz`) |

**Keyless on exe.dev.** On an exe.dev VM, tsctl asks the integration for a
short-lived OIDC token and exchanges it at Tailscale's
`/api/v2/oauth/token-exchange` for an API token. Nothing secret is stored on the VM.
Setup ([exe.dev docs](https://exe.dev/docs/integrations-tailscale-wif.md)):

1. `ssh exe.dev integrations add wif --name tailscale-wif --audience api.tailscale.com/pending --consumer tailscale --attach vm:<vm>`
   prints an Issuer and a Subject.
2. In Tailscale: Settings → Trust credentials → Add → OpenID Connect. Paste the
   Issuer (custom) and Subject. Tailscale shows a Client ID.
3. `ssh exe.dev integrations edit tailscale-wif --audience api.tailscale.com/<CLIENT_ID> --metadata=client_id=<CLIENT_ID>`
4. `tsctl auth status` checks it.

## 🚀 Usage

```bash
tsctl auth status                       # which credential, and does it work
tsctl devices list                      # name, IP, OS, owner/tags, online, key expiry
tsctl devices get muse --json
tsctl devices tag muse tag:muse         # replace tags (tag must be in tagOwners)
tsctl devices tag muse --add tag:agent  # or edit the set
tsctl devices expiry cockpit --disable  # turn node key expiry off
tsctl devices authorize new-box
tsctl devices delete old-box --yes

tsctl policy get > policy.hujson        # HuJSON, comments kept
tsctl policy validate -f policy.hujson  # Tailscale validates it and runs its "tests"
tsctl policy diff -f policy.hujson
tsctl policy set -f policy.hujson --dry-run
tsctl policy set -f policy.hujson       # diff, validate, then save with If-Match

tsctl keys list
tsctl keys create --tag tag:muse --expiry 1h --key-out /tmp/key   # secret goes to a 0600 file
tsctl keys delete <id>

tsctl api GET /tailnet/{tailnet}/settings
tsctl api PATCH /tailnet/{tailnet}/settings -d '{"devicesApprovalOn":true}'
```

A DEVICE can be its MagicDNS name, hostname, Tailscale IP, ID or node ID.

## 🛡️ Safety

- `--read-only` (or `TSCTL_READONLY=1`) refuses every write before it's sent.
  `policy validate` still works: it saves nothing.
- `policy set` always validates first (so the policy's `tests` must pass) and saves
  with `If-Match`, so it never overwrites an edit made meanwhile in the console.
- `devices delete` needs `--yes`.
- Agents: use `--json` on reads.

Other flags: `--tailnet` (default `-`, the credential's own tailnet; also
`TSCTL_TAILNET`), `--debug` (method, path and status only).

## 🧑‍💻 Development

```bash
go test ./...      # fake Tailscale API; no network or credentials
go build -o tsctl .
```

Releases: push a `vX.Y.Z` tag. GitHub Actions runs the tests and GoReleaser
attaches binaries. Then update `Formula/tsctl.rb` in `dorkitude/homebrew-tap`
(new tag URL and sha256).

## License

MIT
