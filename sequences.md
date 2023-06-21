# Packetflix Service Sequences

This is a high-level view on the overall sequence of things when a client (such
as the ClusterShark plugin) uses the Packetflix service API. It is not our goal
to document detailed flow of control inside the implementation of Packetflix.
Please refer to the source code for such details, as they are the only
authoritative source in that case.

## Connecting with Up-to-Date Container Reference

Let's start with the most straightforward situation, where a client of the
packet capture service (such as the ClusterShark extcap plugin) connects and
supplies up-to-date reference information as to from which container to capture
packets from. That is, the container reference's `netns`, `pid` and `starttime`
are valid, so the capture service can simply kick off capturing without further
hesitation.

> Please note that the sequence diagrams are slightly simplified as to not
> clobber them with the ugly implementation details of using a reactor for
> asynchronuous I/O handling.

```plantuml
hide footbox

boundary Wireshark
participant ClusterShark <<extcap plugin>>
activate ClusterShark
entity Packetflix <<service>>
participant nsenter <<executable>>
participant dumpcap <<executable>>

create ClusterShark
Wireshark -> ClusterShark : ""--""capture

ClusterShark -> Packetflix : websocket.connect\n/capture?container=...
activate Packetflix
  create nsenter
  Packetflix -> nsenter : spawn\nnsenter ""--""no-fork
  activate nsenter

ClusterShark <-- Packetflix : websocket.connected
deactivate Packetflix

    create dumpcap
    nsenter -> dumpcap : exec
    activate dumpcap
  deactivate nsenter

|||

loop
  dumpcap -> Packetflix : stdout data
  activate Packetflix
  Packetflix -> ClusterShark : frame
  deactivate Packetflix
  ClusterShark -> Wireshark : fifo.write
end

|||

Wireshark -> ClusterShark : fifo.close
ClusterShark -> Packetflix : websocket.close
activate Packetflix
  Packetflix -> dumpcap : kill SIGTERM
  Packetflix -> Packetflix : waitpit
  activate Packetflix
    Packetflix <-- dumpcap
  deactivate Packetflix
  destroy dumpcap
return websocket.closed
destroy ClusterShark
```

## Connecting with Stale Container Reference

The container reference might be stale in that it still references a valid
container by `name` and `prefix`, but the `netns` might be invalid. This is
crosschecked and detected on the basis of `pid` and `starttime`. If the
`starttime`doesn't match the one of the process `pid`, then the capture service
needs to contact the discovery service (that's GhostWire) in order to fetch the
most up-to-date network namespace ID.

If the data returned by the discovery service still lists the container by
`name` and `prefix`, then starting the capture and streaming packet capture
data proceeds as usual, as seen in the previous sequence diagram.

```plantuml
hide footbox

boundary Wireshark
participant ClusterShark <<extcap plugin>>
activate ClusterShark
entity Packetflix <<service>>
entity GhostWire <<service>>
participant nsenter <<executable>>
participant dumpcap <<executable>>

create ClusterShark
Wireshark -> ClusterShark : ""--""capture

ClusterShark -> Packetflix : websocket.connect\n/capture?container=...
activate Packetflix
  Packetflix -> GhostWire : GET /mobyshark
deactivate Packetflix
  activate GhostWire
  |||
Packetflix <-- GhostWire
  deactivate GhostWire
activate Packetflix
  create nsenter
  Packetflix -> nsenter : spawn\nnsenter ""--""no-fork
  activate nsenter

ClusterShark <-- Packetflix : websocket.connected
deactivate Packetflix

    create dumpcap
    nsenter -> dumpcap : exec
    activate dumpcap
  deactivate nsenter

|||

loop
  dumpcap -> Packetflix : stdout data
  activate Packetflix
  Packetflix -> ClusterShark : frame
  deactivate Packetflix
  ClusterShark -> Wireshark : fifo.write
end

|||

Wireshark -> ClusterShark : fifo.close
ClusterShark -> Packetflix : websocket.close
activate Packetflix
  Packetflix -> dumpcap : kill SIGTERM
  Packetflix -> Packetflix : waitpit
  activate Packetflix
    Packetflix <-- dumpcap
  deactivate Packetflix
  destroy dumpcap
return websocket.closed
destroy ClusterShark
```
