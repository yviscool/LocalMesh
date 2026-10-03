# Phase 0 Git and Network Foundation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use incremental implementation and test-driven development for each task.

**Goal:** Establish a reviewable GitHub-ready Go repository and extend Phase 0 with pairing and command contracts.

**Architecture:** Keep identity, classroom, discovery, pairing, session, and command types in the domain layer. CI validates formatting, vet, race-enabled tests, and vulnerability checks. Network transports will depend on these contracts later.

**Tech Stack:** Go 1.23, standard library testing, GitHub Actions, GitHub CLI.

**Spec:** `docs/ARCHITECTURE.md`, `docs/DOMAIN-MODEL.md`, `docs/ROADMAP.md`.

## Global Constraints

- Discovery never grants authorization.
- Stable IDs must not depend on IP addresses.
- Commands require an explicit target, capability, policy, and result.
- Privileged Windows operations remain outside the domain package.
- Every behavior change includes a focused test and fresh verification evidence.

## Tasks

- [x] Add repository hygiene: `.gitignore`, Makefile, README, contributing rules, CI workflow, and GitHub templates.
- [x] Add pairing request lifecycle with idempotency key and approval/revocation states.
- [x] Add command envelope, retry policy, and per-target result aggregation types.
- [x] Add in-memory discovery registry with stale-observation expiry.
- [x] Add reconnect backoff policy.
- [x] Initialize local Git history and publish the repository through `gh`.
- [ ] Add the 100-agent simulation test.

## Verification

```text
gofmt -l .
go test -race -shuffle=on ./...
go vet ./...
```
