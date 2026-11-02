# Packetflix Service API

Packetflix exposes its service API on a single HTTP/Websocket port, defaulting
to TCP port `5001` on single-host container deployments.

> **⚠️** Packetflix does not have any integrated support for TLS (HTTPS).
> Instead, deploy a TLS-terminating and authenticating proxy in front of
> Packetflix with single-host container hosts.

The following API paths are currently available:

- `/version`
- `/capture`
- `/...` (everything else, when proxying the discovery service)

## Version API

The version service is exposed via HTTP at the `/version` path.

- parameters: _none_.
- response: JSON document with the folowing atrributes:
  - `name`: name of capture service implementation, such as "packetflix"; this
    name never contains version information.
  - `version`: semantic version, such as `1.0.666`.

## Discovery Service Proxy

Only active if the packetflix service has been started using the
`--proxy-discovery` CLI option. All HTTP and Websocket requests not handled by
Packetflix itself are then forwarded (reverse proxied) to an associated
Ghostwire service, with their request paths left unchanged except for
normalization. For backwards compatibility, `/discover/mobyshark` gets forwarded
as `/mobyshark`. Additionally, Packetflix tells the Ghostwire UI to enable its
capture buttons.

This request forwarding path should only be enabled when running a Packetflix
service together with a Ghostwire service on a single stand-alone container
host. In this mode, capture clients (especially
[csharg](https://github.com/siemens/csharg) and csharg-enabled applications)
only need to be configured with the host IP address as well as a single service
port: the port of the Packetflix service. The discovery part is then hidden
behind the packetflix service facade.

## Capture API

The capture service is exposed via websocket (`ws://`) at the `/capture` path.
If you try to contact it via plain HTTP without the websocket upgrade, then it
will reply very courteously that you would better use the WebSocket protocol.

Packetflix always completes the websocket upgrade first and only then checks the
capture parameters and starts capturing. Errors are thus reported by closing the
websocket with a close code and a reason text:

- `1008` (policy violation): invalid capture parameters, or unknown network
  namespace.
- `1011` (internal server error): capture couldn't be started or failed; the
  reason then is the error message from `dumpcap`, if available.
- `1000` (normal closure): the capture process terminated.

- `/capture?container=`: this is the preferred way to use the capture service,
  because it safeguards against bad luck with reused network namespace identifiers.
  This comes at the price of slightly longer connection setup delays in case a
  stale (reused) network namespace identifier is detected. The `container=`
  parameter expects a JSON object with at least some attributes; when leaving
  out certain optional attributes then the ability to detect stale network namespaces
  will be cut off, and this call will basically fall back to the non-preferred
  behavior (documented below).

  The JSON value is structured as follows (typically, it will be the same
  value as one of the elements returned from Ghostwire's `/mobyshark` API
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

  - `network-interfaces` (optional): list (array) of network interface names to
    capture from. If missing, Packetflix uses the list from the discovery
    service when it needed to refresh a stale `netns`, and otherwise falls back
    to the `any` pseudo network interface (see below).

  Or in tabular form:

  | `netns` | `pid` | `starttime` | `name` | `prefix` | `network-interfaces` | |
  |:-------:|:-----:|:-----------:|:------:|:--------:|:--------------------:|-|
  |    x    |       |             |        |          |         (x)          | I feel lucky: quick & dirty & potentially very wrong |
  |    x    |   x   |      x      |        |          |         (x)          | fail safe: quick & correct, or fail correctly |
  |    x    |   x   |      x      |   x    |    x     |         (x)          | safe capture; might be slower in startup, when `netns` is stale and a name lookup becomes necessary |
  |    x    |       |             |   x    |    x     |         (x)          | capture by namespace name; always slower startup due to namespace resolution |

- `/capture?netns=`: most simple case where the client feels lucky by just
  specifying the (inode number) identifier of the network namespace it wants
  to capture packets from. If there is no such network namespace, Packetflix
  closes the websocket with an appropriate reason. If the client is unlucky,
  then the identifier has been reused for a different network namespace, so the
  capture will be done somewhere else.
  
  Traffic will be captured using the `any` pseudo network interface, that is,
  from all network interfaces inside the specified network namespace, but as a
  single "cooked" packet stream without telling the individual network
  interfaces apart.

- `/capture?netns=&nif=`: narrows packet capture from a specific network
  namespace (`netns=`) to only the network interfaces listed in `nif=`. Multiple
  network interfaces must be separated by "`/`" (and not by comma!). For
  instance: `nif=eth0/eth1`. The rationale here is that Linux network interface
  names are allowed to contain "`,`", but not slashes. `nif=` can also be used
  together with `container=`, where `nif=any` keeps the container's network
  interfaces.

  Please note that the same problem with potential network namespace identifier
  reuse also applies to this API call.

Further optional query parameters:

- `filter=`: capture filter expression in [pcap-filter
  syntax](https://www.tcpdump.org/manpages/pcap-filter.7.html), applied to all
  network interfaces captured from.
- `chaste`: avoid promiscuous mode on all network interfaces captured from, if
  possible. Only its presence matters.

The pcapng stream's section header block gets a comment with YAML-formatted
capture target information, including the container name and type, capture
filter, and promiscuous mode setting.

As some proxies (such as the Kubernetes remote API pod proxy verb) drop the URL
query parameters of websocket connects, the parameters can alternatively be
passed in the following HTTP headers, which take precedence:

| Header | Query Parameter |
|---|---|
| `Clustershark-Container` | `container` |
| `Clustershark-Nif` | `nif` |
| `Clustershark-Filter` | `filter` |
| `Clustershark-Chaste` | `chaste` |
