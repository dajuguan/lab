# Backpressure Mechanisms

This module compares three common backpressure mechanisms and analyzes their trade-offs in terms of message loss, blocking behavior, cascading risk, and typical use cases.

| Model                                             | Message Loss | Blocking | Cascading Risk | Typical Use Case                           |
| ------------------------------------------------- | ------------ | -------- | -------------- | ------------------------------------------ |
| **Blocking Backpressure**                         | ❌ No         | ✅ Yes    | ✅ Possible     | Reliability-critical systems               |
| **Drop-based Backpressure (Incoming / Outgoing)** | ✅ Yes        | ❌ No     | ❌ No           | P2P networks, real-time systems            |
| **Signal-based Backpressure**                     | ❌ No         | ❌ No     | ❌ No           | Protocol-level flow control (e.g., HTTP/2) |

> Here, the first two mechanisms are implemented in go.
- **In Go**, message dropping can be implemented using a `select` statement with a `default` case. When the channel buffer is full, the `default` branch is triggered, allowing the sender to skip the send operation instead of blocking.

- **In Rust**, `tokio::sync::mpsc::Sender::try_send` provides a non-blocking send operation. If the channel is full, it returns an error. By intentionally ignoring this error, we can implement a drop-on-full strategy.


```rust
match msg {
    types::Payload::Data(data) => {
        // Send message to application using non-blocking try_send.
        //
        // We intentionally drop messages when the application buffer is
        // full rather than blocking. Blocking here would also block
        // processing of gossip messages (BitVec, Peers), causing the
        // peer connection to stall and potentially disconnect.
        let sender = senders.get_mut(&data.channel).unwrap();
        if let Err(e) = sender.try_send((peer.clone(), data.message)) {
            if matches!(e, TrySendError::Full(_)) {
                self.dropped_messages
                    .get_or_create(&metrics::Message::new_data(&peer, data.channel))
                    .inc();
            }
            debug!(err=?e, channel=data.channel, "failed to send message to client");
        }
    }
    types::Payload::Greeting(_) => unreachable!(),
    types::Payload::BitVec(bit_vec) => {
        // Gather useful peers
        tracker.bit_vec(bit_vec, self.mailbox.clone());
    }
    ...
}
```
---

## Notes

* **Blocking backpressure** propagates pressure upstream by blocking producers when buffers are full. It preserves message integrity but may introduce cascading stalls.
* **Drop-based backpressure** avoids blocking by discarding messages when buffers are full. It improves system liveness and isolation, especially in distributed or peer-to-peer environments.
* **Signal-based backpressure** relies on explicit flow-control signals (e.g., window updates) to regulate data transmission without blocking or dropping messages at the application layer.

---

## Reference

* [*Message Delivery in commonware (Incoming and Outgoing Drop)*](https://docs.rs/commonware-p2p/latest/commonware_p2p/authenticated/discovery/index.html#message-delivery)

---

## Tokio Broadcast In Builder WebSocket Path

In the flashblocks builder's WebSocket publishing path, the core mechanism is not "block the producer until slow clients catch up". It is a bounded fan-out buffer with lag detection:

- The producer continues publishing into a bounded `tokio::sync::broadcast::channel`.
- **All subscribers share one bounded ring buffer**.
- Each WebSocket subscriber owns only its own receiver cursor / read position.
- `recv()` advances only that subscriber's cursor; it does not consume the message globally.
- If one subscriber falls behind the channel's retained window, old messages are overwritten.
- The slow subscriber observes `RecvError::Lagged(skipped)` and skips directly to newer messages.

This is a drop-on-lag model, not a blocking backpressure model.

### Shared Buffer Model

`tokio::sync::broadcast` is not "one queue per consumer". Its structure is closer to:

- one shared bounded history window
- multiple readers
- one cursor per reader

So the important semantic point is:

- consumers share the same retained message buffer
- consumers do not own independent send queues
- producer progress is decoupled from slow readers

This is why one slow consumer does not fill up "its own queue" and block the publisher. The publisher keeps writing into the shared ring buffer, and only the lagging reader pays the price by losing overwritten history.

### Core Rust Code

Publisher side:

```rust
pub fn with_capacity(addr: SocketAddr, capacity: usize) -> io::Result<Self> {
    let (pipe, _) = broadcast::channel(capacity);
    // ...
}

pub fn publish(&self, payload: &impl Serialize) -> io::Result<usize> {
    let serialized = serde_json::to_string(payload)?;
    let utf8_bytes = Utf8Bytes::from(serialized);
    self.pipe.send(utf8_bytes)?;
    Ok(size)
}
```

Receiver side for each WebSocket connection:

```rust
payload = self.blocks.recv() => match payload {
    Ok(payload) => {
        if let Err(e) = self.stream.send(Message::Text(payload)).await {
            break;
        }
    }
    Err(RecvError::Closed) => {
        return;
    }
    Err(RecvError::Lagged(skipped)) => {
        warn!(skipped = skipped, "Broadcast channel lagged, some messages were dropped");
    }
},
```

### Why This Is Not Blocking Backpressure

If this were blocking backpressure, a slow WebSocket client would eventually block the builder's send path and could delay flashblock production. That is not what happens here.

- `publish()` writes into the shared broadcast channel and returns immediately once the message is accepted into the bounded buffer.
- Slow consumers do not force the producer to wait.
- Once channel history exceeds `capacity`, lagging consumers lose older messages instead of pushing pressure back to the producer.

So the system chooses liveness and isolation over per-subscriber reliability.

### Where This Appears In Current Code

- Channel creation: [publisher.rs](/base/crates/builder/publish/src/publisher.rs#L45)
- Producer send: [publisher.rs](/base/crates/builder/publish/src/publisher.rs#L65)
- Lag handling: [broadcast.rs](/base/crates/builder/publish/src/broadcast.rs#L60)

### Architectural Meaning

For flashblocks, this design makes sense for the builder-side publish path:

- A slow RPC subscriber should not stall block building.
- Fan-out cost is isolated behind the broadcast channel and per-connection loops.
- The cost of isolation is that some subscribers may miss intermediate flashblocks under load.

This is therefore better described as:

- bounded buffer
- slow-consumer drop
- lag detection via `RecvError::Lagged`

It is not:

- end-to-end reliable delivery
- producer-blocking backpressure
- replay or retransmission

---

## Application-Level Backpressure Framework

At application level, backpressure design is often not complicated in implementation, but it must be explicit in trade-offs. The useful abstraction is not only "blocking vs drop", but three deeper questions:

- When downstream cannot keep up, what property is the system trying to preserve?
- Who pays the cost: producer, slow consumer, or the whole pipeline?
- Does pressure propagate upstream, or is it absorbed locally?

### Common Design Families

#### 1. Blocking

- Producer waits when buffer is full.
- Preserves message integrity.
- Pressure propagates upstream.
- Risk: cascading stalls and latency amplification.

Typical fit:

- reliability-critical pipelines
- ordered processing where loss is unacceptable

#### 2. Drop Newest / Reject Send

- New send is rejected when buffer is full.
- Existing queued data is preserved.
- Producer is not blocked, but some new messages are discarded.

Typical fit:

- non-blocking ingress
- systems where temporary overload should be shed immediately

#### 3. Drop Oldest / Overwrite History

- New data is accepted, oldest retained data is overwritten.
- Slow consumers lose historical completeness.
- Producer stays live.

Typical fit:

- latest-state dissemination
- broadcast, telemetry, real-time views

`tokio::sync::broadcast` is closest to this model.

#### 4. Explicit Flow Control

- Sender and receiver coordinate via credits, windows, or acknowledgements.
- The application does not rely only on queue semantics.
- Useful when both throughput and bounded loss matter.

Typical fit:

- transport or protocol layers
- multiplexed streams such as HTTP/2 style flow control

### A Practical Decision Rule

When analyzing an application-level backpressure design, ask:

1. Is the system preserving completeness, freshness, or liveness?
2. Should slow consumers be allowed to stall fast producers?
3. Is the unit of correctness "every message", or "the latest visible state"?

For flashblocks, the answer is usually:

- preserve builder liveness
- preserve recent state visibility
- do not let slow subscribers stall the build path

That is why the builder-side WebSocket path is naturally a better fit for:

- bounded buffers
- non-blocking producer behavior
- lagging consumers dropping history
