# Commonware 架构分析（按“需求分析 -> 概要设计”）

本文档基于当前仓库代码结构与公开接口整理，目标是提供一份可持续演进的架构基线。

## 1. 需求分析

### 1.1 根源需求

从仓库目标与核心 primitive 定位看，系统的根源需求是：

1. 在对抗环境中构建可组合的分布式系统原语。
2. 在高性能要求下保证正确性与确定性。
3. 通过清晰的稳定性分级支持长期演进。

对应证据：

- Workspace 与 primitive 全景：`Cargo.toml`
- 稳定性分级治理：`README.md`
- 核心并发与确定性执行能力：`runtime/src/lib.rs`、`runtime/src/deterministic.rs`

### 1.2 需求归类

#### A. 安全与对抗鲁棒性

- 需求：面对不可信输入、恶意参与者、网络抖动与部分失效，系统仍可保持安全性与可恢复性。
- 关键模块：`consensus`、`cryptography`、`p2p`、`resolver`、`storage`。

#### B. 正确性与确定性

- 需求：测试和关键行为可复现，可审计。
- 关键模块：`runtime/src/deterministic.rs` 的确定性运行与审计状态。

#### C. 可移植与可替换

- 需求：业务 primitive 与具体 runtime 解耦，算法可在不同执行策略上复用。
- 关键模块：`runtime` trait 抽象、`parallel` 的 `Strategy`。

#### D. 性能与资源效率

- 需求：避免不必要分配，支持并行、缓冲池、分层执行。
- 关键模块：`runtime`、`parallel`、`coding`、`storage`。

#### E. 兼容性与演进治理

- 需求：在演进时控制破坏性变更，保证格式稳定性。
- 关键模块：`macros` 稳定性标注、`conformance` 机制、`conformance.toml` 基线文件。

### 1.3 稳定点与变化点

#### 稳定点

1. 跨子系统 trait 契约（如 `Runner`、`Spawner`、`Codec`、`Persistable`、`Automaton`）。
2. 稳定性等级机制（ALPHA/BETA/GAMMA/DELTA/EPSILON）及其 CI 检查。
3. Conformance 回归校验机制（哈希基线）。

#### 变化点

1. 具体算法实现（如编码方案、存储结构细节、协议优化策略）。
2. 运行后端与 feature 组合（tokio、deterministic、iouring）。
3. ALPHA 范围内 API 与机制可快速迭代。

## 2. 概要设计

概要设计目标：将系统正交拆分为子系统，并通过稳定接口串联。

### 2.1 为什么分解为这 6 个子系统（正交性依据）

这里采用的是“责任正交”而不是“目录正交”。判据是：每个子系统都回答不同的问题，拥有不同的一组不变量，且可被独立替换/演进。

1. S1 基础契约层：回答“数据和密码学对象在语义上如何被表达与验证”。
2. S2 执行与资源层：回答“任务、时间、网络、存储能力如何被统一调度与提供”。
3. S3 通信与分发层：回答“消息如何在不可靠网络中被发送、接收、限流、拉取”。
4. S4 一致性控制层：回答“在拜占庭环境下如何达成顺序与提交”。
5. S5 持久化层：回答“状态如何持久化、恢复、修剪并维持一致性”。
6. S6 机制算法层：回答“编码/数学机制如何提供可验证的算法保证”。

这 6 类问题分别对应不同故障模型与变化速率：

1. 契约错误是“语义错误”。
2. 执行层错误是“调度/资源错误”。
3. 通信层错误是“网络与对端行为错误”。
4. 共识层错误是“协议安全性错误”。
5. 存储层错误是“崩溃一致性错误”。
6. 算法层错误是“正确性与复杂度错误”。

因此把它们拆开后，可以在不跨层污染的前提下做局部优化或替换，例如：替换并行策略不应影响共识安全规则；替换网络实现不应改变编码契约。

### 2.2 这些边界是如何确定的（边界判定规则）

边界不是按 crate 名字硬切，而是按以下 4 条规则确定：

1. 不变量归属规则：
一个规则只能由一个子系统“最终负责”。例如任务监督与停机协议只归 S2，不能散落到 S3/S4。

2. 抽象最小闭包规则：
如果一组接口能独立形成最小闭环，就应成为边界。例如 `Codec`（读/写/大小/解码）形成完整数据契约闭环，故归 S1。

3. 故障域隔离规则：
同一类故障应在同一层内被处理。网络对端恶意行为在 S3 处理（如 `Blocker`）；协议活性/安全在 S4 处理。

4. 演进速率分离规则：
高频变化实现与低频稳定契约隔离。实现可在 ALPHA 快速演进，但跨层 trait 语义尽量稳定。

在当前仓库中，这些规则落地为如下边界信号：

1. trait 边界：如 `Runner/Spawner`、`Sender/Receiver`、`Automaton`、`Persistable`。
2. 配置边界：各层 `Config` 只描述本层策略，不越层携带实现细节。
3. 生命周期边界：典型入口统一为 `new/init/start`，退出由 `stop/sync/destroy` 收口。
4. 稳定性边界：`stability_scope!` 标注决定 API 可见与变更权限。

### 2.3 子系统分解与责任（不变量）

#### 子系统 S1：基础契约层

- 责任：定义跨 primitive 的公共语义与边界，不承载业务策略。
- 不变量：
1. 序列化契约一致（`Codec`）。
2. 加密与摘要契约自洽（`Signer`/`PublicKey`/`Digestible`/`Committable`）。
3. 并行策略可替换且结果语义一致（`Strategy`）。
- 核心规格（抽象）：
1. `codec::Read::read_cfg(buf, cfg) -> Result<Self, Error>`：不可信输入解码入口。
2. `codec::Encode::encode() -> Bytes`、`codec::Decode::decode_cfg(buf, cfg)`：编码/解码闭环。
3. `codec::Codec = Encode + Decode`：跨模块通用数据边界。
4. `cryptography::Signer::sign(namespace, msg)`、`Verifier::verify(namespace, msg, sig)`：签名验签闭环。
5. `cryptography::Digestible::digest()` 与 `Committable::commitment()`：唯一标识与承诺语义。
6. `parallel::Strategy::fold/fold_init`：算法执行策略解耦（顺序/并行可替换）。
- 核心规格（关键结构体）：
1. `coding::Config { minimum_shards, extra_shards }` 与 `Config::total_shards()`。
2. `coding::CodecConfig { maximum_shard_size }`：分片编解码边界约束。

#### 子系统 S2：执行与资源层（Runtime）

- 责任：提供任务调度、监督、时钟、网络、存储能力抽象。
- 不变量：
1. 任务生命周期受监督（父任务结束会终止子任务链）。
2. 停机信号语义一致（`stop/stopped`）。
3. 确定性 runtime 在固定 seed 下可复现。
- 核心规格（抽象）：
1. `Runner::start(root)`：运行根任务。
2. `Spawner::{spawn, shared, dedicated, stop, stopped}`：任务派生与停机协议。
3. `Clock::{current, sleep, sleep_until, timeout}`：统一时钟与超时语义。
4. `Network::{bind, dial}` + `Listener::accept` + `Sink::send` + `Stream::{recv, peek}`：网络 I/O 抽象。
5. `Storage::{open, open_versioned, remove, scan}` + `Blob::{read_at, write_at, resize, sync}`：存储 I/O 抽象。
6. `Metrics::{with_label, with_attribute, register, encode}`：运行时指标上下文传播。
- 核心规格（关键结构体）：
1. `deterministic::Config`：`with_seed/with_timeout/with_storage_fault_config` 等可复现测试配置。
2. `deterministic::Runner::{new, seeded, timed, start_and_recover}`：确定性执行与恢复。
3. `deterministic::Auditor::state()`：确定性状态审计输出。

#### 子系统 S3：通信与分发层

- 责任：在认证加密与限流前提下完成点对点通信、广播、请求收集与数据解析。
- 不变量：
1. 发送语义区分“校验失败”和“离线丢包”。
2. 接收侧消息需满足通道配置并完成认证解密。
3. 解析与拉取流程对失败路径有显式建模（`failed/cancel/retain`）。
- 核心规格（抽象）：
1. `p2p::Sender::send(recipients, message, priority)`、`Receiver::recv()`。
2. `p2p::Provider::{peer_set, subscribe}`、`Manager::track`、`Blocker::block`。
3. `resolver::Resolver::{fetch, fetch_targeted, cancel, retain}`、`Consumer::{deliver, failed}`。
4. `broadcast::Broadcaster::broadcast(recipients, message)`。
5. `collector::Originator::{send, cancel}`、`Handler::process`、`Monitor::collected`。
- 核心规格（关键结构体/类型）：
1. `p2p::Recipients::{All, Some, One}`、`p2p::Message<P> = (P, IoBuf)`、`p2p::Channel = u64`。
2. `p2p::types::Ingress::{Socket, Dns}`，`Ingress::resolve/resolve_filtered`。
3. `p2p::types::Address::{Symmetric, Asymmetric}`，`Address::ingress/egress/egress_ip`。
- 核心规格（实现入口）：
1. `p2p::authenticated::lookup::Network::{new, register, start}`。
2. `p2p::authenticated::discovery::Network::{new, register, start}`。
3. `resolver::p2p::Engine::{new, start}` + `resolver::p2p::Config`。
4. `broadcast::buffered::Engine::{new, start}` + `broadcast::buffered::Config`。
5. `collector::p2p::Engine::{new, start}` + `collector::p2p::Config`。

#### 子系统 S4：一致性控制层

- 责任：在拜占庭环境中推进排序、认证、提交流程。
- 不变量：
1. 共识推进接口与应用逻辑分离（`Automaton`）。
2. 认证阶段判定必须确定性（`CertifiableAutomaton`）。
3. 外部数据传播与报告能力可替换（`Relay`、`Reporter`）。
- 核心规格（抽象）：
1. `consensus::Automaton::{genesis, propose, verify}`：应用提议/验证边界。
2. `consensus::CertifiableAutomaton::certify(round, payload)`：finalize 前认证钩子。
3. `consensus::Relay::broadcast(payload)`：payload 扩散边界。
4. `consensus::Reporter::report(activity)`、`Monitor::subscribe()`：外部观测边界。
5. `consensus::Block::parent()` 与 `CertifiableBlock::context()`：区块与上下文契约。
- 核心规格（关键结构体/类型）：
1. `consensus::types::{Epoch, Height, View, Round}`：协议时序主键。
2. `simplex::Config`：共识参数总线（超时、fetch 并发、journal 配置等）。
- 核心规格（实现入口）：
1. `consensus::simplex::Engine::{new, start}`。
2. `start` 需要三组网络通道：`vote_network`、`certificate_network`、`resolver_network`。

#### 子系统 S5：持久化层

- 责任：提供持久化结构与恢复语义，支撑 crash recovery 与状态管理。
- 不变量：
1. `commit/sync/destroy` 语义边界清晰。
2. 存储结构在重启后行为可预测。
3. 格式变更需经 conformance 流程显式确认。
- 核心规格（抽象）：
1. `storage::Persistable::{commit, sync, destroy}`：持久化生命周期标准语义。
- 核心规格（实现入口，当前主链路常用）：
1. `storage::journal::contiguous::fixed::Journal::init(context, cfg)`。
2. `Journal::{append, reader, replay, sync, rewind, prune, destroy}`。
3. `storage::journal::contiguous::fixed::Config { partition, items_per_blob, page_cache, write_buffer }`。
- 核心规格（链路关系）：
1. 共识层通过 runtime `Storage/Blob` 能力读写。
2. 结构化存储模块（journal/index/archive/metadata）在其上封装恢复与校验逻辑。

#### 子系统 S6：机制算法层

- 责任：提供可验证的编码/数学机制，服务上层协议。
- 不变量：
1. 编码-验片-重构语义闭环一致（`Scheme`）。
2. 同承诺下不同碎片组合重构结果一致或显式失败。
- 核心规格（抽象）：
1. `coding::Scheme::{encode, reshard, check, decode}`。
2. `coding::ValidatingScheme`：`check` 足以证明编码有效性的标记语义。
- 核心规格（实现）：
1. `ReedSolomon`、`Zoda`、`NoCoding` 三类方案挂在统一 `Scheme` 接口。

### 2.4 子系统边界与串联关系

典型链路可抽象为：

`S1契约 -> S2执行 -> S3通信 -> S4一致性 -> S5持久化`

机制算法 `S6` 作为横向能力嵌入 `S3/S4/S5`。

边界原则：

1. 通过 trait 耦合，不通过具体实现耦合。
2. 稳定性分级决定可见 API 面与允许变更范围。
3. 所有跨边界数据应可编码、可验证、可回归测试。

### 2.5 稳定与变化范围映射

#### 稳定区域（优先保持）

1. 跨子系统 trait 接口语义。
2. 数据格式稳定区域（BETA 及以上）。
3. 稳定性标注与 conformance 工作流本身。

#### 变化区域（允许快速迭代）

1. ALPHA 模块内部实现与接口。
2. 各 primitive 的性能优化策略。
3. 特定后端/feature 的平台化细节。

## 3. 重大风险与消减策略

### 3.1 重大风险

1. Runtime 能力接口膨胀，导致跨 primitive 耦合扩大。
2. 共识链路集成复杂度高（`consensus + p2p + resolver + storage`）。
3. 格式变更无意引入兼容性破坏。

### 3.2 已有消减机制

1. 稳定性标注与检查：`stability_scope!` + `just check-stability`。
2. Conformance 哈希基线：`conformance` crate + 各模块 `conformance.toml`。
3. 确定性回归能力：`runtime` deterministic 执行与审计状态。

## 4. 核心规格清单（可串联）

本节给出一条最小可运行链路的“结构体 + 公开函数”规格。

1. 运行时上下文：
- `deterministic::Runner::timed(timeout).start(|ctx| async move { ... })`
- `ctx` 需要满足 `Spawner + Clock + Network + Storage + Metrics` 等能力 trait。

2. 网络建立：
- `p2p::authenticated::{lookup|discovery}::Network::new(ctx, cfg)`
- `network.register(channel, quota, backlog)` 获取 `(Sender, Receiver)`
- `network.start()`

3. 数据分发/拉取：
- `resolver::p2p::Engine::new(ctx, resolver_cfg)` 返回 `(engine, mailbox)`
- `engine.start((net_sender, net_receiver))`
- 业务侧通过 `mailbox` 触发 fetch/cancel；consumer 通过 `deliver/failed` 接收结果。

4. 共识推进：
- `consensus::simplex::Engine::new(ctx, simplex_cfg)`
- `engine.start(vote_network, certificate_network, resolver_network)`
- 其中应用实现 `Automaton`/`CertifiableAutomaton`，网络层实现 `Relay`，观测层实现 `Reporter`。

5. 持久化：
- `storage::journal::contiguous::fixed::Journal::init(ctx, journal_cfg)`
- `journal.append(item) -> position`
- `journal.sync()` 保证落盘
- `journal.reader().await.replay(...)` 或 `journal.rewind/prune` 支持恢复与维护

6. 数据契约：
- 所有跨网络/跨存储对象需满足 `Codec`。
- 共识对象需满足 `Digestible + Committable`，保证 digest/commitment 语义一致。

## 5. 建议的后续架构动作

1. 增加一张“依赖方向图”，明确禁止反向依赖（尤其业务层反向依赖 runtime 具体实现）。
2. 为核心链路建立“架构级回归用例矩阵”：确定性、Byzantine、恢复、格式兼容四类。
3. 给每个子系统补一页 ADR（Architecture Decision Record），记录已定边界与变更准入规则。

## 6. Workspace 依赖方向图（文本版）

说明：以下为架构分析视角下的“生产依赖方向”，忽略部分 crate 内部测试用的自引用依赖（例如 `commonware-consensus -> commonware-consensus`）。

### 6.1 分层视图

L0 基础层：

- `commonware-macros`
- `commonware-math`
- `commonware-parallel`
- `commonware-conformance`

L1 公共契约层：

- `commonware-codec` -> `commonware-conformance`(可选)
- `commonware-utils` -> `commonware-codec`
- `commonware-cryptography` -> `commonware-math`

L2 执行与存储能力层：

- `commonware-runtime` -> `commonware-cryptography`, `commonware-macros`, `commonware-parallel`, `commonware-utils`, `commonware-conformance`(可选)
- `commonware-storage` -> `commonware-codec`, `commonware-cryptography`, `commonware-utils`, `commonware-parallel`(可选), `commonware-runtime`(可选)

L3 网络与机制层：

- `commonware-stream` -> `commonware-cryptography`, `commonware-macros`
- `commonware-p2p` -> `commonware-macros`, `commonware-parallel`, `commonware-runtime`
- `commonware-broadcast` -> `commonware-macros`, `commonware-runtime`
- `commonware-resolver` -> `commonware-macros`, `commonware-runtime`
- `commonware-collector` -> `commonware-macros`, `commonware-runtime`
- `commonware-coding` -> `commonware-codec`, `commonware-cryptography`, `commonware-parallel`, `commonware-storage`, `commonware-utils`

L4 协议编排层：

- `commonware-consensus` -> `commonware-math`, `commonware-macros`, `commonware-parallel`, `commonware-storage`, `commonware-resolver`, `commonware-runtime`

L5 外围工具层：

- `commonware-deployer` -> `commonware-cryptography`(可选)

### 6.2 依赖方向约束（建议）

1. 高层不得反向依赖低层实现细节，只依赖低层抽象。
2. 业务协议层（如 `consensus`）不得引入具体 runtime 实现（如 tokio 模块），只依赖 `runtime` trait 能力。
3. 通信层（`p2p`/`resolver`/`broadcast`/`collector`）不得直接绑定共识实现，保持可复用。
4. 存储格式相关变更进入 BETA 区域后，必须经 conformance 基线更新流程。

## 7. 端到端时序（`p2p -> resolver -> consensus -> storage`）

### 7.1 正常路径（抽象）

1. 节点通过 `p2p` 接收消息并完成认证/解密，产出可处理 payload。
2. 若 payload 仅有摘要或缺失正文，`resolver` 发起 `fetch/fetch_targeted` 拉取数据并交给 `Consumer::deliver` 校验。
3. `consensus` 通过 `Automaton::verify` 验证 payload 有效性，进入投票/认证推进。
4. 达到协议条件后，`consensus` 触发 finalize（必要时经过 `CertifiableAutomaton::certify`）。
5. 应用将最终结果写入 `storage`，并调用 `commit/sync` 固化状态。
6. 重启后由 `storage` 恢复状态，`consensus` 从已持久化进度继续。

### 7.2 关键不变量（链路级）

1. 任一进入共识的数据在语义上可被唯一标识（digest/commitment 一致）。
2. `resolver` 失败路径显式可观测（`failed/cancel/retain`），不会隐式吞错。
3. `consensus` 的认证决策在诚实节点间必须确定性一致。
4. `storage` 的持久化边界清晰：`sync` 完成后应满足崩溃恢复预期。

### 7.3 最小验证矩阵（建议）

1. 确定性：固定 seed 下重复运行，审计状态一致（`runtime/deterministic`）。
2. 对抗性：注入恶意/冲突消息，验证阻断与恢复路径。
3. 恢复性：在 finalize 前后注入崩溃，验证重启恢复点正确。
4. 兼容性：对涉及编码/存储格式的对象运行 conformance 校验。
