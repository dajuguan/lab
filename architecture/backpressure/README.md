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