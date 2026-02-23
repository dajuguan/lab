# Solana Turbine 架构设计分析（需求分析 + 概要设计）

本文基于以下输入：
- 架构方法论：`./architecture/README.md`
- Turbine 相关实现：
  - `turbine/src/cluster_nodes.rs`
  - `turbine/src/broadcast_stage.rs`
  - `turbine/src/broadcast_stage/standard_broadcast_run.rs`
  - `turbine/src/broadcast_stage/broadcast_utils.rs`
  - `turbine/src/retransmit_stage.rs`
  - `turbine/src/addr_cache.rs`
  - `turbine/src/sigverify_shreds.rs`
  - `turbine/src/xdp.rs`

## Deprecated 说明（避免误解）

- docs.rs 上 `solana_turbine` 的 deprecated，指的是 crate/API 组织方式在迁移（并入 Agave 的 unstable API feature开关），不是 Turbine 协议在网络层被替换。
- 在当前代码中，`turbine` crate 由 `#![cfg(feature = "agave-unstable-api")]` 门控；未开启该 feature 时该 crate 不参与编译。
- 因此应区分：
  API 层 deprecated != turbine模块儿被停用。

## Scope / Non-goals

本稿聚焦两件事：
1. 子系统关键规格（高频对接接口）。
2. 接口如何串成 Turbine 主链路。

`In scope`
- 需求分析（功能/非功能/稳定点）。
- 高频跨子系统接口的完整参数签名。
- 广播、校验、重传、缓存、XDP 的接口编排。

`Out of scope`
- 常量参数清单。
- 低频内部 helper 函数逐项说明。
- 运维/调优 runbook。

## 1. 需求分析

### 1.1 根源需求

1. 高吞吐下稳定扩散 shreds。
2. 在 gossip/epoch 视图变化下维持确定性传播树。
3. 通过 Erasure Coding 降低 egress 与 repair 放大。

### 1.2 功能需求

1. Leader 广播：`WorkingBankEntry -> data/coding shreds -> 网络发送 + 落盘`。
2. Ingress 校验：签名验证 + retransmitter 语义验证 + 重签。
3. Validator 重传：去重、路由、下游发送、slot 统计。
4. 缓存预热：空闲时预计算重传目标地址。
5. 发送后端：UDP/XDP 并列。

### 1.3 非功能需求

1. Correctness：未验证包不得进入重传主链路。
2. Low latency：批处理与并行执行。
3. Network efficiency：Erasure Coding + batching。
4. Resilience：通道断开/slot 中断/缓存 miss 时可收敛。
5. Observability：广播与重传的阶段/slot 级指标。

## 2. 概要设计（Core / Peripheral）

Core
1. `cluster_nodes`：传播拓扑与路由关系。
2. `broadcast_stage`：leader 侧 shred 生产与发送编排。
3. `retransmit_stage`：validator 侧重传执行引擎。
4. Erasure Coding：嵌入 broadcast 主链路。

Peripheral
1. `sigverify_shreds`：入站验证与重签。
2. `addr_cache`：热点地址缓存与预热。
3. `xdp`：高性能发送后端。

## 3. 子系统关键规格（只保留高频对接接口）

### 3.1 `broadcast_stage` 关键规格

`BroadcastStageType` 对外构造入口：
```rust
pub fn new_broadcast_stage(
    &self,
    sock: Vec<UdpSocket>,
    cluster_info: Arc<ClusterInfo>,
    receiver: Receiver<WorkingBankEntry>,
    retransmit_slots_receiver: Receiver<Slot>,
    exit_sender: Arc<AtomicBool>,
    blockstore: Arc<Blockstore>,
    bank_forks: Arc<RwLock<BankForks>>,
    shred_version: u16,
    xdp_sender: Option<XdpSender>,
    votor_event_sender: VotorEventSender,
) -> BroadcastStage
```

`BroadcastRun`（广播执行契约，`StandardBroadcastRun` 实现）：
```rust
trait BroadcastRun {
    fn run(
        &mut self,
        keypair: &Keypair,
        blockstore: &Blockstore,
        receiver: &Receiver<WorkingBankEntry>,
        socket_sender: &Sender<(Arc<Vec<Shred>>, Option<BroadcastShredBatchInfo>)>,
        blockstore_sender: &Sender<(Arc<Vec<Shred>>, Option<BroadcastShredBatchInfo>)>,
    ) -> Result<()>;

    fn transmit(
        &mut self,
        receiver: &TransmitReceiver,
        cluster_info: &ClusterInfo,
        sock: BroadcastSocket,
        bank_forks: &RwLock<BankForks>,
    ) -> Result<()>;

    fn record(&mut self, receiver: &RecordReceiver, blockstore: &Blockstore) -> Result<()>;
}
```

广播发送核心接口：
```rust
pub fn broadcast_shreds(
    socket: BroadcastSocket,
    shreds: &[Shred],
    cluster_nodes_cache: &ClusterNodesCache<BroadcastStage>,
    last_datapoint_submit: &AtomicInterval,
    transmit_stats: &mut TransmitShredsStats,
    cluster_info: &ClusterInfo,
    bank_forks: &RwLock<BankForks>,
    socket_addr_space: &SocketAddrSpace,
) -> Result<()>
```

Erasure Coding 生产入口（高频）：
```rust
fn process_receive_results(
    &mut self,
    keypair: &Keypair,
    blockstore: &Blockstore,
    socket_sender: &Sender<(Arc<Vec<Shred>>, Option<BroadcastShredBatchInfo>)>,
    blockstore_sender: &Sender<(Arc<Vec<Shred>>, Option<BroadcastShredBatchInfo>)>,
    receive_results: ReceiveResults,
    process_stats: &mut ProcessShredsStats,
) -> Result<()>
```

```rust
fn entries_to_shreds(
    &mut self,
    keypair: &Keypair,
    entries: &[Entry],
    reference_tick: u8,
    is_slot_end: bool,
    process_stats: &mut ProcessShredsStats,
    max_data_shreds_per_slot: u32,
    max_code_shreds_per_slot: u32,
) -> std::result::Result<Vec<Shred>, BroadcastError>
```

### 3.2 `cluster_nodes` 关键规格

epoch 视图缓存接口（broadcast/sigverify/retransmit 共享）：
```rust
pub(crate) fn get(
    &self,
    shred_slot: Slot,
    root_bank: &Bank,
    working_bank: &Bank,
    cluster_info: &ClusterInfo,
) -> Arc<ClusterNodes<T>>
```

重传下游地址计算接口（重传主路径高频调用）：
```rust
pub fn get_retransmit_addrs(
    &self,
    slot_leader: &Pubkey,
    shred: &ShredId,
    fanout: usize,
    socket_addr_space: &SocketAddrSpace,
) -> Result<(u8, Vec<SocketAddr>), Error>
```

重签校验依赖的 parent 计算接口：
```rust
pub(crate) fn get_retransmit_parent(
    &self,
    leader: &Pubkey,
    shred: &ShredId,
    fanout: usize,
) -> Result<Option<Pubkey>, Error>
```

### 3.3 `retransmit_stage` 关键规格

重传 stage 对外入口：
```rust
pub fn new(
    bank_forks: Arc<RwLock<BankForks>>,
    leader_schedule_cache: Arc<LeaderScheduleCache>,
    cluster_info: Arc<ClusterInfo>,
    retransmit_sockets: Arc<Vec<UdpSocket>>,
    retransmit_receiver: Receiver<Vec<shred::Payload>>,
    max_slots: Arc<MaxSlots>,
    rpc_subscriptions: Option<Arc<RpcSubscriptions>>,
    slot_status_notifier: Option<SlotStatusNotifier>,
    xdp_sender: Option<XdpSender>,
    votor_event_sender: Sender<VotorEvent>,
) -> Self
```

重传主循环接口：
```rust
fn retransmit(
    thread_pool: &ThreadPool,
    bank_forks: &RwLock<BankForks>,
    leader_schedule_cache: &LeaderScheduleCache,
    cluster_info: &ClusterInfo,
    retransmit_receiver: &Receiver<Vec<shred::Payload>>,
    retransmit_sockets: &[UdpSocket],
    xdp_sender: Option<&XdpSender>,
    stats: &mut RetransmitStats,
    cluster_nodes_cache: &ClusterNodesCache<RetransmitStage>,
    addr_cache: &mut AddrCache,
    shred_deduper: &mut ShredDeduper,
    max_slots: &MaxSlots,
    rpc_subscriptions: Option<&RpcSubscriptions>,
    slot_status_notifier: Option<&SlotStatusNotifier>,
    shred_buf: &mut Vec<Vec<shred::Payload>>,
    votor_event_sender: &Sender<VotorEvent>,
    migration_status: &MigrationStatus,
) -> Result<(), ()>
```

单 shred 重传接口：
```rust
fn retransmit_shred(
    shred: shred::Payload,
    root_bank: &Bank,
    shred_deduper: &ShredDeduper,
    cache: &HashMap<Slot, (Pubkey, Arc<ClusterNodes<RetransmitStage>>)>,
    addr_cache: &AddrCache,
    socket_addr_space: &SocketAddrSpace,
    socket: RetransmitSocket<'_>,
    stats: &RetransmitStats,
) -> Option<RetransmitShredOutput>
```

地址获取接口（命中缓存或回退实时计算）：
```rust
fn get_retransmit_addrs<'a>(
    shred: &ShredId,
    cache: &HashMap<Slot, (Pubkey, Arc<ClusterNodes<RetransmitStage>>)>,
    addr_cache: &'a AddrCache,
    socket_addr_space: &SocketAddrSpace,
    stats: &RetransmitStats,
) -> Option<(u8, Cow<'a, [SocketAddr]>)>
```

### 3.4 `sigverify_shreds` 关键规格

校验 stage 对外入口：
```rust
pub fn spawn_shred_sigverify(
    cluster_info: Arc<ClusterInfo>,
    bank_forks: Arc<RwLock<BankForks>>,
    leader_schedule_cache: Arc<LeaderScheduleCache>,
    shred_fetch_receiver: Receiver<PacketBatch>,
    retransmit_sender: EvictingSender<Vec<shred::Payload>>,
    verified_sender: Sender<Vec<(shred::Payload, bool)>>,
    num_sigverify_threads: NonZeroUsize,
) -> JoinHandle<()>
```

校验主循环接口：
```rust
fn run_shred_sigverify<const K: usize>(
    thread_pool: &ThreadPool,
    keypair: &Keypair,
    cluster_info: &ClusterInfo,
    bank_forks: &RwLock<BankForks>,
    leader_schedule_cache: &LeaderScheduleCache,
    deduper: &Deduper<K, [u8]>,
    shred_fetch_receiver: &Receiver<PacketBatch>,
    retransmit_sender: &EvictingSender<Vec<shred::Payload>>,
    verified_sender: &Sender<Vec<(shred::Payload, bool)>>,
    cluster_nodes_cache: &ClusterNodesCache<RetransmitStage>,
    cache: &RwLock<LruCache>,
    stats: &mut ShredSigVerifyStats,
    shred_buffer: &mut Vec<PacketBatch>,
) -> Result<(), ShredSigverifyError>
```

重签校验接口：
```rust
fn maybe_verify_and_resign_packet(
    packet: &mut PacketRefMut,
    root_bank: &Bank,
    working_bank: &Bank,
    cluster_info: &ClusterInfo,
    leader_schedule_cache: &LeaderScheduleCache,
    cluster_nodes_cache: &ClusterNodesCache<RetransmitStage>,
    stats: &ShredSigVerifyStats,
    keypair: &Keypair,
) -> Result<(), ResignError>
```

### 3.5 `addr_cache` 关键规格

仅保留高频路径上的两个接口：
```rust
pub(crate) fn get(&self, shred: &ShredId) -> Option<(u8, &[SocketAddr])>
```

```rust
pub(crate) fn record(&mut self, slot: Slot, stats: &mut RetransmitSlotStats)
```

### 3.6 `xdp` 关键规格

XDP 发送接口（broadcast/retransmit 高频调用）：
```rust
pub(crate) fn try_send(
    &self,
    sender_index: usize,
    addr: impl Into<XdpAddrs>,
    payload: shred::Payload,
) -> Result<(), TrySendError<(XdpAddrs, shred::Payload)>>
```

## 4. 接口如何串起来（关键编排链路）

### 4.1 Leader 广播链路（含 Erasure Coding）

1. 入口构造
- `BroadcastStageType::new_broadcast_stage` 构造 `BroadcastStage` 并注入 `BroadcastRun` 实现。

2. 生产线程
- `BroadcastRun::run`（`StandardBroadcastRun::run`）拉取 entry。
- `process_receive_results` 调 `entries_to_shreds` 生成 data/coding。
- 生产出的同一批 `Arc<Vec<Shred>>` 同时送入：
  - `socket_sender`（网络发送路径）
  - `blockstore_sender`（持久化路径）

3. 发送线程
- `BroadcastRun::transmit` 调 `broadcast_shreds`。
- `broadcast_shreds` 内部按 slot 调 `ClusterNodesCache::get`，再据路由选目标并通过 UDP/XDP 发送。

4. 记录线程
- `BroadcastRun::record` 从 `RecordReceiver` 拉批并写入 blockstore。

### 4.2 Ingress 校验到重传链路

1. `spawn_shred_sigverify` 启动校验线程。
2. `run_shred_sigverify` 批量 dedup + 签名校验。
3. `maybe_verify_and_resign_packet` 使用 `ClusterNodesCache::get` 与 `get_retransmit_parent` 做 retransmitter 语义校验。
4. 通过后发送到 `retransmit_sender`，进入重传主循环。

### 4.3 Validator 重传链路

1. `RetransmitStage::new` 初始化重传所需 bank/schedule/cache/socket/channel 依赖。
2. `retransmit` 批量消费 `retransmit_receiver`。
3. 对每个 shred 调 `retransmit_shred`。
4. `retransmit_shred` 通过 `get_retransmit_addrs` 取地址：
- `AddrCache::get` 命中则直接发送。
- miss 则调用 `ClusterNodes::get_retransmit_addrs` 实时计算。
5. 发送完成后经 `AddrCache::record` 回写统计与地址，形成“实时路径 -> 缓存热化”闭环。

### 4.4 Reed-Solomon 生成与转发设计（补充）

1. 生成位置（编码发生在哪里）
- 语义上是先聚齐一个 FEC set 的 data shreds（例如 32 份），leader 在本地完成 Reed-Solomon 编码得到 data+coding（例如 64 份）后，才进入 Turbine 逐 shred 传播。
- Turbine 侧入口：`StandardBroadcastRun::entries_to_shreds`（`turbine/src/broadcast_stage/standard_broadcast_run.rs`）。
- 继续调用：`Shredder::make_merkle_shreds_from_entries` -> `make_shreds_from_data_slice`（`ledger/src/shredder.rs`）。
- 真正 Reed-Solomon 编码：`shred::merkle::finish_erasure_batch` 中
  `reed_solomon_cache.get(...).encode(...)`（`ledger/src/shred/merkle.rs`）。

2. 广播发包粒度（leader 到第一跳）
- `broadcast_shreds` 是“按 shred 选一个目标节点”，不是“给每个节点发送全部 shreds”。
- 对每个 data/coding shred，调用 `get_broadcast_peer` 得到单一目标地址并发送。
- 结论：单个节点在 leader 这一跳只收到 shreds 子集（包含部分 coding），不会收到全部 Reed-Solomon codes。

3. 全网扩散方式（后续跳数）
- 第一跳之后由 `retransmit_stage::retransmit_shred` 按 Turbine 树继续扩散。
- 因此“全量覆盖”靠多跳传播完成，不是 leader 对每个节点全量单播。

4. 视角定义（避免歧义）
- 时间视角（单个 shred）：固定同一个 `shred_id`，传播顺序是 `leader -> hop1 -> hop2 -> ...`，hop1 先于 hop2。
- 分片视角（同一 FEC set 的多个 shred）：每个 `shred_id` 都有各自的树和 hop 分层。
- **因而同一节点在 `shred A` 可能是 hop1，在 `shred B` 可能是 hop2；这并不矛盾，因为它们属于不同 shred 的传播树**。
- 节点累计“任意 32 份”发生在同一FEC set 的多个 shred 叠加接收过程中，而不是在单个 shred 的单条路径内完成。

5. coding 在补发路径的处理
- `BroadcastStage::check_retransmit_signals` 会分别从 blockstore 读取
  `get_data_shreds_for_slot` 与 `get_coding_shreds_for_slot`，并重新送入发送队列。

6. 设计收益
- leader egress 压力不会因“对每个节点发送全部 coding”而线性爆炸。
- 结合多跳与 FEC，系统在带宽和恢复能力之间取得平衡。

## 5. 子系统不变量（与关键接口绑定）

1. `BroadcastRun::run/transmit/record`：同一批 shreds 的网络发送与落盘必须保持一致批语义。
2. `entries_to_shreds`：必须同时生成 data/coding，不能退化为 data-only。
3. `ClusterNodes::get_retransmit_addrs`：必须拒绝 leader loopback。
4. `run_shred_sigverify/maybe_verify_and_resign_packet`：未验证包不得进入 `retransmit_sender`。
5. `retransmit/retransmit_shred`：缓存 miss 只能影响性能，不能影响正确性（必须可回退实时计算）。
6. `XdpSender::try_send`：只是发送后端替换，不改变路由和编码语义。

## 6. 失败语义（接口级）

1. `broadcast_shreds` / `retransmit_shred` 发送失败
- 语义：记录失败并继续处理后续批次，避免全局阻塞。

2. `run_shred_sigverify` channel 错误
- 语义：`RecvDisconnected/SendError` 导致线程退出；`RecvTimeout` 仅一轮空转。

3. `get_retransmit_addrs` 路由错误
- 语义：当前 shred 丢弃并计数，不传播错误路由状态。

4. `BroadcastRun::run` 在 slot 中断
- 语义：收敛旧 slot，并在新 slot 视图下继续生成/发送。

## 7. 小结

Turbine 的关键不是“函数多”，而是“接口契约清晰且可编排”：
1. 广播接口负责 data/coding 生成与双通道分发。
2. 校验接口负责把未验证输入挡在重传链路之外。
3. 重传接口负责在正确性不变前提下用缓存和后端优化吞吐。
