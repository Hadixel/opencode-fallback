# ocf — opencode-fallback

Runs OpenCode with a chain of models and moves on when a provider says its free tier is exhausted.

## Why

OpenCode's free tier is enforced server-side, in two layers:

- **Admission.** Free models are only accepted from the real OpenCode client — its TLS
  fingerprint is the ticket. Anything else gets
  `FreeTierError: OpenCode's free tier can only be used from within OpenCode`.
- **Quota.** The free allowance is counted per **source IP**.
  `Free usage limit reached. Subscribe to OpenCode Go` is the quota, and it does not follow
  your account: two different Console accounts on the same exit hit the same wall, while the
  same client on a different network can work immediately.

There is no client-side switch that resets it. Changing the TLS fingerprint gets you thrown
out of the gate, and rotating VPN exits only helps when the new exit has an unspent bucket.
What you *can* do is stop treating the limit as an error: try the next model you are entitled
to. `ocf` does that.

## Install

```sh
git clone https://github.com/Hadixel/opencode-fallback
cd opencode-fallback
go build -o ~/.local/bin/ocf .
```

Requires `opencode` on `PATH`.

## Use

```sh
# free models first, then your own working model
ocf run --chain "opencode/deepseek-v4.1-flash-free,opencode/glm-5.3-flash-free,9router/oc-hq/ocg/deepseek-v4.1-flash" \
  "explain this stack trace"

# set the chain once
export OCF_CHAIN="opencode/deepseek-v4.1-flash-free,9router/oc-hq/ocg/deepseek-v4.1-flash"
ocf run "summarise the last commit"

# where am I, and is the free tier open?
ocf check
```

```
$ ocf check
egress: 45.82.161.235
  opencode/deepseek-v4.1-flash-free        ok        2.1s
  opencode/glm-5.3-flash-free              limited   1.8s
  opencode/big-pickle                      ok        1.2s
```

`ocf run` streams the model's output; when a model is rate-limited or unavailable it prints a
one-line banner and moves to the next entry.

Exit codes: `0` answered, `1` hard failure, `2` usage, `3` every model in the chain
limited/unavailable.

## What it does not do

No TLS-fingerprint spoofing, no exit-IP rotation, no account juggling. The first is the
admission gate (spoofing it locks you out), the rest is circumventing a quota. `ocf` respects
the limit and routes around it to something you are entitled to.

## License

MIT
