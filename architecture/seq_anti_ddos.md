# Sequencer Anti-DDoS 设计梳理

本文从 sequencer 的交易处理链路出发，整理其在不同攻击面上的防御机制，并分析剩余风险，尤其是仍然可能打到 sequencer 的 expensive-valid transaction。

## 1. 总览

如果把 sequencer 看成一条从“外部请求 -> 交易入池 -> 候选选择 -> 模拟执行 -> 出块”的链路，那么常见 DDoS / 资源消耗攻击大致会落在以下几层：

1. 入口连接层：大量连接或订阅占满前门。
2. RPC 提交层：大量无效 raw tx、重复 bundle、超慢 metering 请求。
3. TxPool 容量层：塞满 pending / queued / basefee / blob 子池，或单账户占满槽位。
4. 交易验证层：发送表面合法但不适配 OP/Base 规则的交易，企图消耗校验资源。
5. 构块执行层：发送真正能入池、也能过基本验证，但执行成本高的 expensive-valid tx，拖慢 flashblock / block 构建。
6. P2P / Discovery 层：如果 sequencer 暴露了底层网络接口，还会受到握手洪泛、discv4 包洪泛等压力。

Builder / sequencer 的防御策略不是单点，而是“前门限流 + pool 限额 + validator 过滤 + builder 预算控制”这几层叠加。

## 2. 攻击面 -> 代码防线 -> 剩余风险

| 攻击面 | 典型攻击方式 | 代码防线 | 剩余风险 |
| --- | --- | --- | --- |
| 入口连接层 | 大量 WebSocket 连接、订阅连接不释放 | `websocket-proxy` 对连接做全局限额和单 IP 限额，超限直接返回 `429`，不会进入后续逻辑。见 `InMemoryRateLimit` 与 `websocket_handler`。 | 如果攻击来自大量真实 IP，单机全局连接配额仍可能被占满；此层只能限制连接数，不能判断业务价值。 |
| RPC 提交层 | 空数据、非法编码、重复 bundle、超慢 bundle metering | `ingress-rpc` 先做轻量解码，空 tx / 非法 2718 直接拒绝；对 bundle 做 TTL 去重；对 metering RPC 加 timeout；对“预测执行时间 > block time”的 bundle 直接拒绝。 | 攻击者仍可发送“解码合法、metering 不超时、但业务上低价值”的请求，继续向后消耗 txpool / builder 资源。 |
| TxPool 容量层 | 塞满 pending/basefee/queued/blob 子池；单账户 nonce 链占坑；replacement spam | 底层复用 reth txpool：每个 subpool 都有 `max_txs` 和 `max_size`；默认每池 10000 tx / 20MB；每 sender 默认最多 16 个 executable slots；replacement 需要 price bump，普通 tx 10%，blob tx 100%。 | 多账户协同 spam 仍可在总池容量内抢占空间；配额控制的是“占用”，不是“交易价值”。 |
| 交易验证层 | 无效 tx 洪泛；发 OP/Base 不支持的 tx；余额不够但试图占用校验资源 | `OpPoolBuilder` 在 validator 上配置 `no_eip4844`、`max_tx_input_bytes`、`rpc_tx_fee_cap`、`max_tx_gas_limit`、`minimum_priority_fee`、`additional_validation_tasks`；`OpTransactionValidator` 额外检查 blob tx 不允许、余额必须覆盖 L2 cost + L1 data fee。 | 对“合法但很贵”的 tx，这一层不会拦，因为它们本来就是 valid；额外 validation tasks 只是提升吞吐，不是拒绝机制。 |
| 构块候选选择层 | 利用 flashblocks 多轮构建，使同一批候选被反复扫描 / 反复执行 | `BestFlashblocksTxs` 维护 committed tx 集合；每轮 flashblock 刷新优先级边界，但会跳过已提交 tx，避免重复纳入导致 `NonceTooLow` 和重复执行。 | 仍然要在每轮重新从 pool 拉 best txs，恶意流量足够大时，扫描和筛选本身仍有成本。 |
| 构块执行层 | 发送 expensive-valid tx：能入池、能通过 validator，但执行很慢、状态访问重、state root 代价高、占 DA / uncompressed 空间 | `execute_best_transactions` 对每笔 tx 在执行前检查 gas / DA / DA footprint / uncompressed size / predicted execution time / predicted state root time；超限则 `mark_invalid(sender, nonce)`，剪掉该 nonce 及其后继。builder 还可通过 `max_gas_per_txn`、`max_execution_time_per_tx_us`、`max_state_root_time_per_tx_us`、`flashblock_execution_time_budget_us`、`block_state_root_time_budget_us`、`max_uncompressed_block_size` 做预算控制。 | 这是最难彻底防住的一层。若某些 tx 在 metering 数据缺失或低估时仍然被视为合法候选，就仍可能触发真实执行开销。预算控制能止损，但不能让前几笔 expensive-valid tx 完全“零成本”。 |
| P2P / Discovery 层 | discv4 UDP 包洪泛；超量 inbound 连接 | reth discv4 对单 IP 包速率做 primitive rate limit；network swarm 对超容量 inbound 连接直接拒绝。 | 仅在 sequencer 对外暴露这些网络面时适用；而且仍然无法完全防御大规模分布式来源。 |

## 3. 各层代码锚点

### 3.1 入口连接层

- `InMemoryRateLimit::new(instance_limit, per_ip_limit)` 使用全局 `Semaphore` + `active_connections` 做双限流。
- `websocket_handler` 在 upgrade 前调用 `try_acquire`，若失败直接返回 `429 Too Many Requests`。

代码：
- `crates/infra/websocket-proxy/src/rate_limit.rs`
- `crates/infra/websocket-proxy/src/server.rs`

### 3.2 RPC 提交层

`ingress-rpc` 的核心防御点：

- `get_tx` 对空字节和非法 `2718` 编码做快速拒绝。
- `bundle_cache` 做 TTL 去重，避免同 bundle 重复计量 / 重复入队。
- `meter_bundle` 外层有 `timeout(...)`，防止 metering RPC 卡死。
- 如果 metering 结果中的 `total_execution_time_us` 已超过 block time，则直接拒绝，不送 builder。

代码：
- `crates/infra/ingress-rpc/src/service.rs`

### 3.3 TxPool 容量层

底层 txpool 是 reth 的标准池，不是 builder 自己实现的新池。关键限制包括：

- `pending_limit / basefee_limit / queued_limit / blob_limit`
- `max_account_slots`
- `price_bumps`
- `minimum_priority_fee`
- `gas_limit`
- `max_queued_lifetime`

默认值：

- 每个 subpool 默认 `10000` 笔或 `20MB`
- 每 sender 默认 `16` 个 executable slots
- replacement 默认 `10%` price bump，blob replacement `100%`

代码：
- `reth/crates/transaction-pool/src/config.rs`
- `reth/crates/node/core/src/args/txpool.rs`

### 3.4 交易验证层

`OpPoolBuilder` 在构建 pool validator 时挂上了几类基础防线：

- `no_eip4844()`
- `with_max_tx_input_bytes(...)`
- `set_tx_fee_cap(...)`
- `with_max_tx_gas_limit(...)`
- `with_minimum_priority_fee(...)`
- `with_additional_tasks(...)`

然后再包一层 `OpTransactionValidator`，补充 OP/Base 语义：

- blob tx 直接视为 invalid
- sender 余额必须覆盖常规 tx cost 加上 L1 data fee

代码：
- `crates/execution/node/src/node.rs`
- `crates/execution/txpool/src/validator.rs`

### 3.5 构块候选选择层

flashblocks 模式下，builder 不是一次性消费 pool，而是每个 flashblock 都：

1. 用当前 `base_fee/blob_gasprice` 重新取 `best_transactions_with_attributes(...)`
2. 刷新 iterator
3. 执行当前预算内能容纳的交易
4. 将已落到状态里的 tx hash 标记为 committed
5. 下一轮跳过 committed tx

这层的关键价值是：避免已经在前一轮 flashblock 提交的交易被下一轮再次拿出来模拟或执行。

代码：
- `crates/builder/core/src/flashblocks/payload.rs`
- `crates/builder/core/src/flashblocks/best_txs.rs`

### 3.6 构块执行层

真正与 expensive-valid tx 正面对抗的是 `execute_best_transactions`。

它在 tx 执行前先构建 `ResourceLimits`，逐笔交易检查：

- `tx_data_limit`
- `block_data_limit`
- `block_gas_limit`
- `block_da_footprint_limit`
- `tx_execution_time_limit_us`
- `flashblock_execution_time_limit_us`
- `tx_state_root_time_limit_us`
- `block_state_root_time_limit_us`
- `block_uncompressed_size_limit`

如果超限：

- 对静态限制一律拒绝
- 对 metering 限制，`dry-run` 只记录，`enforce` 直接拒绝
- 调用 `best_txs.mark_invalid(tx.signer(), tx.nonce())`，将这个 nonce 及其后继从当前候选流里剪掉

同时，来自 mempool 的 blob tx / deposit tx 也会在这里被跳过，不允许进入 sequencer 普通出块路径。

代码：
- `crates/builder/core/src/flashblocks/context.rs`

### 3.7 P2P / Discovery 层

如果 sequencer 不是纯 RPC 前门，而是同时暴露了 p2p/discv4，那么还有底层网络防护：

- discv4：单 IP 每分钟最多 60 个包，超过直接丢弃。
- inbound TCP session：超容量直接拒绝接入。

代码：
- `reth/crates/net/discv4/src/lib.rs`
- `reth/crates/net/network/src/swarm.rs`

## 4. 还能被什么样的 expensive-valid tx 打到

这是 sequencer 最关键也最难完全防住的一类攻击。

所谓 expensive-valid tx，指的是：

- 它不是非法交易。
- 它能通过 txpool validator。
- 它能进入候选集合。
- 但它在真实执行、状态访问、回执生成、state root 预测或 block packing 上非常贵。

### 4.1 典型类型

#### 4.1.1 真实执行耗时高，但静态字段不夸张的 tx

例如：

- gas limit 并不超阈值
- calldata 长度也不异常
- 但合约内部路径复杂，触发大量 opcode 执行、深层 storage 访问或复杂控制流

这种交易在 validator 层一般不会被拦，因为 validator 更多做的是格式、余额、fee、协议支持性检查，不会完整执行交易。

Builder 只能依赖 metering 提供的 `predicted_execution_time_us`。如果 metering 缺失、过期或低估，这类 tx 仍可能进入真实执行路径，直到在 EVM 执行阶段暴露成本。

#### 4.1.2 状态根相关代价高的 tx

有些交易本身执行不一定最慢，但会带来较重的状态修改集合，导致后续 state root / trie 相关成本偏高。

代码里已经有：

- `max_state_root_time_per_tx_us`
- `block_state_root_time_budget_us`

但它们依赖预测值。如果预测值偏低，真实构块过程仍可能承受较高的 trie/state 相关压力。

#### 4.1.3 多账户协同的“合法低价值 spam”

`max_account_slots` 能限制单账户，但拦不住多账户协同。

也就是说，攻击者可以用大量账户各发少量合法 tx：

- 每个账户都不超 16 slots
- 每笔 tx 都合法
- 每笔 priority fee 甚至不低
- 但总体上抢占候选扫描、执行预算和 block 空间

这类攻击不会被单账户限额解决，本质上只能依赖总 pool 容量、排序优先级、builder 预算和更外层的经济门槛共同约束。

#### 4.1.4 calldata / DA / uncompressed size 接近阈值的 tx

有些交易执行本身不慢，但非常占：

- DA budget
- block uncompressed size
- gas footprint

这类交易会导致 builder 很快触及块级预算，使同一 flashblock 能容纳的其他交易减少，从而形成“低吞吐占坑”效果。

代码已经用：

- `tx_data_limit`
- `block_data_limit`
- `block_uncompressed_size_limit`
- `block_da_footprint_limit`

来约束，但如果攻击者专门贴着阈值发高优先费交易，它们仍可能合法抢占部分 block 空间。

#### 4.1.5 revert 但仍然昂贵的 tx

revert 不代表便宜。

某些 tx 即使最终 revert，仍然可能：

- 消耗相当多 gas
- 访问较多状态
- 占用模拟时间

代码中会记录 reverted gas，但只要它没有违反前置预算、且经济上有竞争力，它依然可能被纳入考虑。也就是说，“最终 revert” 并不天然等于“对 sequencer 无害”。

### 4.2 为什么这类 tx 不能完全在前面拦住

原因是前几层解决的是“明显坏的交易”：

- 连接限流解决的是流量洪泛
- RPC 快速拒绝解决的是非法字节和重复请求
- txpool 限额解决的是池子占用
- validator 解决的是协议不合法、不支持、余额不够

而 expensive-valid tx 的难点在于：它们本来就合法，而且很多成本只有在更接近真实执行时才真正暴露。

换句话说，sequencer 面对这类攻击时，本质上是在做一件“成本预测”问题，而不是“静态合法性判断”问题。

### 4.3 代码当前的止损手段

当前实现已经在尽量把伤害从“拖垮整个 builder”降到“局部预算内止损”：

1. 用 metering 给 tx 打 execution/state-root 预测标签。
2. 用 tx 级和 flashblock/block 级预算，在执行前筛掉明显超限项。
3. 在 flashblocks 中按批次推进，而不是整块一次性跑完，降低单轮爆炸半径。
4. 一旦确认某 sender 某 nonce 无法接受，直接 `mark_invalid(sender, nonce)`，避免同一条 nonce 链持续浪费当前轮的筛选/执行资源。
5. 对已经提交过的 tx 做 committed 去重，避免 flashblock 之间重复执行。

### 4.4 仍然存在的剩余风险

剩余风险主要集中在“预测误差”和“分布式合法 spam”两类：

#### 4.4.1 预测误差

如果 metering 数据：

- 没有覆盖到某笔 tx
- 和当前 pending state 已不一致
- 系统性低估真实执行时间 / state root 时间

那么 builder 仍可能把 expensive-valid tx 放进真实执行路径。

#### 4.4.2 多账户分布式合法 spam

如果攻击者用大量地址发送：

- 都能通过 validator
- 单账户不超槽位限制
- 单笔都不超预算
- 但整体大量挤占候选扫描与打包预算

那么现有设计更多是“限制伤害上界”，而不是“完全阻断进入”。

#### 4.4.3 高优先费占坑

排序本身是偏收益导向的。若攻击者愿意付更高优先费，那么即使交易本身并不高价值，也可能在排序上压过正常用户交易，从而形成经济型 DoS。

这不是代码 bug，而是排序目标与抗滥用目标之间的天然张力。

## 5. 小结

可以把 sequencer 的抗 DDoS 设计总结成下面这句话：

> 前门先限流，入池先限额，校验先过滤，构块再按预算止损。

这套设计对“明显无效流量”和“直接塞满内存”的攻击已经比较完整；真正难完全防住的，是那些能够通过前面各层、并在经济上看起来也“说得过去”的 expensive-valid tx。

因此，当前实现的重点不是让这类交易完全零成本，而是：

- 尽量在预测阶段提前识别
- 尽量把损害限制在 tx / flashblock / block 预算内
- 尽量避免同一批恶意交易在多个 flashblock 中重复消耗 builder 资源

## 6. 参考代码

- `/home/po/now/base/crates/infra/websocket-proxy/src/rate_limit.rs`
- `/home/po/now/base/crates/infra/websocket-proxy/src/server.rs`
- `/home/po/now/base/crates/infra/ingress-rpc/src/service.rs`
- `/home/po/now/base/crates/execution/node/src/node.rs`
- `/home/po/now/base/crates/execution/txpool/src/validator.rs`
- `/home/po/now/base/crates/execution/txpool/src/transaction.rs`
- `/home/po/now/base/crates/builder/core/src/flashblocks/payload.rs`
- `/home/po/now/base/crates/builder/core/src/flashblocks/context.rs`
- `/home/po/now/base/crates/builder/core/src/flashblocks/best_txs.rs`
- `/home/po/.cargo/git/checkouts/reth-e231042ee7db3fb7/bef3d7b/crates/transaction-pool/src/config.rs`
- `/home/po/.cargo/git/checkouts/reth-e231042ee7db3fb7/bef3d7b/crates/node/core/src/args/txpool.rs`
- `/home/po/.cargo/git/checkouts/reth-e231042ee7db3fb7/bef3d7b/crates/net/discv4/src/lib.rs`
- `/home/po/.cargo/git/checkouts/reth-e231042ee7db3fb7/bef3d7b/crates/net/network/src/swarm.rs`

## 7. 为什么应用层限流还不够

上面的限流、校验和预算控制，大多发生在应用层或 builder 层。它们能防住“已经到达服务逻辑”的恶意请求，但防不住更前面的网络层洪泛。

如果 sequencer 节点先被大量流量打满：

- 网卡带宽被占满
- SYN backlog / conntrack / socket buffer 被打爆
- TLS / WebSocket upgrade 前的 CPU 已被耗尽

那么请求还没进入 `websocket-proxy`、`ingress-rpc` 或 txpool，应用层限流就来不及生效，节点仍然会先挂。

因此完整防线应当是：

`Network/L4 anti-DDoS -> Proxy/L7 cheap reject -> Ingress/TxPool/Builder limits`

最关键的补强点只有三条：

1. 不要让 builder / sequencer 直接暴露公网，前面要有 DDoS 清洗、L4/L7 代理或 LB。
2. 在更前面做连接和包级限速，比如 SYN rate limit、单 IP 并发限制、PPS/连接数限制。
3. 将 `builder`、真实 txpool、flashblock service 放内网，只暴露轻量前门服务。

换句话说，应用层防线负责“拒绝坏请求”，网络层防线负责“让请求有机会进入应用层再被拒绝”。

在很多实际部署里，这两层之间还会再插入一层公网入口 relay / gateway，用来承接统一的 RPC 入口治理与流量整形；下面单独展开这一层。

## 8. 公网入口前的 Relay / Gateway 分层

很多系统里，sequencer 并不会直接把核心排序或构块进程暴露在公网，而是会在前面再放一层公网入口服务。这个入口层有时叫 `relay`，有时叫 `gateway`、`rpc frontend`、`ingress proxy` 或 `mempool proxy`，名字不完全统一，但职责很相似。

常见拓扑如下：

```text
Users / Wallets / Searchers
    |
    v
Public RPC / Relay / Gateway
    |
    v
Admission Control / Rate Limit / Dedup / Basic Validation
    |
    v
Internal Sequencer / TxPool / Builder
    |
    v
Execution / Block Construction
```

### 8.1 这层通常中继什么

不只是交易提交。常见会经过这层的请求包括：

- `sendRawTransaction`
- bundle / private orderflow 提交
- 高频 `eth_call` / simulation 类请求
- `estimateGas`
- nonce / receipt / block 查询
- 一些面向 searcher / builder 的专用 RPC

因此它的语义通常不是“只转发交易”，而是“对公网 RPC 请求做统一入口治理”。

### 8.2 为什么要放在 sequencer 前面

主要原因有五类：

1. 抗 DDoS
- 不让 sequencer 主进程直接暴露在公网。
- 连接管理、TLS、反向代理、全局限流、单 IP 限流、黑白名单等都先在入口层处理。
- 让最便宜的防御逻辑先消化最便宜的攻击流量。

2. 隔离昂贵逻辑
- sequencer 的真正昂贵部分是 txpool 管理、候选排序、模拟执行、构块与 state root。
- relay / gateway 先做轻量 admission control，把明显垃圾流量挡在前面。
- 即使前层被打满，也不等于 builder 一定被同步拖垮。

3. 隐藏内网拓扑
- 外部只看到统一入口，不直接看到内部 sequencer、备用节点或 builder 拓扑。
- 这样可以减少对 leader / active sequencer 的定向打击。

4. 做流量分流
- 公共钱包流量、searcher 流量、合作方专线、私有 orderflow 可以在入口层拆分到不同后端。
- 不同来源可以配不同配额、不同 QoS、不同鉴权策略。

5. 做协议兼容和请求整形
- 入口层通常负责 JSON-RPC 兼容、Header 鉴权、基础参数检查、去重、缓存、重试等事务性工作。
- 核心 sequencer 可以更聚焦于“合法候选交易如何排序与打包”。

### 8.3 这层一般会做哪些事情

这层往往不是 dumb relay，而是会做轻量但高性价比的治理：

- 连接数限制
- 单 IP / 单 API key 限流
- TLS 终止与反向代理
- payload 大小限制
- 基础格式校验
- 重复 bundle / 重复 raw tx 去重
- 简单缓存与快速失败
- 用户类型分流
- admission queue / backpressure

因此更准确地说，这一层常常是公网 ingress control plane，而不是单纯的字节转发器。

### 8.4 它能防什么，不能防什么

它很适合防：

- 连接洪泛
- 明显非法的 RPC 请求
- 重复提交 spam
- 大量低成本查询请求
- 对 sequencer 内网地址的直接探测和打击

但它很难彻底防：

- 经过格式校验后仍然“业务合法”的请求
- 愿意付费、愿意过基础过滤的合法 spam
- 真正 expensive-valid 的交易

原因是 relay / gateway 通常不会也不应该完整执行交易。它只能做轻量治理，不能替代 builder 的执行预算控制。也就是说：

- relay/gateway 负责把“明显坏的”和“明显便宜可拦的”挡在外面
- builder/sequencer 负责处理“看起来合法，但执行上可能很贵”的剩余问题

### 8.5 和本文后续各层防线的关系

如果系统前面存在 relay / gateway，那么本文前面的“入口连接层”和“RPC 提交层”很多防线其实会优先落在这层，而不一定直接落在 sequencer 进程里。

这样可以把整体防线理解成两段：

1. 外层公网入口
- 连接治理
- 限流
- 基础校验
- 去重
- 分流

2. 内层 sequencer / builder
- txpool 容量控制
- validator 过滤
- 候选选择
- 执行预算控制
- flashblock / block 构建止损

这两层叠加后，系统才能同时应对：

- 廉价网络洪泛
- 廉价 RPC spam
- 合法但低价值的 pool 占坑
- 合法但昂贵的 expensive-valid tx
