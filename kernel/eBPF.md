## eBPF Defination
eBPF is a sandboxed, hook-triggered execution enviroment embedded in the Linux kernel that enables dynamically loaded programs to run in response to kernel or user-space events.
- sandbox: eBPF program 运行在内核提供的受限执行环境里，需通过 verifier 校验（有界循环、内存访问安全、调用受限 helper）后才能加载，避免破坏内核稳定性。
- hooks trigger: eBPF 不是常驻轮询，而是“事件驱动”执行；可挂载在内核路径（kprobe/tracepoint/XDP/TC/cgroup/LSM）或用户态相关路径（uprobe、USDT）上，事件发生时触发。
- embedded in kernel: 执行位置在 kernel context，能观测内核态关键路径并做快速处理；但权限不是“无限特权”，而是受程序类型、attach 点、helper 白名单和内核能力约束。
- dynamically loaded programs: 程序可在运行时装载/卸载，无需重编内核或重启(校验、执行与 JIT 能力都由内核内建提供)；字节码先经verifier，再由解释器执行或经 JIT 编译为本机指令以提升性能。
