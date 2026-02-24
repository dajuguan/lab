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
- 如果上面仍提示 `Unable to locate package`，按照[这个教程手抖安装perf](https://www.arong-xu.com/en/posts/wsl2-install-perf-with-manual-compile/)。
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

- 安装systemtap以替代perf script
```
sudo apt update
sudo apt install -y systemtap systemtap-runtime
```

2. 运行
```bash
cd perf
sudo -E ./run_offcpu_flamegraph.sh 10
# 上面的代码在 Ubuntu 或者 CentOS 上面通常都会失败，主要是现在最新的系统为了性能考虑，并没有支持 sched statistics。 对于 Ubuntu，貌似只能重新编译内核，而对于 CentOS，只需要安装 kernel debuginfo，然后在打开 sched statistics 就可以了。
# 所以如果ubuntu的话最好用如下命令:
sudo -E ./run_offcpu_flamegraph.sh 10
```
- 若你手动编译了 `tools/perf`，建议显式指定，避免 `sudo` 下走到系统旧版本：
```bash
cd perf
sudo -E PERF_BIN=/path/to/tools/perf/perf ./run_offcpu_flamegraph.sh 10
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


### WSL2 补全 SystemTap
下面示例统一使用 `D:\WSL`（WSL 内路径 `/mnt/d/WSL`）。如果你放在其他盘，替换前缀即可。

1. 编译 WSL2 内核并生成 `modules.vhdx`
```bash
sudo apt update
sudo apt install -y git build-essential flex bison dwarves libssl-dev libelf-dev cpio qemu-utils bc libncurses-dev

mkdir -p ~/wsl && cd ~/wsl
git clone --depth=1 --branch linux-msft-wsl-6.6.87.2 https://github.com/microsoft/WSL2-Linux-Kernel.git
cd WSL2-Linux-Kernel
cp Microsoft/config-wsl .config
make olddefconfig
make -j"$(nproc)" KCONFIG_CONFIG=Microsoft/config-wsl
make INSTALL_MOD_PATH="$PWD/modules" modules_install
sudo ./Microsoft/scripts/gen_modules_vhdx.sh "$PWD/modules" "$(make -s kernelrelease)" /mnt/d/WSL/modules.vhdx
cp arch/x86/boot/bzImage /mnt/d/WSL/bzImage
```

2. Windows 侧配置 `%UserProfile%\\.wslconfig`
```ini
[wsl2]
kernel=D:\\WSL\\bzImage
kernelModules=D:\\WSL\\modules.vhdx
```
然后执行：
```powershell
wsl --shutdown
```

3. 回到 WSL 补 `build/.config`（SystemTap 关键）
```bash
cd ~/wsl/WSL2-Linux-Kernel
sudo mkdir -p /lib/modules/$(uname -r)
sudo ln -sfn "$PWD" /lib/modules/$(uname -r)/build
sudo ln -sfn "$PWD" /lib/modules/$(uname -r)/source
test -f /lib/modules/$(uname -r)/build/.config && echo "build/.config OK"
```

4. 安装和验证 SystemTap
```bash
sudo apt update
sudo apt install -y systemtap systemtap-runtime
stap -V # shoule be 5.x
### for ubuntu 22.04 因为上述默认只会安装4.6不兼容，需要手动安装；24.04不用管如下的
sudo apt install -y libboost-all-dev
sudo apt remove systemtap systemtap-runtime
git clone https://sourceware.org/git/systemtap.git
cd systemtap
./configure --prefix=/usr/local
make -j"$(nproc)"
sudo make install
```

验证:
```bash
sudo stap -v -e 'probe kernel.function("vfs_read") { printf("read performed\n"); exit() }'
```