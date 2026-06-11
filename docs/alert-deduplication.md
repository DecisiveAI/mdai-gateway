# Alert Deduplication

How mdai-gateway deduplicates Prometheus alert deliveries from Alertmanager, and the delivery guarantees that result. Code: `internal/adapter/deduper.go`, `internal/adapter/promalert.go`, `internal/nats/service.go`, `internal/server/handlers.go`.

## Why deduplication exists

Alertmanager re-sends notifications for the same alert state: on its `repeat_interval` (periodic re-notification of still-firing alerts), on group changes (a group resend carries all alerts in the group, including unchanged ones), and on retries after non-2xx responses. Without deduplication, every re-delivery would re-trigger hub automations.

## Dedupe key and comparison

Each alert is keyed by `<hub>/<fingerprint>` — the hub name qualifying Alertmanager's hash of the alert's label set — mapped to the latest seen **change time**. The hub qualifier is required because `hub_name` is an annotation, not a fingerprinted label: alerts from different hubs can share a fingerprint and are distinct alerts. The change time is:

- firing alerts use `StartsAt`
- resolved alerts use `EndsAt`

A delivery is accepted only if its change time is **strictly newer** than the stored one. Consequences:

- A `repeat_interval` re-notification of an unchanged firing alert carries the same `StartsAt` and is skipped.
- A firing → resolved transition carries `EndsAt` (later than `StartsAt`) and passes.
- A resolved alert that fires again carries a new `StartsAt` and passes.
- An alert without a fingerprint rejects the whole payload (`ErrMissingFingerprint`) with `400`, as does any other adaptation failure (e.g. a missing `hub_name` annotation): the defect is permanent, so the response must not trigger Alertmanager's 5xx retry. Alertmanager always sets fingerprints in practice.

The skipped count is reported in the webhook response (`"skipped"`), distinct from `"successful"`.

## Two-phase check: peek at adaptation, commit after publish

Dedupe state is read and written at different points in the request, and the separation is load-bearing:

1. **Peek (adaptation).** `ToMdaiEvents` checks each alert against the shared `Deduper` via `PeekLast` without modifying it. Within-payload duplicates are handled by a local per-batch map, since the shared state cannot see them yet.
2. **Commit (publish success).** `nats.PublishEvents` invokes an `onPublished` callback for each event that NATS accepted. The callback is the adapter's `CommitPublished`, which records the dedupe key and change time captured on the event at adaptation (`EventPerSubject.DedupeKey`/`ChangeTime`) — commit stores exactly the values the peek compared. Failed publishes commit nothing.
3. **Failure (response).** If any publish fails, the handler returns `500`. Alertmanager retries the whole notification; alerts that published successfully are now committed and skip as stale, so the retry re-publishes exactly the failed ones.

Committing during adaptation instead would permanently swallow alerts: a retry of a failed publish carries the same fingerprint and change time and would be skipped as stale, while the 5xx is required because Alertmanager treats any 2xx as delivered and never retries.

## Delivery guarantees

The pipeline is **at-least-once**. Duplicates are rare but possible; loss is not:

- Alert event IDs are deterministic — `<hub>/<fingerprint>-<changeTime unix nanos>` — and the publisher sets the `Nats-Msg-Id` header from the event ID. JetStream drops a re-publish of the same alert state within the stream's duplicate window (2 minutes by default). This covers concurrent deliveries racing past the peek (Alertmanager retries against a slow gateway, HA Alertmanager double sends) and retries after a lost publish acknowledgment.
- Duplicates separated by more than the duplicate window still pass: a pod restart clears the in-memory state, and the next re-notification of a still-firing alert (typically a `repeat_interval` resend, hours later) carries the same ID but falls outside the window and is re-published.
- Because the ID is deterministic, a duplicate that escapes the window reaches consumers with the same event ID as the original. The correlation ID remains unique per delivery.

Downstream consumers of alert events are expected to tolerate duplicate triggers.

## Constraints

- **State is in-memory and per-replica.** Multiple gateway replicas each keep their own dedupe map; running more than one replica multiplies the duplicate window. Durable shared state (e.g. Valkey-backed) is a known option, deliberately not taken to keep a synchronous dependency out of the alert hot path.
- **The map has no TTL.** Entries accumulate one per unique fingerprint for the lifetime of the pod (`deduper.go` carries a TODO to add a TTL matching Alertmanager's 12h default). Fingerprint cardinality equals distinct alert label sets, so growth is slow in practice but unbounded.
- **Equal timestamps are skipped.** "Strictly newer" means a legitimate re-fire with an identical `StartsAt` (sub-second flap collapsed by Prometheus) is indistinguishable from a repeat and is dropped.

## Scope

Only the Alertmanager webhook path (`POST /alerts/alertmanager`) is deduplicated. The variables API and OpAMP publish paths call `PublishEvents` with a nil `onPublished` and perform no deduplication — their events are user- or agent-initiated and carry no retry semantics that would duplicate them.
