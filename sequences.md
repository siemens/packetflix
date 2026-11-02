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
> clobber them with the implementation details of the Go routines involved.
> Especially, Packetflix starts `dumpcap` from a separate OS thread that it has
> switched into the target's network namespace, passing on the necessary
> capabilities as ambient capabilities.

```plantuml
hide footbox

boundary Wireshark
participant ClusterShark <<extcap plugin>>
activate ClusterShark
entity Packetflix <<service>>
participant dumpcap <<executable>>

create ClusterShark
Wireshark -> ClusterShark : ""--""capture

ClusterShark -> Packetflix : websocket.connect\n/capture?container=...
activate Packetflix
ClusterShark <-- Packetflix : websocket.connected
  Packetflix -> Packetflix : setns(netns)
  create dumpcap
  Packetflix -> dumpcap : spawn
  activate dumpcap
deactivate Packetflix

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
  Packetflix -> Packetflix : waitpid
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
needs to contact the discovery service (that's Ghostwire) in order to fetch the
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
entity Ghostwire <<service>>
participant dumpcap <<executable>>

create ClusterShark
Wireshark -> ClusterShark : ""--""capture

ClusterShark -> Packetflix : websocket.connect\n/capture?container=...
activate Packetflix
ClusterShark <-- Packetflix : websocket.connected
  Packetflix -> Ghostwire : GET /mobyshark
deactivate Packetflix
  activate Ghostwire
  |||
Packetflix <-- Ghostwire
  deactivate Ghostwire
activate Packetflix
  Packetflix -> Packetflix : setns(netns)
  create dumpcap
  Packetflix -> dumpcap : spawn
  activate dumpcap
deactivate Packetflix

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
  Packetflix -> Packetflix : waitpid
  activate Packetflix
    Packetflix <-- dumpcap
  deactivate Packetflix
  destroy dumpcap
return websocket.closed
destroy ClusterShark
```
