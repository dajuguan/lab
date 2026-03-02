## Off-CPU Analysis 核心原理

Off-CPU 分析关注的是“线程没有在 CPU 上运行”的时间，也就是 `wall-clock time - on-CPU time` 的主要来源。  
典型问题不是“算得慢”，而是“在等”：等锁、等 I/O、等调度、等外部事件。

### 1. 两种做法：Sampling vs Tracing

#### 1.1 基于 Sampling（采样）

1. 机制：按固定频率（或事件触发）抓取线程栈和状态，统计“看到的”等待栈。  
2. 优点：
- 开销可控，适合线上长期跑；
- 实现简单，不需要完整维护每个线程的起止状态。
3. 缺点：
- 结果是统计近似，不是精确区间；
- 可能漏掉短暂但频繁的 off-cpu 等待（采样盲区）。

#### 1.2 基于 Tracing（事件追踪）

1. 机制：挂 `sched_switch` 等 tracepoint，逐事件记录 `prev`/`next`，精确闭合每段 off-cpu 区间。  
2. 优点：
- 单段时长精确，可做栈级归因；
- 能还原“谁在什么时候因为什么阻塞”。
3. 缺点：
- 事件量大时开销显著，工程复杂度高（状态维护、丢事件处理、生命周期清理）。

#### 1.3 为什么 Tracing 性能容易不行

核心原因是：**调度事件频率极高 + 每次事件处理成本不低**。在高并发负载下，`sched_switch` 可达到很高吞吐，tracing 要为几乎每次切换付费：

1. 高频事件放大：
- 每次上下文切换都触发处理，CPU 忙时切换更频繁，形成正反馈开销。

2. 栈采集成本高：
- 采用户栈/内核栈需要 unwind 与符号解析（离线也有成本），比纯计数重很多。

3. map 读写与竞争：
- 需要频繁更新 `start/start_stack/counts`，多核下会出现 map 热点与同步成本。

4. 数据搬运与落盘压力：
- 向用户态输出事件或中间结果会带来 ring buffer/perf buffer 压力，可能丢包或反压。

5. 缓存与调度扰动：
- 分析器本身会污染 cache、增加调度负担，导致“观测影响被观测对象”。

所以实践里常见策略是：
- 线上默认用 sampling 做常态巡检；
- tracing 用于短时、定向、可控范围内的深挖定位。

### 2. 观测对象与核心思路

1. 观测对象：线程从 `TASK_RUNNING` 切到不可运行状态（如 `S`/`D`）到再次被调度运行之间的时间段。  
2. 核心思路：利用内核 `sched_switch` 事件，把一次 off-cpu 区间拆成：
- 起点：线程被切出（`prev` 下 CPU）。
- 终点：线程被切入（`next` 上 CPU）。
3. 区间归因：在区间起点采样调用栈（用户栈/内核栈），在终点做时长结算并聚合到“栈维度”的统计结果。

### 3. 数据结构（最小闭环）

1. `start[pid] = ts`：记录线程开始 off-cpu 的时间戳。  
2. `start_stack[pid] = stack_id`：记录该线程下线时的栈签名（可含 user/kernel stack）。  
3. `counts[stack_id] += delta`：按栈聚合 off-cpu 总时长（或次数、最大值等）。

这个模型的关键是：**按线程暂存起点，按再次运行时结算终点**，形成完整区间。

### 4. `handle_sched_switch` 的工作机制

`handle_sched_switch` 通常挂在 `sched:sched_switch` tracepoint（或等价 kprobe）上，每次上下文切换都会执行一次。  
它在一次事件中同时处理两个角色：

1. 处理 `prev`（即将下 CPU 的线程）：决定是否“开始记一段 off-cpu”。  
2. 处理 `next`（即将上 CPU 的线程）：决定是否“结束一段 off-cpu并结算”。

可抽象为：

```text
now = ktime_get_ns()

# A. prev 下线：记录起点
if prev 满足采样条件 且 prev_state 表示非可运行阻塞:
    start[prev_pid] = now
    start_stack[prev_pid] = collect_stack(prev)
else:
    删除 prev 旧记录(避免脏数据)

# B. next 上线：结算终点
if start[next_pid] 存在:
    delta = now - start[next_pid]
    if delta 在阈值范围内:
        counts[start_stack[next_pid]] += delta
    删除 start/start_stack[next_pid]
```

### 5. 关键判定点（为什么这么做）

1. 只对“阻塞切出”记账：  
`prev_state` 是核心过滤条件。线程若只是时间片用完仍可运行（runnable），不应算作 off-cpu 阻塞等待。

2. 归因用“切出时栈”而非“切入时栈”：  
切出时的调用路径更接近阻塞原因（如 `futex_wait`、`io_schedule`、`ep_poll`），因此更适合定位瓶颈来源。

3. 在 `next` 处结算：  
只有线程真正重新获得 CPU，off-cpu 区间才闭合，时长计算才准确。

4. 生命周期清理必须严格：  
线程退出、pid 复用、丢事件都会造成脏记录；结算后立即删除 map 项可降低误计风险。

### 6. 结果如何解读

最终可得到按函数栈聚合的 off-cpu 热点，例如：
- 锁竞争热点（`futex` 路径）；
- I/O 等待热点（块设备/文件系统等待路径）；
- 调度与同步热点（条件变量、poll/epoll 等待）。

因此 Off-CPU 与 On-CPU 需要联合看：  
- On-CPU 告诉你“CPU 时间花在哪”；  
- Off-CPU 告诉你“挂钟时间卡在哪”。

## References
- [结合 On-CPU 和 Off-CPU 分析的挂钟时间分析](https://eunomia.dev/zh/tutorials/32-wallclock-profiler/)
- [Brendan Gregg 的 "Off-CPU Analysis](http://www.brendangregg.com/offcpuanalysis.html)
- [Tidb flamegraph explaination](https://pingkai.cn/tidbcommunity/blog/6435ce44)