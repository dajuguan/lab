# Backpressure Mechanisms

This module compares three common backpressure mechanisms and analyzes their trade-offs in terms of message loss, blocking behavior, cascading risk, and typical use cases.

| Model                                             | Message Loss | Blocking | Cascading Risk | Typical Use Case                           |
| ------------------------------------------------- | ------------ | -------- | -------------- | ------------------------------------------ |
| **Blocking Backpressure**                         | ❌ No         | ✅ Yes    | ✅ Possible     | Reliability-critical systems               |
| **Drop-based Backpressure (Incoming / Outgoing)** | ✅ Yes        | ❌ No     | ❌ No           | P2P networks, real-time systems            |
| **Signal-based Backpressure**                     | ❌ No         | ❌ No     | ❌ No           | Protocol-level flow control (e.g., HTTP/2) |

> Here, the first two mechanisms are implemented in go.
---

## Notes

* **Blocking backpressure** propagates pressure upstream by blocking producers when buffers are full. It preserves message integrity but may introduce cascading stalls.
* **Drop-based backpressure** avoids blocking by discarding messages when buffers are full. It improves system liveness and isolation, especially in distributed or peer-to-peer environments.
* **Signal-based backpressure** relies on explicit flow-control signals (e.g., window updates) to regulate data transmission without blocking or dropping messages at the application layer.

---

## Reference

* [*Message Delivery in commonware (Incoming and Outgoing Drop)*](https://docs.rs/commonware-p2p/latest/commonware_p2p/authenticated/discovery/index.html#message-delivery)