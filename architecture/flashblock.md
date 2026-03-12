# Flashblock 设计：需求分析与概要设计

本文基于 `/home/po/now/lab/architecture/README.md` 的架构方法论，结合一级 spec `docs/specs/flashblock_core.md` 与实现参考 `crates/builder/core/src/flashblocks/payload.rs`，给出 Flashblock 设计的**需求分析**与**概要设计**。目标是明确不变量、边界、接口与主链数据流，并对关键风险做隔离。

---

## 最核心原则
很难保证不产生bug，但是要保证`可观测、可降级、可回滚`这个最基础的链路足够robust。

## 1. 需求分析

### 1.1 根源需求
- **降低用户感知延迟**：在 OP Stack 仍维持 1–2s L2 block time 的前提下，提供 200ms 级的增量状态反馈。
- **不改协议**：保持 “out-of-protocol” 设计，避免硬分叉或全网升级。
- **可回退**：任何 Flashblock 组件故障都应能回退到标准 OP Stack 构块路径。

### 1.2 需求归类

**功能需求**
- 以固定间隔（`FLASHBLOCKS_TIME`，默认 200ms）持续产出增量 Flashblock。
- 通过 Rollup Boost 对 Flashblock 进行验证与传播。
- RPC Provider 能在 `pending` tag 下返回预确认状态。

**可靠性需求**
- 若 Builder 故障，仍应维持已发预确认（优先保持预确认完整性）。
- `engine_getPayload` 必须快速返回最终完整块（无需额外计算状态根）。

**兼容性需求**
- 不引入新的核心协议字段，仅扩展 RPC 语义或新增少量探测接口（`op_supportedCapabilities`）。
- RPC 侧对未支持 Flashblocks 的调用保持兼容退化。

### 1.3 稳定点与变化点

**稳定点**
- OP Stack 的区块生产周期与 `engine_forkchoiceUpdated`/`engine_getPayload` 交互流程。
- OP Stack 交易执行与系统交易必须在块内最先执行的规则。

**变化点**
- Flashblock 的节奏（固定或动态）与交易分配启发式。
- 传播与缓存策略（WebSocket 代理、RPC 状态缓存策略）。
- 元数据粒度（是否传输完整 metadata 或由 RPC 侧重放交易）。

### 1.4 不变量（必须保证）
- **单调序号**：Flashblock `index` 严格递增且连续。
- **不可变 base**：首个 flashblock 的 `ExecutionPayloadBaseV1` 在同一 L2 block 内不可变。
- **执行正确性**：每个 flashblock 必须能被 Sequencer 本地 EL 验证。
- **预确认保持**：一旦广播预确认，不可在同一块内反悔或“换块”。


---

## 2. 概要设计

### 2.1 子系统正交分解（核心 vs 外围）

**核心系统（Core System）**
- **Flashblock Builder（External Block Builder）**
  - 负责生成 Flashblock payload，包含状态根和增量交易。
  - 维护连续性、序号、不变量保证。

- **Rollup Boost（Sequencer 侧控制平面）**
  - 转发 FCU；验证 Flashblock；汇聚并在 `engine_getPayload` 时返回最终块。
  - 作为系统主链中最关键的边界适配器。

**外围系统（Peripheral Systems）**
- **WebSocket Proxy / Mirror**：对外广播 Flashblocks，解耦安全/带宽。
- **RPC Provider Flashblock Cache**：维护预确认状态 Overlay，并对 `pending` tag 做扩展。
- **Fallback EL**：Builder 不可用时的退路。

### 2.2 纵向主链（业务链路）

`Sequencer(op-node)` → `Rollup Boost` → `External Block Builder` → `Rollup Boost` → `WebSocket Proxy` → `RPC Providers` → `End Users`

**主链职责**
- FCU 触发构块 → builder 每 200ms 推送增量 → RB 验证并广播 → RPC 提供预确认 → 最终 `engine_getPayload` 打包正式块。

### 2.3 横向并列系统
- **Fallback EL**（并列构块路径，保障 liveness）
- **HA Sequencer**（多实例状态同步，涉及 preconfirmation 迁移）
- **RPC State Overlay**（可替换实现策略）

---

## 3. 关键数据结构（对外规格）

### 3.1 FlashblocksPayloadV1（Builder → RB → RPC）

- `payload_id: Bytes8`
- `index: uint64`
- `parent_flash_hash: Optional[Bytes32]`
- `base: Optional[ExecutionPayloadBaseV1]`（仅 index=0 有）
- `diff: ExecutionPayloadFlashblockDeltaV1`
- `metadata: FlashblocksMetadata`（可裁剪）

### 3.2 ExecutionPayloadBaseV1（首个 Flashblock 固定字段）
- `parent_hash, block_number, gas_limit, timestamp, base_fee_per_gas, ...`

### 3.3 ExecutionPayloadFlashblockDeltaV1（增量字段）
- `state_root, receipts_root, logs_bloom, gas_used, block_hash`
- `transactions, withdrawals, withdrawals_root`

### 3.4 元数据（供 RPC 预确认）
- `AccountMetadata`、`StorageSlot`、`TransactionMetadata`

---

## 4. 关键接口与最小规格（必须描述）

### 4.1 Builder ↔ Rollup Boost（WebSocket/SSZ，核心数据平面）

**接口类型**  
长连接 WebSocket，消息体为 SSZ 编码 `FlashblocksPayloadV1`。

**输入/输出**  
- Builder 输入：`engine_forkchoiceUpdated`（来自 RB 的控制面触发）。  
- Builder 输出：连续 `FlashblocksPayloadV1` 流（数据面增量）。  

**最小规格（必须保证）**  
- **有序性**：`index` 从 0 开始递增且连续，RB 遇到乱序/跳号必须丢弃并保持内部状态不被污染。  
- **单一性**：同一 `(payload_id, index)` 只能有一个 Flashblock（禁止 equivocation）。  
- **base 仅首次出现**：`index=0` 才携带 `ExecutionPayloadBaseV1`，后续 `base=None`。  
- **可重建性**：RB 仅靠已收到的增量即可重建完整块，不依赖额外同步调用。  

**处理方式（网络层）**  
- **版本探测**：SSZ 无 schema，必须通过前 4 字节 `version` 选择解析结构。  
- **连接中断**：Builder 可对当前块内消息做短期缓冲，重连后继续推送；进入新块周期时旧缓存丢弃。  
- **背压控制**：RB 作为接收端必须具备队列与限速策略，避免订阅扩散影响构块主链。  

**信任边界**  
- RB 必须对每条 flashblock 做本地 EL 验证，未经验证的消息不得外发。  
- RB 只接受来自已认证 Builder 的连接（建议 TLS + 认证）。  

### 4.2 Rollup Boost ↔ Sequencer EL（Engine API，核心控制面）

**接口类型**  
标准 Engine API RPC（通常 HTTP 或 IPC），同步请求 + 快速返回要求。  

**关键调用与处理方式**  
- `engine_forkchoiceUpdated`：RB 并发转发到 Builder 与 Sequencer 本地 EL，并依赖“payload_id 在不同 EL 间确定性一致”的假设以便聚合。  
- `engine_getPayload`：RB 必须直接返回“已验证 flashblocks 的聚合结果”，不允许在此时再同步请求 Builder 或重新计算状态根。  

**失败模式**  
- EL 验证超时或失败：RB 丢弃对应 flashblock，但不能破坏已发布序列。  
- Builder 缺失或 EL 故障：RB 走 Fallback EL，保证链上块可按时产出。  

### 4.3 Rollup Boost ↔ WebSocket Proxy（安全与扩散层）

**接口类型**  
WebSocket 字节流中继，Proxy 不需要理解 flashblock 语义。  

**处理方式与约束**  
- **隔离安全面**：RB 端点不直接对外开放，外部订阅必须经 Proxy。  
- **弹性扩散**：Proxy 负责多订阅转发，RB 仅维护有限连接数。  
- **速率保护**：Proxy 需具备限速与连接数控制，避免 DDoS 影响 RB。  

### 4.4 RPC Provider ↔ Proxy（消费者接口）

**接口类型**  
WebSocket 订阅 flashblock 流；本地维护 preconfirmation state overlay。  

**处理方式（语义层）**  
- **序列校验**：RPC 侧也必须按 `index` 连续性校验，遇到缺失/重复拒绝更新 overlay。  
- **`pending` tag 语义**：`eth_getBalance` / `eth_getBlockByNumber` 等方法基于 overlay 返回预确认状态。  
- **字段占位**：`blockHash` 可用空 hash，`blockNumber` 使用当前构块编号以兼容客户端。  

**能力探测**  
- `op_supportedCapabilities → ["flashblocksv1"]` 作为最小协商接口。  

---

## 5. Flashblock 构造流程（概览）

1. FCU 到达 Rollup Boost。
2. Rollup Boost 并发转发至 Builder + Sequencer EL。
3. Builder 每 `FLASHBLOCKS_TIME`：
   - 执行必需系统交易（仅首个 flashblock）。
   - 执行选定交易并生成 `diff`。
   - 生成 `state_root/receipts_root/logs_bloom`。
   - 推送 `FlashblocksPayloadV1`。
4. Rollup Boost 验证并广播至 RPC Providers。
5. `engine_getPayload` 时 RB 直接聚合已验证 Flashblocks。

---

## 6. 关键风险与隔离

**风险 1：Builder 宕机**
- 设计原则：保留已发预确认；后续块回退到 Fallback EL。

**风险 2：状态根计算成本**
- 设计原则：每次 flashblock 计算状态根，保证 `engine_getPayload` 快速返回。
- 可选优化：禁用部分状态根计算（实现中可配置）。

**风险 3：高负载下大交易被排挤**
- 设计原则：启发式 gas allocation；可限制 flashblock 数量或动态调整。

**风险 4：WebSocket 暴露安全面**
- 设计原则：通过 Proxy/Mirror 隔离，避免直接暴露 RB。

---

## 7. 概要设计结论

Flashblock 的核心设计在于：
- **保持协议不变**，将预确认作为旁路数据流。
- **核心系统小而强**：Builder 负责正确生成；Rollup Boost 负责验证与聚合。
- **外围系统可独立演进**：RPC Provider 预确认缓存策略、代理层、安全策略均可替换。

该设计遵循“先识别语义复杂核心，再按数据流拆外围”的方法论，核心不变量集中在 Builder + RB，外围可迭代优化而不破坏协议正确性。

---

## 8. 待验证 / 可选增强

- 预确认元数据是否可裁剪（RPC 侧重放交易）。
- HA Sequencer 中 Flashblock 状态的迁移策略。
- 动态 flashblock cadence 的收益/风险评估。
- 大交易公平性策略（max gas inclusion 机制）。

---

## 9. 代码对照：Engine API 控制面（RB ↔ EL）

本节基于 `rollup-boost` 实现，补充第 4.2 的“接口合同 + 失败降级”细节。

**代码锚点**
- `/home/po/now/rollup-boost/crates/rollup-boost/src/server.rs`
- `/home/po/now/rollup-boost/crates/rollup-boost/src/client/rpc.rs`
- `/home/po/now/rollup-boost/crates/rollup-boost/src/health.rs`
- `/home/po/now/rollup-boost/crates/rollup-boost/src/flashblocks/service.rs`

### 9.1 `engine_forkchoiceUpdatedV3` 合同

**输入**
- `fork_choice_state`
- `payload_attributes: Option<OpPayloadAttributes>`

**输出**
- RB 始终返回 L2 客户端响应（builder 响应用于内部协同，不直接透传给上游）

**关键分支**
- `execution_mode == disabled`：只走 L2
- builder 不健康且策略要求跳过：只走 L2
- `no_tx_pool == true`：只走 L2
- `payload_attributes` 存在且 `no_tx_pool == false`：并发 L2 + builder
- `payload_attributes == None`：L2 同步返回，builder 异步 `spawn` 同步链头

**状态跟踪**
- `payload_trace_context` 记录 `payload_id` 与 builder 是否参与
- `external_state_root` 开启时缓存 `payload_id -> FCU 请求`，供后续状态根流程

### 9.2 `engine_getPayloadV3/V4` 合同

**输入**
- `payload_id`
- `version`

**输出**
- 选择后的 payload（builder 或 L2）

**处理流程**
- 并发拉取 L2 payload 与 builder payload
- builder 失败/无 payload/不可达：回落 L2，不中断出块路径
- builder 成功时先本地 `new_payload` 验证，再参与最终选择
- `dry_run` 强制返回 L2
- 配置 `block_selection_policy` 时按策略二选一，否则优先 builder

### 9.3 时延预算与错误策略

**时延预算**
- RPC 客户端设置 `request_timeout`，防止同步调用无限等待
- 关键路径采用并发（`tokio::join!`），降低 builder 慢响应对上游时延的影响
- 非关键 builder 同步走异步任务，避免阻塞主线程

**失败模式与行为**
- builder 超时/不可达/响应错误：记录错误，最终返回 L2
- builder payload 本地校验失败：视为 builder 不可用，返回 L2
- L2 失败：主路径失败，健康降级为 `ServiceUnavailable`
- builder 失败但 L2 正常：健康降级为 `PartialContent`

### 9.4 Fallback EL 降级规则（可操作定义）

触发条件：
- `execution_mode` 禁用
- builder 健康检查不通过且策略要求跳过
- builder payload 获取失败
- builder payload 验证失败

降级行为：
- 立即选择并返回 L2 payload
- 不改变 Engine API 入口合同，不要求上游处理 builder 特殊错误

---

## 10. 代码对照：安全与扩散隔离（RB ↔ Proxy）

本节给出 `RB ↔ Proxy` 的落地控制点，明确“RB 高价值目标，Proxy 承担公网治理”。

**代码锚点**
- `/home/po/now/rollup-boost/crates/websocket-proxy/src/server.rs`
- `/home/po/now/rollup-boost/crates/websocket-proxy/src/main.rs`
- `/home/po/now/rollup-boost/crates/websocket-proxy/src/registry.rs`
- `/home/po/now/rollup-boost/crates/websocket-proxy/src/rate_limit.rs`

### 10.1 接入边界与认证

- 启用 API keys 时只暴露 `/ws/{api_key}`，无效 key 返回 `401`
- 未配置 API keys 时走公开 `/ws`
- 升级前先执行限流器 `try_acquire`，超限返回 `429`
- 支持按 header（默认 `X-Forwarded-For`）提取源 IP 做治理

### 10.2 DDoS 面与扩散控制

- 连接上限：instance / per-IP / per-app（可选 Redis 分布式）
- 下游扇出采用有界广播缓冲，慢消费者触发 lag 后断开，防止整体拖慢
- 可选 client ping/pong 健康检查，超时自动踢除僵尸连接
- 上游 subscriber 具备重连 backoff 与 ping/pong 探活，降低单链路抖动影响

### 10.3 角色分工（避免职责漂移）

- RB：负责 Engine API 正确性、payload 选择与 fallback
- Proxy：负责公网接入治理（认证、限流、连接管理、扇出）
- 建议部署边界：RB 不直连公网，只暴露给 proxy（内网、allowlist、TLS/mTLS）

---

## 11. 失败矩阵（可复用模板 + RB↔EL 示例）

本节给一个可复用的失败矩阵模板，目标是把“没想到全部故障”的风险，转成“即使出错也可降级、可观测、可回滚”。

### 11.1 可复用模板

| 子系统 | 故障类型 | 触发条件 | 检测信号 | 自动动作（降级） | 人工动作（应急） | 恢复判据 | 观测指标 |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `<Subsystem>` | `<Timeout / Unreachable / Invalid / Inconsistent / Slow / Flapping>` | `<具体条件>` | `<日志/错误码/指标>` | `<系统自动行为>` | `<Runbook 操作>` | `<恢复条件>` | `<Counter/Gauge/Latency>` |

最小使用规则：
- 每个关键接口至少覆盖 `超时`、`不可达`、`脏响应`、`状态不一致`、`慢消费者`、`重连抖动`。
- 每一行必须有 `自动动作` 和 `恢复判据`，否则不是可运营设计。
- `自动动作` 优先保证安全退化路径（例如 fallback L2），而不是维持最佳性能。

### 11.2 RB↔EL 示例（6 行）

| 子系统 | 故障类型 | 触发条件 | 检测信号 | 自动动作（降级） | 人工动作（应急） | 恢复判据 | 观测指标 |
| --- | --- | --- | --- | --- | --- | --- | --- |
| RB↔EL `getPayload` | 超时 | builder `get_payload` 超过 `request_timeout` | client timeout + `builder_api_failed=true` | 返回 L2 payload；health 置 `PartialContent` | 检查 builder 负载、网络 RTT、timeout 参数 | 连续 N 次 builder 成功返回 payload | builder timeout 次数、fallback 次数 |
| RB↔EL `FCU/getPayload` | 不可达 | builder endpoint down / DNS / TCP fail | 连接错误日志、RPC 调用失败 | 跳过 builder，仅走 L2；维持出块 | 修复 builder 进程/网络；校验 JWT 与地址配置 | builder 可达且健康检查恢复 `Healthy` | builder 可达率、health 状态迁移 |
| RB↔EL `new_payload` 验证 | 脏响应 | builder 返回 payload 但本地 EL 验证不通过 | `InvalidPayload`/验证失败日志 | 判定 builder payload 无效并回落 L2 | 检查 builder 与 L2 fork/version 配置、执行参数一致性 | builder payload 连续验证通过 | invalid payload 计数、builder 选中率 |
| RB↔EL 状态映射 | 状态不一致 | payload_id 映射缺失/不匹配，或 builder 无对应 payload | `has_builder_payload=false`、payload mismatch 日志 | 不走 builder payload，直接使用 L2 payload | 排查 FCU 流与 payload_id 映射逻辑、trace 上下文 | payload_id 映射稳定且命中率恢复 | payload_id mismatch 次数、builder_has_payload 命中率 |
| RB↔Proxy / Proxy↔RPC | 慢消费者 | 下游消费落后，广播窗口被覆盖 | `RecvError::Lagged`、lagged_connections 增长 | 断开 lagging 连接或跳过旧消息，保护主路径 | 扩容 proxy、调大 buffer、限速低优先级客户端 | lagged 比例回落到阈值内 | lagged_connections、active_connections、send_failures |
| Builder↔RB WS / Proxy↔Upstream | 重连抖动 | 连接频繁断开重连（短时间 flap） | reconnect backoff 频繁触发、连接成功率下降 | 启用指数退避与心跳超时剔除，避免重连风暴 | 排查链路稳定性/网关/LB；临时提高 backoff 上限 | 连接存活时间超过阈值并持续稳定 | reconnect 次数、连接存活时长、pong timeout 次数 |

### 11.3 设计原则（实践版）

- 可降级：先保证“还能安全出块”，再追求 builder 最优路径。
- 可观测：每个故障行都要有可告警指标，未知问题先变成可见问题。
- 可回滚：任何增强路径失败都能退回基线路径（L2/fallback），避免单点锁死。

---

## 12. Flashblock 观测系统分析（Builder + RB + Proxy）

本节回答“flashblock 的观测系统如何做”，并给出可落地的最小告警面。

### 12.1 观测分层（你要先建立这个心智模型）

1. `Builder`（生产层）
- 关注：是否按节奏产出 flashblock、是否受预算限制、构建耗时是否异常、payload 大小与交易数是否异常。
- 代码：`/home/po/now/base/crates/builder/core/src/metrics.rs`、`/home/po/now/base/crates/builder/core/src/flashblocks/payload.rs`。

2. `RB`（控制与选择层）
- 关注：builder 入站连接健康、消息处理正确性（payload_id/index）、fallback 发生频率、最终 payload 来源（builder vs l2）。
- 代码：`/home/po/now/rollup-boost/crates/rollup-boost/src/flashblocks/{inbound.rs,service.rs,metrics.rs}`、`/home/po/now/rollup-boost/crates/rollup-boost/src/server.rs`、`/home/po/now/rollup-boost/crates/rollup-boost/src/probe.rs`。

3. `Proxy`（扩散与接入层）
- 关注：连接数、lagged 断连、限流命中、上游连接稳定性、客户端 pong 超时。
- 代码：`/home/po/now/rollup-boost/crates/websocket-proxy/src/{metrics.rs,registry.rs,server.rs,subscriber.rs}`。

### 12.2 当前实现里“已经有”的关键信号

`Builder` 侧指标（`base_builder` scope）：
- 产出与节奏：`flashblock_count`、`missing_flashblocks_count`、`reduced_flashblocks_number`、`first_flashblock_time_offset`、`flashblocks_time_drift`。
- 时延：`flashblock_build_duration`、`total_block_built_duration`、`transaction_pool_fetch_duration`、`state_root_calculation_duration`。
- 质量：`invalid_built_blocks_count`、`flashblock_byte_size_histogram`、`flashblock_num_tx_histogram`。
- 预算/限额：`flashblock_execution_time_exceeded_total`、`block_state_root_time_exceeded_total`、`gas_limit_exceeded_total`、`da_footprint_exceeded_total`。

`RB` 侧 flashblocks 指标：
- 入站 WS：`flashblocks.ws_inbound.reconnect_attempts`、`connection_status`、`messages_received`。
- 服务层：`flashblocks.service.messages_processed`、`extend_payload_errors`、`current_payload_id_mismatch`、`flashblocks_missing_*`。
- 健康探针：`/healthz` 返回 `200/206/503`（Healthy/PartialContent/ServiceUnavailable）。

`RB` 侧 tracing：
- span labels 已限制为低基数：`code`、`payload_source`、`method`、`builder_has_payload`。
- 自动产出 span duration + `gas_delta`/`tx_count_delta` 直方图，用于比较 builder 与 l2 产物。

`Proxy` 侧指标（`websocket_proxy` scope）：
- 连接治理：`new_connections`、`closed_connections`、`active_connections`、`unauthorized_requests`、`rate_limited_requests`。
- 广播健康：`lagged_connections`、`failed_messages`、`bytes_broadcasted`。
- 上游稳定性：`upstream_connection_attempts/successes/failures`、`upstream_connections`、`upstream_errors`。
- 心跳：`client_pong_disconnects`。

### 12.3 Tracing（单独分析）

`tracing` 在这套系统里的角色不是“替代 metrics”，而是提供按 `payload_id` 聚合的请求级因果链。

核心实现点：
- `RB` span 标签白名单在 `rollup-boost/src/tracing.rs`：`code`、`payload_source`、`method`、`builder_has_payload`。
- `RB` 在 `server.rs` 的 `FCU/getPayload/newPayload` 路径中记录 span 字段，并在 `get_payload` 记录 `gas_delta`、`tx_count_delta`。
- `RB` flashblocks service 在 `service.rs` 记录 `payload_id`、`index`、mismatch/error 事件，帮助定位“状态不一致发生在何处”。
- `Builder` 在 `payload.rs` 记录关键阶段日志（build_flashblock、finalize、cancel 分支），用于确认生产时序和预算触发点。

`Span::current()` 与 `PayloadTraceContext` 的关系（最近讨论结论）：
- `tracing::Span::current()` 返回的是“当前执行上下文里的 span 句柄”，不是日志本身。
- `PayloadTraceContext` 保存的是 `payload_id -> {trace_id, builder_has_payload}` 以及 `parent_hash -> payload_id[]` 的索引，用于跨请求关联。
- 在 FCU 处理时，RB 把当前 span 的 `trace_id` 存进 `PayloadTraceContext`。
- 在后续 `getPayload/newPayload` 处理中，RB 从 `PayloadTraceContext` 取回 `trace_id`，通过 `Span::current().follows_from(cause)` 建立因果链。
- span 标签值（如 `builder_has_payload`、`payload_source`）是由 `Span::current().record(...)` 写入当前 span；`tracing.rs` 只负责读取这些 span 属性并做指标化，不直接读取 `PayloadTraceContext`。

tracing 数据流（端到端）：
1. 代码侧通过 `#[instrument(...)]` 和 `tracing::info!/error!` 产生命名 span 与 event。
2. `init_tracing()` 调用 `set_global_default(...)` 注册全局 subscriber，建立统一接收总线。
3. 所有 span/event 流入该 subscriber 后，按 layer 分发：
- `fmt` layer：输出文本或 JSON 日志。
- `OpenTelemetryLayer`：把 tracing span 转成 OTEL span。
4. OTEL provider 在 `init_tracing()` 中挂载 `MetricsSpanProcessor`。
5. span 结束时触发 `MetricsSpanProcessor::on_end`：
- 读取 span 属性（白名单标签 + status/span_kind）。
- 计算 span 时长并写入 `{span_name}_duration` 直方图。
- 额外写入 `block_building_gas_delta` / `block_building_tx_count_delta`。

补充说明：
- `set_global_default` 不是“只捕获 `#[instrument]`”，而是捕获整个进程里发往 tracing dispatcher 的 span/event。
- `filter_name`/Targets 只影响哪些 target 与级别被处理，不改变上述数据流结构。

为什么 tracing 需要独立看：
- metrics 只能回答“坏了多少”，tracing 才能回答“具体哪条链路、哪个 payload_id、哪一步坏”。
- 例如 `current_payload_id_mismatch` 告警触发后，要靠 tracing 回放同一 `payload_id` 的 FCU -> inbound -> extend -> getPayload 路径。

落地规则（避免侵入过重）：
- 核心系统只埋“语义锚点”字段：`payload_id`、`index`、`payload_source`、`builder_has_payload`。
- tracing 标签必须低基数，禁止把高基数数据（tx hash、地址列表）当 metric label。
- tracing 采样可高于 logs，但不应影响主路径；OTLP 导出异常不能阻塞出块。

时延统计是如何做的（原理）：
- `#[instrument(...)]` 在进入关键 RPC 方法时创建 span，span 生命周期覆盖整个处理过程（例如 `fork_choice_updated_v3`、`get_payload_v3/v4`）。
- 自定义 `MetricsSpanProcessor` 在 span 结束时拿到 `start_time/end_time`，计算 `duration`。
- processor 从 span attributes 中筛选低基数字段（白名单）作为 labels，附加 `span_kind`、`status`。
- 然后写入直方图：
  - 通用：`{span_name}_duration`
  - 专项：`block_building_gas_delta`、`block_building_tx_count_delta`
- 这样实现了“同一套 tracing 数据同时支持排障和时延指标聚合”，且通过白名单控制标签基数，避免 metrics cardinality 爆炸。

### 12.4 最小告警集（建议先从这 8 条开始）

1. `RB /healthz != 200` 持续超过阈值（区分 `206` 与 `503`）。
2. `flashblocks.ws_inbound.connection_status == 0` 持续超过阈值。
3. `flashblocks.ws_inbound.reconnect_attempts` 速率突增（重连抖动）。
4. `flashblocks.service.current_payload_id_mismatch > 0`（状态一致性风险）。
5. `flashblocks.service.extend_payload_errors` 增长（序列/数据损坏风险）。
6. `base_builder.missing_flashblocks_count` 持续偏高（产出节奏退化）。
7. `websocket_proxy.lagged_connections` 持续上升（下游扩散压力过高）。
8. `websocket_proxy.rate_limited_requests` 异常飙升（接入攻击或配置过紧）。
