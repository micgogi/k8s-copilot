# k8s-copilot

An LLM-powered debugging assistant for Kubernetes microservices. Engineers facing a misbehaving service paste a service name (or run the CLI inside their cluster); the copilot pulls logs, traces, recent deploys, and config, then an agent diagnoses root cause and suggests a fix.

This file is the primary context document for Claude when working in this repo. Read it first.

## Vision (one sentence)

> "When prod is on fire, an SRE runs `kcp diagnose <service>` and gets back a ranked list of likely root causes with evidence — in 60 seconds."

## Why this exists

- **Real problem**: K8s/Istio debugging is the slowest, most-senior-engineer-bound workflow in modern infra. Junior on-call → 45 min just to find the right logs.
- **Why an LLM helps**: incidents are *unstructured* (logs, traces, deploys, configs) but diagnosable. LLMs are good at correlating signal across noisy text sources.
- **Why open-source**: trust matters for tools that touch prod. Demo-able on a sample cluster anyone can spin up.

## Target user

- Primary: SRE / platform engineer on-call
- Secondary: backend engineer debugging their own service in staging
- NOT: data scientists, ML researchers, end-customer-facing dashboards

## North-star demo

A 2-minute Loom:
1. Sample Istio Bookinfo cluster is running. Reviewer service is misbehaving (5xx spike).
2. Run `kcp diagnose reviews`.
3. CLI streams agent reasoning: "Checking pod status... istio-proxy errors... recent deploy 12 min ago..."
4. Final output: ranked root causes with cited evidence + suggested `kubectl` commands to fix.

If a viewer doesn't say "I want this" after watching, the project has failed.

## Architecture

```
┌──────────────┐      ┌────────────────┐      ┌──────────────┐
│  kcp CLI     │ ───▶ │  agent loop    │ ───▶ │  Claude API  │
│  (Go)        │      │  (Go)          │      │  (tool use)  │
└──────────────┘      └────────────────┘      └──────────────┘
       │                       │
       ▼                       ▼
  k8s client            tool implementations:
  (client-go)             - kubectl get/describe/logs
                          - istioctl analyze
                          - Prometheus query
                          - git log (recent deploys)

┌──────────────┐
│  web UI      │   reads incident history + agent traces from
│  (Next.js)   │   Postgres; useful for review/eval, not live debug
└──────────────┘
```

Agent design: tool-use loop with Claude. Tools are read-only (no `kubectl apply` etc — copilot suggests, human applies). Prompt caching is mandatory for the system prompt + tool definitions.

## Stack

- **CLI**: Go 1.22+, `client-go` for k8s, `cobra` for commands
- **LLM**: Claude API (use latest Sonnet — `claude-sonnet-4-6` — for the agent loop; consider Haiku for cheap classifiers)
- **Web UI**: Next.js 14 (App Router), TypeScript, Tailwind, shadcn/ui
- **Storage**: Postgres (incident traces, eval history); SQLite for local dev
- **Demo cluster**: Istio Bookinfo + a fault-injection script in `demo-cluster/`
- **Evals**: simple JSONL golden set in `evals/`; pytest-style runner in Go

## Repo layout

```
cli/            Go CLI + agent loop
web/            Next.js dashboard
evals/          Golden incident scenarios + eval runner
demo-cluster/   K8s manifests + fault-injection scripts for the demo
scripts/        One-off shell scripts (cluster bootstrap, etc.)
docs/           Design notes, blog post drafts
```

## Coding conventions

- **Go**: standard library first; `cobra` for CLI; `slog` for logging. No global state. Errors wrapped with `fmt.Errorf("doing X: %w", err)`.
- **TypeScript**: strict mode on; no `any`. Server Components by default; Client Components only when needed.
- **Comments**: only when WHY is non-obvious. No "what" comments.
- **Tests**: focus on the agent loop and tool implementations. UI tests later.
- **No premature abstraction**: three concrete cases before extracting an interface.

## Claude API usage rules

- Always use prompt caching for the system prompt and tool definitions (`cache_control: {"type": "ephemeral"}`).
- Default model: `claude-sonnet-4-6`. Use `claude-haiku-4-5-20251001` only for non-reasoning classification.
- Use structured outputs (tool-use with a `submit_diagnosis` tool) for the final answer, not freeform text.
- Log every API call (request, response, latency, tokens) to Postgres for eval analysis.
- Budget: target <$0.10 per diagnosis at steady state. Track this from day one.

## Current phase: Week 1 — Foundations

Goal by end of week 1:
- [ ] Go CLI scaffold with `kcp version` + `kcp diagnose --help`
- [ ] Demo cluster boots locally (kind + Istio + Bookinfo)
- [ ] Hello-world Claude API call from Go with prompt caching enabled
- [ ] One real tool: `kubectl get pods` exposed to the agent
- [ ] Single-turn agent run that lists pods and summarizes their state

Out of scope this week: web UI, Postgres, evals, multi-tool reasoning.

## Non-goals (be ruthless)

- ❌ Auto-remediation (writing to the cluster). Read-only forever in v1.
- ❌ Multi-cluster federation. One cluster, one context.
- ❌ Custom model fine-tuning. Prompt + tool use only.
- ❌ Generic "ChatOps" — this is a debugging tool, not a Slack bot.
- ❌ Cloud vendor lock-in. Must run against any conformant k8s cluster.

## Success metrics (track from day one)

- **Eval accuracy**: % of golden incidents where the top-1 root cause matches truth. Target >70% by week 8.
- **Cost per diagnosis**: dollars of Claude API usage. Target <$0.10.
- **Latency**: time from `kcp diagnose` to final answer. Target <60s.
- **Real users**: # of non-author humans who've run `kcp diagnose` against a real cluster. Target 5 by week 6.

## Project context (for me, future Claude)

This is a personal portfolio project for Rahul's FDE applications. Decisions should optimize for:
1. **Demo-ability** — every feature must improve the 2-minute demo
2. **Differentiation** — don't build what already exists (k9s, lens, robusta)
3. **Learning** — favor doing the LLM-native thing even if it's harder
4. **Shipping speed** — 8 weeks to a real public release, not a perfect codebase
