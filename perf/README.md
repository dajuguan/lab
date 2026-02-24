### Hash bench
- [go scheduler](https://github.com/dajuguan/go/tree/master/codes/scheduler)

### Off-CPU demo (Rust + perf flamegraph)

参考思路：`sched_stat_sleep + sched_switch + sched_process_exit`，再用 `perf inject --sched-stat` 合并后出图。

1. 准备依赖
- `perf` 可用
- Ubuntu 22.04 安装 `perf`：
```bash
sudo apt update
sudo apt install -y linux-tools-common linux-tools-generic linux-tools-$(uname -r)
```
- WSL2（`*-microsoft-standard-WSL2` 内核）如果提示 `perf not found for kernel ...`，需要安装 WSL 专用包（通常来自 Microsoft 仓库）：
```bash
sudo apt update
sudo apt install -y linux-tools-$(uname -r) linux-cloud-tools-$(uname -r)
```
- 如果上面仍提示 `Unable to locate package`，按照[这个教程进行](https://www.arong-xu.com/en/posts/wsl2-install-perf-with-manual-compile/)。
- 允许 tracing（否则会出现 `perf_event_paranoid=2 is restrictive for tracing`）：
```bash
echo 1 | sudo tee /proc/sys/kernel/perf_event_paranoid
```
- 开启 schedstats（`perf inject --sched-stat` 依赖）：
```bash
echo 1 | sudo tee /proc/sys/kernel/sched_schedstats
```
- FlameGraph Perl 工具：`flamegraph.pl` 与 `stackcollapse.pl` 在 `PATH`，或通过环境变量指定：
  - `FLAMEGRAPH_PL=/path/to/flamegraph.pl`
  - `STACKCOLLAPSE_PL=/path/to/stackcollapse.pl`
- WSL2 上若出现 `perf script ... Segmentation fault (core dumped)`，通常是当前 `perf` 与内核/事件格式兼容问题；建议按手动编译教程使用与当前 WSL2 内核匹配版本的 `tools/perf`。

sudo apt install -y systemtap systemtap-runtime

2. 运行
```bash
cd perf
sudo -E ./run_offcpu_flamegraph.sh 5
```
- 若你手动编译了 `tools/perf`，建议显式指定，避免 `sudo` 下走到系统旧版本：
```bash
cd perf
sudo -E PERF_BIN=/path/to/tools/perf/perf ./run_offcpu_flamegraph.sh 5
```
- 默认会按 demo 进程名 `offcpu-demo` 过滤 `sched_stat_sleep` 事件；如你改了二进制名，可覆盖：
```bash
sudo -E DEMO_BIN_NAME=offcpu-demo DEMO_COMM=offcpu-demo ./run_offcpu_flamegraph.sh 5
```

3. 输出
- `out/offcpu.svg`：off-cpu flamegraph
- `out/perf.data.raw`：原始 perf 数据
- `out/perf.data`：inject 后数据
- `out/out.perf`：`perf script` 文本
- `out/offcpu.folded`：折叠栈


```bash
./sample-bt-off-cpu -t 10 -p 13491 -u > out.stap
/opt/FlameGraph/stackcollapse-stap.pl out.stap > out.folded
/opt/FlameGraph/flamegraph.pl --colors=io out.folded > offcpu.svg
```
