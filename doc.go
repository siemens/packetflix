// (c) Siemens AG 2023
//
// SPDX-License-Identifier: MIT

/*
Package packetflix implements the Packetflix capture-as-a-service: it captures
network packets from the network interfaces of a container, pod, process-less
virtual IP stack, et cetera. The captured network packets are then streamed live
via a websocket connection to clients connecting to our service.

If available, then Packetflix will use Ghostwire to update stale container
references with the most up-to-date information as to capture correctly from
pods and containers which were reloaded, et cetera. For this, run Ghostwire
side-by-side with Packetflix.

# Capture Handler Options

[NewCaptureHandler] returns the stock capture handler. Downstream variants of
the packetflix service use [NewCaptureHandlerWith] and [CaptureOption]s
instead:

  - [WithAdmission] decides about a capture request before the websocket
    upgrade, so that a refusal is an ordinary HTTP response. An [Admission] can
    also supply the capture arguments, so that nothing from the request's query
    parameters or “Clustershark-*” headers influences the capture.
  - [WithObserver] reports when a capture started, failed to start, and ended,
    together with the original request, the websocket connection ID, the
    effective capture arguments, and the close code and reason. A
    [CaptureStarted] event allows stopping the capture from the server side
    using [CaptureEvent.Stop], for instance after a maximum duration.

Without any options, [NewCaptureHandlerWith] behaves exactly as
[NewCaptureHandler].

See package [github.com/siemens/packetflix/cmd/packetflix/rootcmd] for how to
assemble a packetflix service variant.
*/
package packetflix
