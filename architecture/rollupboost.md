# rollup-boost WebSocket 数据面分析

## 0. 范围与拓扑
本文基于 `rollup-boost` 仓库当前实现，聚焦三段 WebSocket 数据面：

1. Builder ↔ RB（rollup-boost flashblocks inbound）
- 入口：`crates/rollup-boost/src/flashblocks/inbound.rs`
- 语义：RB 作为 WebSocket 客户端连接 builder（或 sequencer 侧 flashblocks 源），接收 `FlashblocksPayloadV1`。

2. RB ↔ Proxy（rollup-boost flashblocks outbound）
- 入口：`crates/rollup-boost/src/flashblocks/outbound.rs`
- 语义：RB 在本地监听 WebSocket，向下游订阅者广播已通过 RB 内部校验/组装流程的 flashblocks。

3. RPC ↔ Proxy（flashblocks-websocket-proxy 的下游面）
- 入口：`crates/websocket-proxy/src/{subscriber.rs,registry.rs,server.rs,main.rs}`
- 语义：proxy 从上游（RB）订阅，再向 RPC 节点/客户端扇出；proxy 负责接入控制与连接治理，不做业务 payload 校验。

补充：`crates/flashblocks-rpc/src/flashblocks.rs` 是典型 RPC 消费者实现，体现了“RPC 侧如何消费 proxy 推流并更新本地 cache”。

## 0.1 间接需求分析（非功能但刚性）
- 直接目标：把 flashblocks 从 builder 推到下游。
- 间接刚性需求：连接不能卡死、状态不能跨块污染、异常不能扩散到全链路。
- 这些需求虽非产品显式功能，但决定系统是否可运营。
- 边界收敛方式：
  - 连接活性收敛在 inbound：`FlashblocksReceiverService::run(self)` + `connect_and_handle(...)`。
  - 序列正确性收敛在核心状态机：
    - `FlashblocksService::set_current_payload_id(&self, payload_id: PayloadId)`
    - `FlashblockBuilder::extend(&mut self, payload: FlashblocksPayloadV1) -> Result<(), FlashblocksError>`
  - 对外扇出收敛在广播层：`WebSocketPublisher::publish(&self, payload: &FlashblocksPayloadV1) -> io::Result<()>`。
- 结论：真实需求不是“能转发消息”，而是“网络抖动、乱序输入、慢消费者存在时仍维持局部正确并快速恢复”。

## 0.2 子系统如何正交分解（核心纵向 + 外围横向）
- 纵向主链（核心闭环）：Builder -> rollup-boost -> (可选 websocket-proxy) -> flashblocks-rpc
  1. `Flashblocks::run(builder_url: RpcClient, flashblocks_url: Url, outbound_addr: SocketAddr, websocket_config: FlashblocksWebsocketConfig) -> eyre::Result<FlashblocksService>`
  2. 建立 `mpsc::channel(100)` 作为主链消息通道。
  3. 输入接入：`FlashblocksReceiverService::new(url, sender, websocket_config)`。
  4. 核心处理：`FlashblocksService::run(&mut self, stream: mpsc::Receiver<FlashblocksPayloadV1>)`（payload_id/index 约束 + 状态演进）。
  5. 对外发布：`WebSocketPublisher::new(addr: SocketAddr) -> io::Result<Self>` / `publish(...)`。
- 横向旁路（不进入核心状态机闭环）：
  1. 鉴权与接入治理：`websocket-proxy` 的 `/ws` vs `/ws/{api_key}`。
  2. 背压与配额：broadcast lag、per-IP/per-app rate limit。
  3. 观测与恢复：metrics、reconnect backoff。
- 判定与收益：
  - 核心系统只承担协议正确性不变量。
  - 外围系统承载可变运维策略并可独立替换。
  - 变化被限制在边界内，降低扩散风险。

## 1. WebSocket 基础（握手、帧、ping/pong、关闭语义）

### 1.1 Builder ↔ RB
- 握手：`connect_async(url)` 发起连接，并用 `tokio::time::timeout(connect_timeout)` 保护握手阶段，防止半开卡死。
- 帧处理：
  - `Text`：反序列化为 `FlashblocksPayloadV1` 后转发到内部 `mpsc`。
  - `Pong`：读取 pong payload（uuid）并用于心跳确认。
  - `Ping`：忽略（由底层协议栈处理回应）。
  - `Close`/读错误/`None`：判定连接失效，触发重连分支。
- 关闭语义：通过 `CancellationToken` 驱动 ping 任务结束，并尝试 `write.close()` 优雅关闭。

### 1.2 RB ↔ Proxy
- RB 侧是 WebSocket server：`accept_async` 接受订阅连接。
- 读路径：订阅连接的入站帧只用于维持协议活性（注释说明依赖 tungstenite 自动处理 ping/close），不消费业务。
- 写路径：从 `broadcast::Receiver<Utf8Bytes>` 取消息，写 `Message::Text` 给每个订阅者。
- 关闭语义：发送失败、读错误、通道关闭、term 信号都会结束该订阅者循环。

### 1.3 RPC ↔ Proxy
- Proxy 作为 server，按配置启用 `/ws` 或 `/ws/{api_key}`。
- 每客户端两条并发路径：
  - 写路径：接收 `broadcast` 消息并 `ws_sender.send().await`。
  - 读路径：`start_reader` 监听 `Pong/Close/Error/None`，用 oneshot 通知断开。
- 客户端心跳：可选全局 ping sender（周期向所有连接广播 ping）；读路径按 `pong_timeout_ms` 断开超时客户端。

### 1.4 连接认证（Authentication）现状
- Builder ↔ RB：当前实现侧重连接可用性与顺序校验，未在该 WebSocket 通道内实现独立 API key 鉴权逻辑（通常依赖网络边界、私网、TLS/反向代理策略）。
- RB ↔ Proxy（rollup-boost outbound）：当前 outbound 发布器本身未内建订阅鉴权，默认是谁能连到监听地址谁就能订阅。
- RPC ↔ Proxy（flashblocks-websocket-proxy）：支持可选 API key 鉴权；配置了 `api_keys` 后走 `/ws/{api_key}`，未配置则是公开 `/ws`。
- 运维建议：生产环境应把“是否允许连接”放在 server 侧强制执行（API key、IP 白名单、网关鉴权、mTLS），不要只依赖客户端自律配置。
### 1.5 核心代码（Rust 节选）
```rust
// Builder ↔ RB: 握手 + 超时保护
let (ws_stream, _) = tokio::time::timeout(timeout, connect_async(self.url.as_str()))
    .await
    .map_err(|_| FlashblocksReceiverError::Connection(Io(TimedOut.into())))??;

// RPC ↔ Proxy: 根据是否配置鉴权切换路由
if self.authentication.is_some() {
    router = router.route("/ws/{api_key}", any(authenticated_websocket_handler));
} else {
    router = router.route("/ws", any(unauthenticated_websocket_handler));
}
```

## 2. 长连接状态机（重连、心跳、读写超时、半连接）

### 2.1 Builder ↔ RB：状态机最完整
- 重连：`ExponentialBackoff`，初始/最大间隔由 `FlashblocksWebsocketConfig` 控制。
- 心跳：主动 ping（携带 uuid），被动校验 pong（从 LRU ping 缓存 pop）。
- 超时：
  - 握手超时：`connect_timeout_ms`。用于限制 `connect_async(...)`（TCP连接+WebSocket升级）的最长等待时间，避免在“既不成功也不失败”的状态长期 pending。
  - 典型防护场景：TCP已建立但Upgrade响应丢失/悬挂、代理/中间网络黑洞、远端卡住不回`101`。
  - 超时后会立即走重连 backoff，而不是卡死在单次连接尝试。
  - pong 超时：定时器到期即判定失活。
- 半连接防护：
  - 握手 timeout 防“TCP 建立但 WebSocket 升级悬挂”。
  - pong deadline 防“链路静默但 socket 未立即报错”。
- 特殊设计：仅当连接存活时间超过 backoff 的 `max_interval` 才 `backoff.reset()`，避免“刚连上就断”的抖动造成重连风暴。

### 2.2 RB ↔ Proxy
- 没有独立主动心跳任务；依赖协议栈和读写错误检测连接生命周期。
- 每个订阅者一个 task，天然隔离慢/坏连接，不会全局阻塞。

### 2.3 RPC ↔ Proxy
- 上游订阅（proxy→RB）有完整心跳+重连（`subscriber.rs`）。
- 下游客户端（client→proxy）有可选心跳检查与 pong 超时断开（`registry.rs` + `main.rs` ping task）。
- 读写超时不是“每次写超时”，而是“心跳式存活判断 + I/O 错误驱动断链”。
### 2.4 核心代码（Rust 节选）
```rust
// 连接失败 -> backoff 重连
if let Err(e) = self.connect_and_handle(&mut backoff, timeout).await {
    let interval = backoff
        .next_backoff()
        .expect("max_elapsed_time not set, never None");
    tokio::time::sleep(interval).await;
}

// ping/pong 超时驱动失活判定
tokio::select! {
    result = read.next() => { /* handle Text/Pong/Close */ }
    _ = pong_interval.tick() => {
        return Err(FlashblocksReceiverError::PongTimeout);
    }
}
```

## 3. 消息顺序与去重（单调序号、乱序、丢包、重复包）

### 3.1 TCP 层负责的部分（传输语义）
- 在单个 TCP 连接内，字节流按序到达（不会把后发字节交付到前发字节之前）。
- TCP 负责丢包重传与校验，尽量保证“连接内可靠传输”。
- 这些能力只覆盖“当前连接”的传输，不等于业务消息可恢复。

### 3.2 业务层负责的部分（应用语义）
- Builder ↔ RB：`FlashblockBuilder::extend` 要求 `payload.index` 严格连续，否则报 `InvalidIndex`。
- Builder ↔ RB：`payload_id` 必须匹配当前 FCU 对应 payload，不匹配直接丢弃。
- RB ↔ Proxy：广播层不做重排/去重，只转发 RB 判定为有效的 payload。
- RPC 消费侧：`index==0` 时 reset，再按连续 index 组装；用于阻断跨块混入和乱序污染。

### 3.3 当前没有 backfill（缺口补齐）机制
- 现状没有“按 `(payload_id,index)` 回补缺失消息”的协议。
- 慢消费者 `Lagged`、连接抖动、重连窗口都可能造成中间 index 缺失。
- 缺失后系统不会自动补洞；通常是继续等待后续消息，或在新一轮 `index==0`/新 `payload_id` 时重建状态。
- 因此当前语义偏低延迟广播（at-most-once 倾向），不是可回放的强可靠流。
### 3.4 核心代码（Rust 节选）
```rust
// 应用层顺序约束：index 必须严格连续
if payload.index != self.flashblocks.len() as u64 {
    return Err(FlashblocksError::InvalidIndex);
}

// 应用层一致性约束：payload_id 必须匹配当前 FCU
match *self.current_payload_id.read().await {
    Some(payload_id) if payload_id != payload.payload_id => {
        self.metrics.current_payload_id_mismatch.increment(1);
        return;
    }
    None => return,
    _ => {}
}
```

## 4. 背压控制（发送限速、接收队列、丢弃策略）

### 4.1 队列与容量
- RB inbound: `mpsc::channel(100)`（builder 消息进 RB service）。
- RB outbound: `broadcast::channel(100)`（RB 对订阅者广播）。
- Proxy ingress/egress:
  - 上游 subscriber handler -> `broadcast::channel(message_buffer_size)`。
  - 默认 `message_buffer_size=20`，慢客户端达到 lag 会被断开。

### 4.2 丢弃策略
- RB outbound：`RecvError::Lagged(_)` 仅告警，消费者继续；意味着该连接丢部分消息。
- Proxy downlink：`Lagged` 分支会断开对应客户端（更激进，保护整体）。

### 4.3 限速/配额
- Proxy server 侧有连接配额（instance/IP/app），支持 in-memory 与 Redis 分布式计数。
- 这是接入层限速，不是消息级 token-bucket。

### 4.4 现状判断
- 当前实现偏“有界队列 + 慢消费者淘汰”，而非“强可靠排队传输”。
- 目标是低延迟广播优先，不是完整历史回放。

### 4.5 核心代码（Rust 节选）
```rust
// 有界队列：RB outbound 广播通道容量固定
let (pipe, _) = broadcast::channel(100);

// 慢消费者策略：lagged 时记录并丢失部分历史消息
payload = blocks.recv() => match payload {
    Ok(payload) => {
        if let Err(e) = sink.send(Message::Text(payload)).await {
            break;
        }
    }
    Err(RecvError::Lagged(_)) => {
        tracing::warn!("Broadcast channel lagged, some messages were dropped");
    }
    Err(RecvError::Closed) => return,
}
```

## 5. 可靠性语义（最多一次/至少一次/恰好一次）

当前三段总体语义更接近：**最多一次（at-most-once）偏向**。

1. Builder ↔ RB
- 连接中断时，未送达消息不会自动补发。
- 重连后从“当下”继续，依赖上游是否重放（当前链路未实现重放协议）。

2. RB ↔ Proxy
- broadcast lag 时直接丢消息（或客户端断开），不保证补齐。

3. RPC ↔ Proxy
- 客户端断线重连后无内建“从序号恢复”机制。

工程结论：
- 该系统为了时效性和简单性，优先低延迟扇出与故障快速恢复。
- 若要“至少一次/恰好一次”，需引入持久化日志、offset/ack、重放窗口、幂等键等额外协议层。

### 5.1 核心代码（Rust 节选）
```rust
// 仅消费实时流：没有 offset/ack/replay 请求路径
pub async fn run(&mut self, mut stream: mpsc::Receiver<FlashblocksPayloadV1>) {
    while let Some(event) = stream.recv().await {
        self.on_event(FlashblocksEngineMessage::FlashblocksPayloadV1(event))
            .await;
    }
}

// lagged 后不会回补，只是继续/断开
Err(RecvError::Lagged(_)) => {
    info!(message = "client is lagging", client = client_id);
    metrics.lagged_connections.increment(1);
    break;
}
```

## 6. 逐段最佳实践映射

### 6.1 这套实现做得好的点
1. 把“连接活性”与“业务处理”分离（ping task + message task）。
2. 全链路有 bounded queue，避免无限内存膨胀。
3. payload_id + index 双重约束，防跨块污染与乱序扩展。
4. proxy 有接入治理（鉴权/限流/心跳），可多实例 + Redis。

### 6.2 仍可增强的点
1. 端到端重放能力：断线后按 `(payload_id,index)` 请求补齐。
2. 消息级幂等键：对重复包显式去重统计。
3. 出站更细粒度背压策略：按 app/client 级水位与降级（drop newest/oldest/priority）。
4. Redis rate-limit 原子化（脚本或事务）以避免部分成功导致计数偏差。
