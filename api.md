# Paketflix Service API

Packetflix exposes its service API on a single HTTP/Websocket port, defaulting
to TCP port `5001` on single-host container deployments.

> **⚠️** Packetflix does have any integrated support for TLS (HTTPS). Instead,
> deploy a TLS-terminating and authenticating proxy in front of Packetflix with
> single-host container hosts.

The following API paths are currently available:

- `/version`
- `/discover/...`
- `/capture`

## Version API

The version service is exposed via HTTP at the `/version` path.

- parameters: _none_.
- response: JSON document with the folowing atrributes:
  - `name`: name of capture service implementation, such as "packetflix"; this
    name never contains version information.
  - `version`: semantic version, such as `1.0.666`.

## Discover API

Only active if the packetflix service has been started using the
`--proxy-discovery` CLI option. HTTP and Websocket requests to `/discover/...`
are forwarded (reverse proxied) to an associated GhostWire service with
rewritten request paths `/...`.

This request forwarding path should only be enabled when running a Packetflix
service together with a GhostWire service on a single stand-alone container
host. In this mode, capture clients (especially
[csharg](https://github.com/siemens/csharg) and csharg-enabled applications)
only need to be configured with the host IP address as well as a single service
port: the port of the Packetflix service. The discovery part is then hidden
behind the packetflix service facade.

## Capture API

The capture service is exposed via websocket (`ws://`) at the `/capture` path.
If you try to contact it via plain HTTP without the websocket upgrade, then it
will reply very coureously that you would better use the WebSocket protocol.

- `/capture?container=`: this is the preferred way to use the capture service,
  because it safeguards against bad luck with reused network namespace identifiers.
  This comes at the price of slightly longer connection setup delays in case a
  stale (reused) network namespace identifier is detected. The `container=`
  parameter expects a JSON object with at least some attributes; when leaving
  out certain optional attributes then the ability to detect stale network namespaces
  will be cut off, and this call will basically fall back to the non-preferred
  behavior (documented below).

  The JSON value is structured as follows (typically, it will be the same
  value as one of the elements returned from GhostWire's `/mobyshark` API
  endpoint):

  - `netns` (recommended): the network namespace identifier to capture packets
    from. If more attributes are present, it will be crosschecked further. If
    the `netns` attribute is missing, then further information in form of
    `name` (and optionally `prefix`) must be present in order to uniquely
    identify the network namespace by name to capture from.

  - `pid` (recommended): PID of the root process of one of the containers
    attached to the network namespace `netns`. Combined with `starttime`,
    this allows the Packetflix capture service to detect stale `netns`
    identifiers.

  - `starttime` (recommended): the starttime of the process `PID` belonging
    to the network namespace `netns`. It allows for detecting stale `netns`
    identifiers.

  - `name` (recommended): the name of a pod or container that
    together with `prefix` uniquely identifies the network namespace to
    capture from, even if the pod or container has been restarted.

  - `prefix` (recommended/optional): an optional prefix for the name of a pod
    or container. The prefix differentiates between containers from
    multiple container engines within the same container host, especially
    in Docker-in-Docker (DinD) configurations.

  - `network-interfaces` (optional): list (array) of network interface names to capture
    from. If missing, it is assumed to be all network interfaces in the network
    namespace.

  Or in tabular form:

  | `netns` | `pid` | `starttime` | `name` | `prefix` | `network-interfaces` | |
  |:-------:|:-----:|:-----------:|:------:|:--------:|:--------------------:|-|
  |    x    |       |             |        |          |         (x)          | I feel lucky: quick & dirty & potentially very wrong |
  |    x    |   x   |      x      |        |          |         (x)          | fail safe: quick & correct, or fail correctly |
  |    x    |   x   |      x      |   x    |    x     |         (x)          | safe capture; might be slower in startup, when `netns` is stale and a name lookup becomes necessary |
  |    x    |       |             |   x    |    x     |         (x)          | capture by namespace name; always slower startup due to namespace resolution |

- `/capture?netns=`: most simple case where the client feels lucky by just
  specifying the (inode number) identifier of the network namespace it wants
  to capture packets from. If there is no such network namespace, the websocket
  connect will fail with an appropriate error message. If the client is unlucky,
  then the identifier has been reused for a different network namespace, so the
  capture will be done somewhere else.
  
  Traffic will be captured from **all network interfaces** inside the specified
  network namespace.

- `/capture?netns=&nif=`: narrows packet capture from a specific network
  namespace (`netns=`) to only the network interfaces listed in `nif=`. Multiple
  network interfaces must be separated by "`/`" (and not by comma!). For instance: `nif=eth0/eth1`. The rationale here is that Linux network interface names are
  allowed to contain "`,`", but not slashes.

  Please note that the same problem with potential network namespace identifier
  reuse also applies to this API call.
