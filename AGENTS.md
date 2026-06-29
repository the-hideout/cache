# AGENTS.md

These instructions are for AI and human agents working in `the-hideout/cache`. They replace the old `.github/copilot-instructions.md`, which was written before the service moved away from Gin/testify and before the repository adopted stricter offline/vendor-first Go workflows.

## Repository Purpose

This repository owns the standalone cache service for Tarkov API traffic.

The service is a small Go HTTP API backed by Redis. It exists to cache GraphQL responses from [`the-hideout/tarkov-api`](https://github.com/the-hideout/tarkov-api) so the Cloudflare Worker side can avoid unnecessary origin work and keep response times low.

The cache service itself runs on an OVH VM. It is fronted by the separate [`the-hideout/ingress`](https://github.com/the-hideout/ingress) stack through Caddy and the shared Docker network named `ingress`.

The upstream `tarkov-api` service lives in Cloudflare and calls `https://cache.tarkov.dev/api/cache`. Treat that URL shape and API contract as production-critical.

Do not clone `the-hideout/tarkov-api` just to inspect behavior. It is a Node project with a large dependency graph, and the preferred inspection path is `gh api` / GitHub web metadata unless the repo owner explicitly says otherwise.

## Current Architecture

The Go module lives in `src/cache`.

The service entrypoint is `src/cache/main.go`.

Runtime configuration is read from `src/cache/config.json` when running inside the Docker image, where it is copied to `/app/config.json`.

Redis is the only backing store. Do not replace Redis or add a second store without an explicit architecture discussion.

The production Docker stack is defined by `docker-compose.yml` plus `docker-compose.production.yml`.

Local development uses `docker-compose.yml` plus `docker-compose.override.yml`.

Production joins the external Docker network named `ingress`; local development publishes `localhost:8080` for the cache API and `localhost:6379` for Redis.

Ingress owns public TLS routing and Basic Auth for protected API traffic. Do not add app-level Basic Auth to this service unless the ingress ownership boundary changes.

## API Contract

Preserve the external wire contract unless the user explicitly approves a breaking change.

`GET /api/cache?key=...` reads a cache value.

`POST /api/cache` writes a cache value.

`GET /health` and `GET /api/health` return text health responses.

On a cache hit, `GET /api/cache?key=...` returns HTTP `200` with a JSON string body, not an object. Example body: `"cached response"`.

On a cache hit, include `X-CACHE-TTL` with the remaining TTL in whole seconds.

On a cache hit, include `Cache-Control: public, max-age=N` where `N` matches the remaining TTL seconds.

On a cache miss, return HTTP `404` with `{"error":"key not found"}` and `Cache-Control: no-store`.

On request validation errors, return HTTP `400` with a JSON error object and `Cache-Control: no-store`.

On server/Redis errors, return HTTP `500` with `{"error":"internal server error"}` and `Cache-Control: no-store`.

The `Cache-Control: no-store` behavior on misses and errors is intentional. It prevents Cloudflare or other intermediaries from accidentally caching negative/error responses.

`POST /api/cache` accepts JSON with `key`, `value`, and optional `ttl`.

`key` must be a non-empty JSON string.

`value` must be a non-empty JSON string. Values that contain JSON text are still stored as strings.

`ttl` is optional. If present, it must be a JSON string containing a base-10 integer greater than zero. Numeric JSON TTLs such as `"ttl": 30` are rejected.

Custom TTL values `<= 0` are rejected with `400`. This is an intentional bug fix from the old behavior, where Redis could treat zero as no expiration.

Custom TTL values greater than `9223372036` seconds are rejected with `400`. This protects the `time.Duration` conversion from overflowing before the TTL reaches Redis.

When `ttl` is absent, the default TTL from `config.json` is used. That default also must be greater than zero.

`GET /api/cache?key=missing&key=existing` uses the first `key` query parameter, matching Go's `URL.Query().Get` behavior and the old observed API behavior.

Wrong methods on `/api/cache` return `404`, preserving the old Gin route behavior.

`HEAD /api/cache?key=...` is treated like the GET path for header probes. This preserves the acceptance suite's `curl -I --request GET` style header checks.

Health success returns status `200` and text body `OK`.

Health failure returns status `503`, text body `Redis connection failed`, and `Cache-Control: no-store`.

Wrong methods on health routes return `404` and `Cache-Control: no-store`.

## Go Implementation Notes

Use the Go standard library HTTP stack. Do not reintroduce Gin for routing or response writing.

Use the Go standard library test stack. Do not reintroduce `testify` for assertions.

The only remaining runtime dependency should be a Redis client plus its small required transitive graph.

The modernization PR retained `github.com/go-redis/redis/v9 v9.0.0-rc.2` because the exact official-client refresh to `github.com/redis/go-redis/v9@v9.21.0` required a networked Go dependency fetch and Socket Firewall did not advertise Go support on this macOS host. Treat the official Redis client migration as a deliberate follow-up dependency update, not as routine cleanup.

The code has a small `CacheStore` boundary around `Ping`, `Get`, `Set`, and `Close`. Keep handler logic testable through that interface.

Production uses a Redis-backed implementation. Unit tests use a fake in-memory store.

The Redis `Get` path pipelines `GET` and `TTL` so a cache hit avoids separate round trips.

If Redis reports a missing key, expired key, or TTL `<= 0`, treat it as a miss rather than returning a cacheable success with an invalid TTL.

Request-scoped Redis contexts/timeouts are intentional: reads should fail quickly, writes should remain inside the upstream worker's write budget, and health checks should be short.

As of the modernization work, read operations use a 4 second timeout, write operations use a 10 second timeout, and health checks use a 2 second timeout.

Avoid global background contexts for request work. Use `r.Context()` plus a timeout.

Do not expand the `CacheStore` interface speculatively. Add methods only when a handler or production path actually needs them.

Keep comments sparse. Add comments only where the code is not self-explanatory.

## Dependency Policy

This repo is intentionally dependency-minimized.

Normal development should be offline and vendor-backed.

Do not run `go mod tidy`, `go mod vendor`, `go get`, or other dependency-mutating commands as part of routine bootstrap, test, lint, build, or Docker build work.

Do not fetch Go dependencies unprotected when Socket Firewall cannot protect the exact Go dependency command on the current host.

Use `script/update-deps` as the intentional networked dependency update path.

`script/update-deps` first checks whether `sfw --help` advertises Go support. If it does not, the script stops unless `ALLOW_UNPROTECTED_GO_DEPS=1` is explicitly set by a human who understands that the Go dependency refresh is unprotected.

Do not set `ALLOW_UNPROTECTED_GO_DEPS=1` silently. Ask first.

Do not add loose or floating dependency constraints. Use exact versions and commit generated vendor changes.

Keep `GOTOOLCHAIN=local` for normal scripts so Go does not silently download another toolchain.

Do not add a `toolchain` directive unless the repo owner explicitly approves raising the toolchain behavior.

Keep `go 1.24` in `go.mod` unless the repo owner explicitly approves a Go language floor change.

The pinned local Go version is currently read from `src/cache/.go-version`.

The committed vendor tree is part of the reviewable source of truth. If a dependency update is approved, regenerate vendor state through the intentional update workflow and inspect the complete diff.

After the modernization PR, the active vendored tree was reduced to Redis plus `github.com/cespare/xxhash/v2` and `github.com/dgryski/go-rendezvous`.

Dependency proof from the modernization work: no Gin/testify references remained in active source/config/manifests, and the vendored tree was reduced to 59 files / 556K in the working tree before PR publication.

## Repository Scripts

Use repo-owned scripts instead of raw commands when they exist.

Start with `script/env` for local Go environment setup. It sets `CACHE_GO_DIR`, `GOTOOLCHAIN=local`, `GOPROXY=off`, `GOSUMDB=off`, `GOFLAGS=-mod=vendor`, repo-local `GOCACHE`, repo-local `GOMODCACHE`, and prepends the pinned goenv binary path when available.

Use `script/bootstrap` to verify the vendored Go dependency graph offline. It runs `go list -mod=vendor -deps ./...` inside `src/cache`.

Use `script/lint` for lightweight lint checks. It checks `gofmt` output for `main.go` and `main_test.go`, and it fails if any workflow `uses:` reference is not pinned to a full 40-character commit SHA.

Use `script/build` for a local static binary build into `bin/cache`.

Use `script/test` for the main Go test gate. It runs race-enabled tests with coverage and count `3` in vendor mode.

Use `script/acceptance` for Docker/Redis acceptance behavior. It requires Docker to be running.

Use `script/update-deps` only when a dependency refresh has been explicitly approved.

The old Copilot instructions said to run `go mod vendor`, `go mod tidy`, and `go mod verify` during setup. That is no longer correct for this repo.

## Testing Notes

Unit tests live in `src/cache/main_test.go` and intentionally use only stdlib `testing`, `httptest`, and small helper functions.

The fake store tests cover config loading, health endpoints, route methods, cache hit/miss/error behavior, POST validation, TTL parsing, cache headers, `no-store` behavior, round-trip JSON string values, healthcheck mode, and TTL fuzz seeds.

Total statement coverage after the modernization work was about 73.6%. The main uncovered areas are live Redis calls and `main`, not the handler contract.

Do not chase 100% coverage by making the code worse. Add coverage where it clarifies behavior or protects a meaningful edge case.

The acceptance suite exercises real Docker/Redis behavior, including default TTL, custom TTL, overwrites, expiration, unicode/special keys and values, light concurrent reads/writes, cache headers, `no-store` on negative responses, and invalid TTL behavior.

`script/acceptance` should be a deterministic assertion suite, not an exploratory probe script. Unexpected statuses, bodies, or headers should increment the failure count and cause a non-zero exit.

Local Docker was not running during the modernization implementation on the developer machine, so the local Docker build was blocked by a missing socket at `/Users/birki/.docker/run/docker.sock`. CI acceptance and the branch deploy later proved the Docker build on the remote runner/host.

## Docker Notes

`src/cache/Dockerfile` builds with committed vendor state only.

The Docker build sets `GOFLAGS=-mod=vendor`, `GOPROXY=off`, `GOSUMDB=off`, and `GOTOOLCHAIN=local`.

The runtime image is `scratch`.

`scratch` does not include `/etc/passwd` or `/etc/group`. If you want human-readable users in a scratch image, create minimal passwd/group files in the builder stage, copy them into the runtime image, and then use `USER nonroot:nonroot`.

The runtime binary is statically built with `CGO_ENABLED=0`.

The runtime user should be the named `nonroot:nonroot` identity, backed by UID/GID `65532`. Prefer the named form in the Dockerfile because it is easier to read and review than raw numeric IDs.

The Docker healthcheck runs `/app/cache healthcheck`; do not add `curl` back into the runtime image just for health checks.

The old `GIN_MODE` compose environment variables were removed because Gin is no longer part of the service.

Every Docker image reference in this project should be pinned to a full `sha256:` digest for reproducible builds.

`script/lint` enforces Docker image digest pins in Dockerfiles and compose files. `FROM scratch` is the only accepted unpinned base because it is Docker's empty sentinel image rather than a registry image manifest.

The Go builder image is pinned as `golang:1.24.3-alpine@sha256:b4f875e650466fa0fe62c6fd3f02517a392123eea85f1d7e69d85f780e4db1c1`. This digest was verified with `docker buildx imagetools inspect golang:1.24.3-alpine`.

The Redis image in `docker-compose.yml` is pinned as `redis:alpine3.16@sha256:2700d5097763fda285c463f4eefc3d0730a2df2a9d48e66707b19d5a5e5f23d4`. This digest was verified with `docker buildx imagetools inspect redis:alpine3.16`.

`FROM scratch` is the special empty Docker base and is not pinned like a registry image. Treat it as acceptable without a digest because there is no upstream image manifest to fetch.

If you update a Docker tag or digest, verify the manifest digest before editing, then run the Docker/compose checks that are available in the current environment.

Production compose raises Redis memory to `2gb`, uses `allkeys-lru`, persists Redis data under `./data/redis:/data`, and attaches the cache service to both `cache` and external `ingress` networks.

Local compose publishes API port `8080` and Redis port `6379`.

## GitHub Actions and CI

All GitHub Actions `uses:` references must be pinned to full-length commit SHAs.

`script/lint` enforces full-SHA action pins and full Docker image digest pins.

Checkout steps should use `persist-credentials: false` unless there is a specific need to keep credentials in the checkout.

The `test` workflow runs `script/bootstrap`, `script/lint`, `script/build`, and `script/test`.

The `acceptance` workflow runs `script/acceptance`.

The `config validation` workflow runs `GrantBirki/json-yaml-validate` against `src/cache`, excluding vendor paths.

The `branch-deploy` workflow is an issue-comment workflow. It responds to `.deploy`, `.noop`, `.lock`, `.unlock`, `.help`, and `.wcid` on pull requests.

Branch deploy uses `github/branch-deploy` with admins `GrantBirki,Razzmatazzz`, environment target `production`, and sticky locks enabled.

Important GitHub Actions behavior: issue-comment workflows run from the default branch workflow file, not from the PR branch's modified workflow file. If a PR changes `.github/workflows/branch-deploy.yml`, those changes will not control that PR's own `.deploy` run until after they land on the default branch.

If changing branch-deploy semantics, consider whether a separate prerequisite PR is needed to land workflow changes before feature changes that depend on them.

## PR and Deployment Flow

For normal code changes, open a PR from a feature branch and wait for CI.

Do not deploy before CI is green unless the repo owner explicitly tells you to bypass that safety gate.

Once CI is green and the owner asks to deploy, comment `.deploy` on the PR.

Watch the `branch-deploy` workflow run after posting `.deploy`.

Do not stop at "workflow success" if the user asked you to watch the deploy. Inspect the job logs and confirm the SSH remote deploy step actually checked out the target SHA, rebuilt/recreated containers, and started the service.

Do not merge after deploy unless the user explicitly asks to merge.

When the user says "wait for my input", stop after deploy verification and do not proceed to merge or cleanup.

If the first `.deploy` fails immediately because branch-deploy sees pending CI or review state, wait for CI to complete and retry only after the gate is actually satisfied.

Branch-deploy admin rights can bypass approval once CI is passing, but approval bypass and code correctness are different things. Still verify checks and deploy logs.

## Deployment Evidence From PR #86

Modernization PR #86 was opened from branch `cache-modernization`.

The first modernization commit was `1289081a5e3415888dfe3b90ff41ec9f5f63633c`.

PR #86 CI passed before deployment: acceptance, test, config validation, and new-PR comment workflows were green.

The `.deploy` trigger comment was posted only after CI passed.

The branch-deploy run for PR #86 was `28351368408`.

Branch-deploy confirmed the PR branch existed, CI was passing, approval was bypassed due to admin rights, branch ruleset checks passed, and the commit signature was valid.

Branch-deploy created or reused the sticky production deployment lock branch and obtained the deployment lock.

The deployment id in the logs was `5234845169`.

The remote deploy script ran as `~/cache/script/deploy -r "1289081a5e3415888dfe3b90ff41ec9f5f63633c" -f "" -d "/home/***/cache" -n ""`.

The remote host's previous checked-out commit was `936f02b8287478c4557af0a6d58ed53800b11dcf`.

The remote deploy checked out `1289081a5e3415888dfe3b90ff41ec9f5f63633c` in detached HEAD state.

The deploy script used the shared host lock at `/tmp/the-hideout-deploy.lock`.

The deploy script ensured the `ingress` Docker network existed before recreating containers.

The production deploy command was effectively `docker compose -f docker-compose.yml -f docker-compose.production.yml up --build -d --remove-orphans` or the `docker-compose` equivalent depending on the host.

The deploy logs showed Docker building `cache-cache`, running `go list -deps ./...`, building the Linux/amd64 static binary, copying the binary and config into the scratch runtime image, recreating the `cache` container, and starting it.

The Redis container was already running during the PR #86 deployment.

The deploy logs ended with `Containers are now running!`, `Successfully executed commands to all hosts.`, deployment completed in 33 seconds, sticky lock detected, and `post deploy completed!`.

Because sticky locks are enabled, branch-deploy did not remove the production lock at the end of the run.

## Security and Safety Notes

Do not print secrets from workflows, deploy logs, local config, `.env` files, SSH material, or GitHub tokens.

The deploy logs redact host, username, key, and port values; keep that boundary in summaries.

Do not put Basic Auth credentials or ingress secrets into this app.

Do not bypass Socket Firewall for package manager operations without explicit user approval.

Do not run destructive git commands such as `git reset --hard` or `git checkout -- <path>` unless the user explicitly asks.

Do not revert unrelated user changes in a dirty worktree.

Before staging, inspect `git status --short --branch --untracked-files=all` and the actual diff. This repo can have very large vendor deletions, so use scoped `git diff --name-status -- . ':!src/cache/vendor'` when you need a readable non-vendor view.

If the full intended change includes vendor pruning, `git add -A` is appropriate only after confirming the whole worktree belongs to the PR.

Keep public PR bodies and comments concise and free of internal/private context.

## Related Repositories

`the-hideout/tarkov-api` is the Cloudflare-side service that uses this cache.

`the-hideout/cloudflare` owns Cloudflare infrastructure and context for the upstream service.

`the-hideout/ingress` owns the public Caddy ingress routing for `cache.tarkov.dev`.

This cache repo should not take over responsibilities that belong to those repos. For example, public TLS, Caddy routing, and Basic Auth belong to ingress; Worker behavior belongs to `tarkov-api` / Cloudflare.

When you need to inspect related repositories, prefer `gh api` / `gh repo view` / `gh search` over cloning. If cloning is necessary, ask first and explain why.

## Coding Style

Keep the code simple. This service is intentionally small.

Prefer straightforward functions over abstractions. The existing store interface is there for testability; do not build a larger framework around it.

Use `net/http`, `encoding/json`, `context`, and other stdlib packages directly where they are sufficient.

Use small helper assertions in tests instead of bringing in a third-party assertion package.

Use `rg` for code searches.

When editing Markdown, do not hard-wrap prose. Preserve meaningful structure such as headings, blank lines, bullets, and code blocks.

When making behavior changes, add tests that pin the wire contract and edge cases.

When changing deployment files, inspect the rendered compose config before deploying.

When changing workflows, remember that action pins and checkout credential behavior are part of the security posture, not formatting details.

## Common Commands

Run `script/bootstrap` from the repository root to verify the vendored graph offline.

Run `script/lint` from the repository root for formatting and action-pin checks.

Run `script/build` from the repository root for a static local binary build.

Run `script/test` from the repository root for the race-enabled Go test suite.

Run `script/acceptance` from the repository root when Docker is available and you need real Redis/container coverage.

Run `docker compose config` to render and sanity-check the compose files without starting containers.

Run `gh pr checks <pr> --watch` to watch PR checks.

Run `gh pr comment <pr> --body ".deploy"` to trigger a production branch deploy after checks are green and the owner has asked for deployment.

Run `gh run watch <run-id> --exit-status` to watch deploy workflow completion.

Run `gh run view <run-id> --job <job-id> --log` to inspect the SSH deploy logs after workflow completion.

## Known Follow-Up Candidate

The planned Redis client migration to `github.com/redis/go-redis/v9@v9.21.0` was intentionally not performed during PR #86 because it required an unprotected Go dependency fetch on this host. The safe future path is: get explicit approval for the dependency refresh, ensure Socket Firewall can protect Go or explicitly approve the bypass, run `script/update-deps github.com/redis/go-redis/v9@v9.21.0`, inspect `go.mod`, `go.sum`, `vendor/modules.txt`, and vendor contents, then run the normal offline gates.
