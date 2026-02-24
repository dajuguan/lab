#!/usr/bin/env bash
set -euo pipefail

echo 1 | sudo tee /proc/sys/kernel/sched_schedstats >/dev/null
echo 1 | sudo tee /proc/sys/kernel/perf_event_paranoid >/dev/null

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
OUT_DIR="${ROOT_DIR}/out"
SECONDS_TO_RECORD="${1:-10}"

DEMO_BIN_NAME="${DEMO_BIN_NAME:-offcpu-demo}"
DEMO_COMM="${DEMO_COMM:-${DEMO_BIN_NAME}}"
SAMPLE_BT_OFFCPU="${SAMPLE_BT_OFFCPU:-$(command -v sample-bt-off-cpu || true)}"
STACKCOLLAPSE_STAP_PL="${STACKCOLLAPSE_STAP_PL:-/opt/FlameGraph/stackcollapse-stap.pl}"
FLAMEGRAPH_PL="${FLAMEGRAPH_PL:-/opt/FlameGraph/flamegraph.pl}"
OFFCPU_INCLUDE_KERNEL="${OFFCPU_INCLUDE_KERNEL:-1}"

OUT_STAP="${OUT_DIR}/out.stap.bt.txt"
OUT_FOLDED="${OUT_DIR}/offcpu.stap.folded"
OUT_SVG="${OUT_DIR}/offcpu.stap.svg"
DEMO_LOG="${OUT_DIR}/demo.stap.log"
DEMO_BLOCK_IO_FILE="${OUT_DIR}/offcpu-demo.stap.bin"
DEMO_SYSCALL_LOG="${OUT_DIR}/offcpu-syscall.stap.log"

TRACE_SUMMARY="${OUT_DIR}/trace-summary.stap.txt"
TRACE_SUMMARY_STAP_RAW="${OUT_DIR}/trace-summary.stap.raw.txt"
TRACE_SUMMARY_STAP_SCRIPT="${OUT_DIR}/trace-summary.stap.stp"

mkdir -p "${OUT_DIR}"

KERNEL_RELEASE="$(uname -r)"
if [[ ! -f "/lib/modules/${KERNEL_RELEASE}/build/.config" ]]; then
  echo "SystemTap kernel build config missing: /lib/modules/${KERNEL_RELEASE}/build/.config"
  echo "Current kernel cannot run sample-bt-off-cpu directly (common on WSL2 custom kernels)."
  echo "Use the perf-based script instead: sudo -E ./run_offcpu_perf_flamegraph.sh ${SECONDS_TO_RECORD}"
  exit 1
fi

if [[ -z "${SAMPLE_BT_OFFCPU}" || ! -x "${SAMPLE_BT_OFFCPU}" ]]; then
  echo "sample-bt-off-cpu not found or not executable: ${SAMPLE_BT_OFFCPU}"
  echo "Set SAMPLE_BT_OFFCPU=/path/to/sample-bt-off-cpu"
  exit 1
fi
if [[ ! -x "${STACKCOLLAPSE_STAP_PL}" ]]; then
  echo "stackcollapse-stap.pl not found or not executable: ${STACKCOLLAPSE_STAP_PL}"
  exit 1
fi
if [[ ! -x "${FLAMEGRAPH_PL}" ]]; then
  echo "flamegraph.pl not found or not executable: ${FLAMEGRAPH_PL}"
  exit 1
fi
if ! command -v stap >/dev/null 2>&1; then
  echo "stap not found in PATH."
  exit 1
fi

echo "[1/4] building demo ..."
cargo build --manifest-path "${ROOT_DIR}/Cargo.toml" --release

echo "[2/4] launching demo ..."
OFFCPU_DEMO_FILE="${DEMO_BLOCK_IO_FILE}" \
OFFCPU_BLOCK_IO_FILE="${DEMO_BLOCK_IO_FILE}" \
OFFCPU_SYSCALL_LOG="${DEMO_SYSCALL_LOG}" \
  "${ROOT_DIR}/target/release/${DEMO_BIN_NAME}" > "${DEMO_LOG}" 2>&1 &
DEMO_PID=$!
trap 'kill ${DEMO_PID} >/dev/null 2>&1 || true' EXIT
sleep 1
if ! kill -0 "${DEMO_PID}" >/dev/null 2>&1; then
  echo "demo exited before profiling; see ${DEMO_LOG}"
  tail -n 80 "${DEMO_LOG}" || true
  exit 1
fi

echo "[3/4] recording off-cpu with sample-bt-off-cpu for ${SECONDS_TO_RECORD}s (pid=${DEMO_PID}) ..."
STAP_ARGS=(-t "${SECONDS_TO_RECORD}" -p "${DEMO_PID}" -u)
if [[ "${OFFCPU_INCLUDE_KERNEL}" == "1" ]]; then
  STAP_ARGS+=(-k)
fi
"${SAMPLE_BT_OFFCPU}" "${STAP_ARGS[@]}" > "${OUT_STAP}"
if [[ ! -s "${OUT_STAP}" ]]; then
  echo "sample-bt-off-cpu produced no output."
  exit 1
fi

echo "[4/4] rendering flamegraph and generating stap summary ..."
"${STACKCOLLAPSE_STAP_PL}" "${OUT_STAP}" > "${OUT_FOLDED}"
"${FLAMEGRAPH_PL}" --colors=io "${OUT_FOLDED}" > "${OUT_SVG}"

cat > "${TRACE_SUMMARY_STAP_SCRIPT}" <<EOS
global total
global demo
global target_pid = ${DEMO_PID}
global deadline = 0

probe begin {
  deadline = gettimeofday_s() + ${SECONDS_TO_RECORD}
}

function hit(name:string) {
  total[name]++
  if (pid() == target())
    demo[name]++
}

probe timer.s(1) {
  if (gettimeofday_s() >= deadline)
    exit()
}

probe kernel.trace("sched_switch") { hit("sched:sched_switch") }
probe kernel.trace("block_rq_issue") { hit("block:block_rq_issue") }
probe kernel.trace("block_rq_complete") { hit("block:block_rq_complete") }
probe vm.pagefault.return {
  hit("page-faults")
  if (vm_fault_contains(fault_type, VM_FAULT_MAJOR)) hit("major-faults")
  if (vm_fault_contains(fault_type, VM_FAULT_MINOR)) hit("minor-faults")
}
probe kernel.trace("vmscan:mm_vmscan_direct_reclaim_begin") { hit("vmscan:mm_vmscan_direct_reclaim_begin") }
probe kernel.trace("vmscan:mm_vmscan_direct_reclaim_end") { hit("vmscan:mm_vmscan_direct_reclaim_end") }
probe kernel.trace("vmscan:mm_vmscan_kswapd_wake") { hit("vmscan:mm_vmscan_kswapd_wake") }
probe syscall.futex { hit("syscalls:sys_enter_futex") }
probe syscall.futex.return { hit("syscalls:sys_exit_futex") }
probe syscall.fsync { hit("syscalls:sys_enter_fsync") }
probe syscall.fdatasync { hit("syscalls:sys_enter_fdatasync") }

probe end {
  printf("EVENT %s %d %d\\n", "sched:sched_switch", total["sched:sched_switch"], demo["sched:sched_switch"])
  printf("EVENT %s %d %d\\n", "block:block_rq_issue", total["block:block_rq_issue"], demo["block:block_rq_issue"])
  printf("EVENT %s %d %d\\n", "block:block_rq_complete", total["block:block_rq_complete"], demo["block:block_rq_complete"])
  printf("EVENT %s %d %d\\n", "syscalls:sys_enter_futex", total["syscalls:sys_enter_futex"], demo["syscalls:sys_enter_futex"])
  printf("EVENT %s %d %d\\n", "syscalls:sys_exit_futex", total["syscalls:sys_exit_futex"], demo["syscalls:sys_exit_futex"])
  printf("EVENT %s %d %d\\n", "syscalls:sys_enter_fsync", total["syscalls:sys_enter_fsync"], demo["syscalls:sys_enter_fsync"])
  printf("EVENT %s %d %d\\n", "syscalls:sys_enter_fdatasync", total["syscalls:sys_enter_fdatasync"], demo["syscalls:sys_enter_fdatasync"])
  printf("EVENT %s %d %d\\n", "major-faults", total["major-faults"], demo["major-faults"])
  printf("EVENT %s %d %d\\n", "minor-faults", total["minor-faults"], demo["minor-faults"])
  printf("EVENT %s %d %d\\n", "page-faults", total["page-faults"], demo["page-faults"])
  printf("EVENT %s %d %d\\n", "vmscan:mm_vmscan_direct_reclaim_begin", total["vmscan:mm_vmscan_direct_reclaim_begin"], demo["vmscan:mm_vmscan_direct_reclaim_begin"])
  printf("EVENT %s %d %d\\n", "vmscan:mm_vmscan_direct_reclaim_end", total["vmscan:mm_vmscan_direct_reclaim_end"], demo["vmscan:mm_vmscan_direct_reclaim_end"])
  printf("EVENT %s %d %d\\n", "vmscan:mm_vmscan_kswapd_wake", total["vmscan:mm_vmscan_kswapd_wake"], demo["vmscan:mm_vmscan_kswapd_wake"])
}
EOS

if ! timeout "$((SECONDS_TO_RECORD + 8))" stap -x "${DEMO_PID}" "${TRACE_SUMMARY_STAP_SCRIPT}" > "${TRACE_SUMMARY_STAP_RAW}" 2>/dev/null; then
  cat > "${TRACE_SUMMARY}" <<EOS
Available Events

Status: SystemTap summary failed in this run.
EOS
else
  count_total() {
    local ev="$1"
    awk -v ev="${ev}" '$1=="EVENT" && $2==ev {print $3; found=1} END{if(!found) print 0}' "${TRACE_SUMMARY_STAP_RAW}" | tail -n 1
  }
  count_demo() {
    local ev="$1"
    awk -v ev="${ev}" '$1=="EVENT" && $2==ev {print $4; found=1} END{if(!found) print 0}' "${TRACE_SUMMARY_STAP_RAW}" | tail -n 1
  }
  {
    echo "Available Events"
    echo ""
    echo "Status: trace summary generated by SystemTap."
    echo ""
    echo "Lock Contention:"
    echo "syscalls:sys_enter_futex - Lock wait begins [available, total=$(count_total "syscalls:sys_enter_futex"), demo_comm=$(count_demo "syscalls:sys_enter_futex")]"
    echo "syscalls:sys_exit_futex - Lock acquired [available, total=$(count_total "syscalls:sys_exit_futex"), demo_comm=$(count_demo "syscalls:sys_exit_futex")]"
    echo "syscalls:sys_enter_futex_wait - Futex wait (kernel 6+) [unavailable, total=0, demo_comm=0]"
    echo "syscalls:sys_exit_futex_wait - Futex wait returns [unavailable, total=0, demo_comm=0]"
    echo ""
    echo "Disk I/O:"
    echo "syscalls:sys_enter_fsync - Blocking fsync [available, total=$(count_total "syscalls:sys_enter_fsync"), demo_comm=$(count_demo "syscalls:sys_enter_fsync")]"
    echo "syscalls:sys_enter_fdatasync - Blocking fdatasync [available, total=$(count_total "syscalls:sys_enter_fdatasync"), demo_comm=$(count_demo "syscalls:sys_enter_fdatasync")]"
    echo "block:block_rq_issue - Disk I/O request issued [available, total=$(count_total "block:block_rq_issue"), demo_comm=$(count_demo "block:block_rq_issue")]"
    echo "block:block_rq_complete - Disk I/O completed [available, total=$(count_total "block:block_rq_complete"), demo_comm=$(count_demo "block:block_rq_complete")]"
    echo ""
    echo "Scheduler:"
    echo "sched:sched_switch - Context switch (thread goes off-CPU) [available, total=$(count_total "sched:sched_switch"), demo_comm=$(count_demo "sched:sched_switch")]"
    echo ""
    echo "Page Faults (Memory-Mapped I/O):"
    echo "major-faults - Page fault requiring disk read (mmap'd page not in RAM) [available, total=$(count_total "major-faults"), demo_comm=$(count_demo "major-faults")]"
    echo "minor-faults - Page fault resolved from RAM (mmap'd page cached) [available, total=$(count_total "minor-faults"), demo_comm=$(count_demo "minor-faults")]"
    echo "page-faults - All page faults (major + minor) [available, total=$(count_total "page-faults"), demo_comm=$(count_demo "page-faults")]"
    echo ""
    echo "Memory Pressure:"
    echo "vmscan:mm_vmscan_direct_reclaim_begin - Process blocked waiting for memory [available, total=$(count_total "vmscan:mm_vmscan_direct_reclaim_begin"), demo_comm=$(count_demo "vmscan:mm_vmscan_direct_reclaim_begin")]"
    echo "vmscan:mm_vmscan_direct_reclaim_end - Memory reclaim completed [available, total=$(count_total "vmscan:mm_vmscan_direct_reclaim_end"), demo_comm=$(count_demo "vmscan:mm_vmscan_direct_reclaim_end")]"
    echo "vmscan:mm_vmscan_kswapd_wake - Background memory reclaimer activated [available, total=$(count_total "vmscan:mm_vmscan_kswapd_wake"), demo_comm=$(count_demo "vmscan:mm_vmscan_kswapd_wake")]"
    echo ""
    echo "Notes:"
    echo "- total = event count across the whole system during the capture window."
    echo "- demo_comm = event count where SystemTap context pid matches target() from stap -x ${DEMO_PID}."
  } > "${TRACE_SUMMARY}"
fi

echo "done."
echo "stap bt   : ${OUT_STAP}"
echo "folded    : ${OUT_FOLDED}"
echo "svg       : ${OUT_SVG}"
echo "demo log  : ${DEMO_LOG}"
echo "summary   : ${TRACE_SUMMARY}"
echo "summary raw: ${TRACE_SUMMARY_STAP_RAW}"

if [[ -n "${SUDO_UID:-}" && -n "${SUDO_GID:-}" ]]; then
  chown -R "${SUDO_UID}:${SUDO_GID}" "${OUT_DIR}" || true
fi
