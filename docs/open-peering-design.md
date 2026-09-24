# Open Peering view - design assumptions

Status: implemented

Scope: Kapkan DEV and production. Production uses the S6750 exporter `172.16.7.11` in VLAN
attribution mode and receives the cutover stream on UDP/2055.

## Goal

Add an **Open Peering** navigation item directly below **Hosts**. The view shows only traffic
crossing operator-selected open-peering VLANs or interfaces:

- aggregate ingress traffic;
- aggregate egress traffic;
- ingress TOP 20 peer MAC addresses with traffic bars;
- egress TOP 20 peer MAC addresses with traffic bars.

The first version is a live view over the engine detection window. It does not add historical
ClickHouse storage or affect detection, mitigation, boundary filtering, Hosts, or Overview.

## Verified source-data facts

The Huawei S6750 `KAPKAN-V2` NetStream record is attached inbound and outbound to
`Eth-Trunk300` and declares:

- source and destination addresses and ports;
- input and output interfaces;
- source MAC;
- destination MAC;
- VLAN;
- byte and packet counters;
- sampler information.

A packet capture supplied by the operator confirms that VLAN 992 carries multiple MAC values.
Observed behavior is direction-specific. NetFlow `Direction` describes the
observation point, not Kapkan's direction relative to the protected network:

- ingress `Source Mac Address` was consistently `a0:bc:6f:09:8e:4d` in the sample;
- records with `Direction=1` carry the usable peer in `Post Source Mac Address`; when their IP
  destination is protected, Kapkan classifies that traffic as ingress.

Therefore:

- `Direction=1` records must use the post-output MAC pair;
- `Direction=0` records must use the ordinary input MAC pair;
- after that per-record selection, Kapkan ingress ranks source MAC and Kapkan egress ranks
  destination MAC.

The UI must not present a common router/switch MAC as a peer ranking.

## Implemented data path

NetFlow MAC fields are carried through ingestion into the normalized `flow.Flow`. The engine
selects the peer MAC according to the attack direction and aggregates it after the existing
boundary decision.

The resulting distribution is kept in the live OpenPeering window and in the attack sample.
The sample is immutable once the attack starts and is reused by the API, UI, callbacks and
history; direction-specific MAC selection is already covered by the engine tests.

## Configuration contract

Top-level configuration:

```yaml
open_peering:
  members:
    - {exporter: "172.16.7.11", vlan: 992}
    - {exporter: "172.16.7.11", vlan: 4090}
    - {exporter: "172.16.7.11", vlan: 2812}
    - {exporter: "172.16.7.11", vlan: 2813}
```

For interface attribution mode:

```yaml
open_peering:
  members:
    - {exporter: "192.0.2.10", ifindex: 13}
```

Validation rules:

1. `open_peering` is optional. When absent or empty, collection is disabled and the navigation item
   is hidden.
2. Members are exporter-scoped.
3. In `attribution.mode: vlan`, each member must set `vlan` and must not set `ifindex`.
4. In `attribution.mode: interface`, each member must set `ifindex` and must not set `vlan`.
5. VLAN 0 and ifIndex 0 are invalid.
6. Duplicate exporter/member pairs are rejected.
7. Each member must exist in the corresponding `attribution.vlans` or
   `attribution.interfaces` list, so the UI always has an operator-facing label.
8. Open Peering classification is independent of capacity pools. A member may belong to both.
9. Production can enable the feature in interface attribution mode. The current production
  mapping uses `172.16.7.110/13` and `172.16.7.100/66` for OP EPIX Warszawa; the DEV overlay
  uses VLAN attribution for exporter `172.16.7.11`.

The optional `mac_ip_mapping` collector reads the IPv4 ARP table from configured SNMP v2c
routers. Its community is read from `community_env`, it refreshes every 60 seconds by default,
and the current result is held in memory rather than persisted. The IP is the router's observed
neighbor address; it identifies the OpenPeering node/operator, not necessarily the participant
behind that node.

## Direction and matching semantics

A record is eligible only after it passes the same existing boundary and excluded-VLAN decision
used by host accounting. This prevents Open Peering from showing traffic that detection ignores.

For a protected network endpoint:

- ingress: destination is protected and the ingress attribution member is Open Peering;
- egress: source is protected and the egress attribution member is Open Peering.

Member selection follows the current attribution mode:

- VLAN mode uses the same selected nonzero VLAN as existing attribution;
- interface mode uses `InIf` for ingress and `OutIf` for egress.

A flow that is not associated with a configured Open Peering member contributes nothing to this
view.

## Rate accounting

All totals and MAC counters use sampling-corrected values:

```text
corrected bytes   = exported bytes   * effective sampling rate
corrected packets = exported packets * effective sampling rate
Mbps              = corrected bytes * 8 / window seconds / 1,000,000
PPS               = corrected packets / window seconds
```

The effective rate is the result of the existing boundary decision, including egress-sampling
correction. The live window equals `detection_window_seconds`.

Ingress and egress totals include every eligible record, including records whose MAC cannot be
used for ranking. TOP20 bars must use the full direction total as their denominator, not the sum of
the TOP20 entries.

## MAC handling

MAC values are stored as fixed six-byte values in the normalized flow. They are formatted only at
the API boundary as lower-case colon-separated addresses.

Do not rank these values:

- all-zero MAC;
- broadcast MAC;
- multicast MAC;
- a configured or automatically observed common local/router MAC, once the ingress field behavior
  has been verified.

Traffic without a usable peer MAC remains included in aggregate traffic and is reported as
`unknown_mbps`, `unknown_pps`, and `unknown_share`. This makes missing telemetry visible instead of
silently lowering the total.

The API should also return `distinct_macs` and an explicit data-quality state:

- `ok`: usable peer MACs are present;
- `partial`: both usable and unknown MAC traffic are present;
- `unavailable`: no usable MAC appeared in the current window.

## Engine architecture

Keep the feature isolated in a new engine file, for example:

```text
engine/internal/engine/openpeering.go
```

The file owns:

- resolved member matching calls;
- a bounded, windowed ingress/egress accumulator;
- per-MAC corrected byte and packet counters;
- TOP20 sorting and snapshot types;
- eviction of stale MAC keys.

`Engine.Process` should make one small call after the existing boundary decision succeeds. It must
not duplicate boundary, VLAN-selection, or sampling logic.

Suggested structure:

- one ring bucket per second;
- separate ingress and egress counters;
- maps keyed by `[6]byte` MAC;
- a hard per-bucket cardinality limit with overflow accumulated under unknown;
- snapshot merges only the current detection window and returns TOP20.

The feature must not use the attack-sample ring: that ring is optional, sized for incident samples,
and does not represent a complete live traffic view.

## API contract

Add a viewer-authorized endpoint:

```text
GET /api/v1/open-peering
```

Proposed response:

```json
{
  "enabled": true,
  "window_seconds": 5,
  "ingress": {
    "mbps": 1234.5,
    "pps": 456789,
    "unknown_mbps": 12.3,
    "unknown_pps": 1200,
    "unknown_share": 0.0099,
    "distinct_macs": 37,
    "quality": "partial",
    "top_macs": [
      {"mac": "00:59:dc:16:5c:e9", "mbps": 240.1, "pps": 21000, "share": 0.194}
    ]
  },
  "egress": {
    "mbps": 987.6,
    "pps": 321000,
    "unknown_mbps": 0,
    "unknown_pps": 0,
    "unknown_share": 0,
    "distinct_macs": 42,
    "quality": "ok",
    "top_macs": []
  }
}
```

The endpoint requires:

- route registration in the API role matrix;
- an endpoint behavior test;
- `enabled: false` with empty directions when no members are configured, or alternatively hiding
  the route behind a stable empty response. A stable response is preferred for UI compatibility.

## Console design

Add a navigation item directly below Hosts and show it only when `/api/v1/status` reports
`open_peering_enabled: true`.

Use a dedicated UI file, for example:

```text
console/open-peering.js
```

The view contains:

1. a compact aggregate strip with current Open Peering ingress and egress Mbps/PPS;
2. a two-column responsive layout;
3. an Ingress TOP20 panel;
4. an Egress TOP20 panel;
5. one row per MAC containing MAC, Mbps, PPS, percentage, and a progress bar;
6. a visible telemetry-quality state and unknown share when nonzero;
7. an empty/unavailable state instead of misleading zero-valued peer rows.

The existing three-second console refresh is sufficient. The view needs no independent timer.
Mobile layout stacks the two panels; desktop keeps them side by side. Bars use existing ingress and
egress colors and existing compact upstream-bar visual patterns.

All supported locales need navigation, aggregate, direction, quality, unknown, and empty-state
strings. Locale parity tests must remain green.

## Required tests

### Ingestion

- NetFlow MAC values are copied into normalized flows.
- Zero MAC values remain zero.
- Direction-specific input/post-output MAC semantics are pinned using a real or minimized Huawei
  template-8004 fixture.

### Configuration

- valid VLAN-mode members;
- valid interface-mode members;
- wrong selector for the active mode;
- zero selector;
- duplicate member;
- unknown attribution member;
- invalid exporter;
- generated schema and overlay metadata stay synchronized.

### Engine

- only configured Open Peering members contribute;
- boundary-rejected and excluded-VLAN flows do not contribute;
- ingress and egress are separated correctly;
- sampling correction and detection-window averaging are correct;
- repeated records aggregate by MAC;
- TOP20 ordering and deterministic tie-breaking;
- unknown/multicast/broadcast handling;
- stale buckets expire;
- cardinality overflow goes to unknown rather than growing memory.

### API and UI

- viewer authorization and role matrix coverage;
- disabled and enabled responses;
- response totals, shares, ordering, and quality state;
- navigation visibility;
- responsive rendering and long-value containment;
- locale parity;
- desktop and mobile Playwright screenshots after live data is available.

## Rollout plan

1. Capture enough VLAN 992/4090/2812/2813 traffic to identify the peer-distinguishing ingress MAC
   field and pin it as a fixture.
2. Implement normalized MAC propagation and tests.
3. Implement configuration and validation without enabling it anywhere.
4. Implement the isolated engine accumulator and API.
5. Implement the isolated console view.
6. Enable in DEV with VLAN attribution, validate aggregate rates and inspect unknown share.
7. Enable production with the S6750 exporter `172.16.7.11`, sampling `1:1024`, VLAN attribution,
  and UDP/2055 after the device cutover.
8. Validate TOP20 and attack `sample.open_peering` against the switch export and SNMP MAC-IP data.

## Acceptance criteria

- DEV navigation shows Open Peering only when configured.
- Aggregate rates reconcile with the selected VLAN totals within sampling/window variance.
- Egress TOP20 matches observed `Post Source Mac Address` distribution.
- Ingress TOP20 uses a field proven to distinguish peers and does not collapse to the common
  `a0:bc:6f:09:8e:4d` value.
- Unknown traffic is visible and included in totals.
- Excluded VLANs and boundary-rejected traffic never appear.
- Production and DEV use the same MAC/IP and attack-sample contract; only their exporter, port and
  environment-specific attribution differ.
