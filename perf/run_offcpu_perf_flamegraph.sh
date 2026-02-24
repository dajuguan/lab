#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
OUT_DIR="${ROOT_DIR}/out"
SECONDS_TO_RECORD="${1:-20}"
PERF_BIN="${PERF_BIN:-$(command -v perf || true)}"
PERF_WRAPPER=()
PERF_SCRIPT_FIELDS="${PERF_SCRIPT_FIELDS:-comm,pid,tid,cpu,time,period,event,ip,sym,dso,trace}"
DEMO_BIN_NAME="${DEMO_BIN_NAME:-offcpu-demo}"
DEMO_COMM="${DEMO_COMM:-offcpu-demo}"

RAW_DATA="${OUT_DIR}/perf.perf.data.raw"
DATA="${OUT_DIR}/perf.perf.data"
SCRIPT_TXT="${OUT_DIR}/out.perf.script.txt"
STACKS_TXT="${OUT_DIR}/offcpu.perf.stacks"
FOLDED="${OUT_DIR}/offcpu.perf.folded"
SVG="${OUT_DIR}/offcpu.perf.svg"
DEMO_LOG="${OUT_DIR}/demo.perf.log"
DEMO_BLOCK_IO_FILE="${OUT_DIR}/offcpu-demo.perf.bin"
DEMO_SYSCALL_LOG="${OUT_DIR}/offcpu-syscall.perf.log"

TRACE_DATA="${OUT_DIR}/trace-events.perf.data"
TRACE_TXT="${OUT_DIR}/trace-events.perf.txt"
TRACE_SCHED_SWITCH="${OUT_DIR}/sched_switch.perf.txt"
TRACE_BLOCK="${OUT_DIR}/block.perf.txt"
TRACE_SUMMARY="${OUT_DIR}/trace-summary.perf.txt"
TRACE_RECORD_LOG="${OUT_DIR}/trace-record.perf.log"
TRACE_PERFSTAT_TOTAL="${OUT_DIR}/perfstat.perf.pagefaults.total.csv"
TRACE_PERFSTAT_DEMO="${OUT_DIR}/perfstat.perf.pagefaults.demo.csv"

mkdir -p "${OUT_DIR}"

if [[ -z "${PERF_BIN}" || ! -x "${PERF_BIN}" ]]; then
  echo "perf not found."
  exit 1
fi

echo "using perf: ${PERF_BIN}"
"${PERF_BIN}" --version || true

TRACE_EVENT_ID="/sys/kernel/tracing/events/sched/sched_stat_sleep/id"
if [[ ! -r "${TRACE_EVENT_ID}" ]]; then
  if command -v sudo >/dev/null 2>&1; then
    echo "tracepoint metadata is not readable as current user; switching to sudo perf ..."
    PERF_WRAPPER=(sudo)
  else
    echo "No permission to read ${TRACE_EVENT_ID}."
    exit 1
  fi
fi

FLAMEGRAPH_PL="${FLAMEGRAPH_PL:-}"
STACKCOLLAPSE_PL="${STACKCOLLAPSE_PL:-}"
if [[ -z "${FLAMEGRAPH_PL}" ]]; then
  FLAMEGRAPH_PL="$(command -v flamegraph.pl || true)"
fi
if [[ -z "${STACKCOLLAPSE_PL}" ]]; then
  STACKCOLLAPSE_PL="$(command -v stackcollapse.pl || true)"
fi
if [[ -z "${FLAMEGRAPH_PL}" || -z "${STACKCOLLAPSE_PL}" ]]; then
  echo "flamegraph Perl tools not found in PATH."
  exit 1
fi

EVENT_LOCK=(
  "syscalls:sys_enter_futex"
  "syscalls:sys_exit_futex"
  "syscalls:sys_enter_futex_wait"
  "syscalls:sys_exit_futex_wait"
)
EVENT_DISK=(
  "syscalls:sys_enter_fsync"
  "syscalls:sys_enter_fdatasync"
  "block:block_rq_issue"
  "block:block_rq_complete"
)
EVENT_SCHED=("sched:sched_switch")
EVENT_FAULT=("major-faults" "minor-faults" "page-faults")
EVENT_VMSCAN=(
  "vmscan:mm_vmscan_direct_reclaim_begin"
  "vmscan:mm_vmscan_direct_reclaim_end"
  "vmscan:mm_vmscan_kswapd_wake"
)

event_desc() {
  case "$1" in
    syscalls:sys_enter_futex) echo "Lock wait begins" ;;
    syscalls:sys_exit_futex) echo "Lock acquired" ;;
    syscalls:sys_enter_futex_wait) echo "Futex wait (kernel 6+)" ;;
    syscalls:sys_exit_futex_wait) echo "Futex wait returns" ;;
    syscalls:sys_enter_fsync) echo "Blocking fsync" ;;
    syscalls:sys_enter_fdatasync) echo "Blocking fdatasync" ;;
    block:block_rq_issue) echo "Disk I/O request issued" ;;
    block:block_rq_complete) echo "Disk I/O completed" ;;
    sched:sched_switch) echo "Context switch (thread goes off-CPU)" ;;
    major-faults) echo "Page fault requiring disk read (mmap'd page not in RAM)" ;;
    minor-faults) echo "Page fault resolved from RAM (mmap'd page cached)" ;;
    page-faults) echo "All page faults (major + minor)" ;;
    vmscan:mm_vmscan_direct_reclaim_begin) echo "Process blocked waiting for memory" ;;
    vmscan:mm_vmscan_direct_reclaim_end) echo "Memory reclaim completed" ;;
    vmscan:mm_vmscan_kswapd_wake) echo "Background memory reclaimer activated" ;;
    *) echo "" ;;
  esac
}

event_available() {
  local ev="$1"
  case "${ev}" in
    page-faults)
      grep -Eq '(^|[[:space:]])page-faults([[:space:]]|$)|(^|[[:space:]])page-faults OR faults([[:space:]]|$)' <<<"${PERF_LIST_TEXT}" ;;
    *) grep -Fq "${ev}" <<<"${PERF_LIST_TEXT}" ;;
  esac
}

event_recordable() {
  local ev="$1"
  "${PERF_BIN}" stat -e "${ev}" --timeout 10 true >/dev/null 2>&1
}

count_event() {
  local ev="$1"
  if [[ ! -s "${TRACE_TXT}" ]]; then
    echo 0
    return
  fi
  rg -c "${ev}:" "${TRACE_TXT}" || true
}

count_event_demo() {
  local ev="$1"
  if [[ ! -s "${TRACE_TXT}" ]]; then
    echo 0
    return
  fi
  rg -c "${DEMO_COMM}.*${ev}:" "${TRACE_TXT}" || true
}

count_perfstat_event() {
  local file="$1"
  local ev="$2"
  if [[ ! -s "${file}" ]]; then
    echo 0
    return
  fi
  awk -F, -v ev="${ev}" '$3==ev {v=$1; gsub(/[^0-9]/,"",v); if(v=="")v=0; print v; found=1} END{if(!found) print 0}' "${file}" | tail -n 1
}

PERF_LIST_TEXT="$(${PERF_BIN} list --no-desc 2>/dev/null || true)"
all_events=("${EVENT_LOCK[@]}" "${EVENT_DISK[@]}" "${EVENT_SCHED[@]}" "${EVENT_FAULT[@]}" "${EVENT_VMSCAN[@]}")
RECORD_EVENTS=()
for ev in "${all_events[@]}"; do
  if event_available "${ev}" && event_recordable "${ev}"; then
    RECORD_EVENTS+=("${ev}")
  fi
done

echo "[1/5] building demo ..."
cargo build --manifest-path "${ROOT_DIR}/Cargo.toml" --release

echo "[2/5] launching demo ..."
OFFCPU_DEMO_FILE="${DEMO_BLOCK_IO_FILE}" \
OFFCPU_BLOCK_IO_FILE="${DEMO_BLOCK_IO_FILE}" \
OFFCPU_SYSCALL_LOG="${DEMO_SYSCALL_LOG}" \
  "${ROOT_DIR}/target/release/${DEMO_BIN_NAME}" > "${DEMO_LOG}" 2>&1 &
DEMO_PID=$!
trap 'kill ${DEMO_PID} >/dev/null 2>&1 || true' EXIT
sleep 1
if ! kill -0 "${DEMO_PID}" >/dev/null 2>&1; then
  echo "demo exited before profiling; see ${DEMO_LOG}"
  exit 1
fi

echo "[3/5] recording off-cpu and trace events for ${SECONDS_TO_RECORD}s (pid=${DEMO_PID}) ..."
: > "${TRACE_RECORD_LOG}"
EVENT_ARGS=()
for ev in "${RECORD_EVENTS[@]}"; do
  EVENT_ARGS+=( -e "${ev}" )
done
"${PERF_WRAPPER[@]}" "${PERF_BIN}" record \
  "${EVENT_ARGS[@]}" -a -g -o "${TRACE_DATA}" \
  -- sleep "${SECONDS_TO_RECORD}" >>"${TRACE_RECORD_LOG}" 2>&1 &
TRACE_REC_PID=$!

"${PERF_WRAPPER[@]}" "${PERF_BIN}" stat -x, \
  -e major-faults,minor-faults,page-faults \
  -a -- sleep "${SECONDS_TO_RECORD}" >/dev/null 2>"${TRACE_PERFSTAT_TOTAL}" &
PERFSTAT_TOTAL_PID=$!

"${PERF_WRAPPER[@]}" "${PERF_BIN}" stat -x, \
  -e major-faults,minor-faults,page-faults \
  -p "${DEMO_PID}" -- sleep "${SECONDS_TO_RECORD}" >/dev/null 2>"${TRACE_PERFSTAT_DEMO}" &
PERFSTAT_DEMO_PID=$!

"${PERF_WRAPPER[@]}" "${PERF_BIN}" record \
  -e sched:sched_stat_sleep -e sched:sched_switch -e sched:sched_process_exit \
  -p "${DEMO_PID}" -g -o "${RAW_DATA}" -- sleep "${SECONDS_TO_RECORD}"

wait "${TRACE_REC_PID}" || true
wait "${PERFSTAT_TOTAL_PID}" || true
wait "${PERFSTAT_DEMO_PID}" || true

echo "[4/5] injecting sched-stat and folding stacks ..."
"${PERF_WRAPPER[@]}" "${PERF_BIN}" inject -v -s -i "${RAW_DATA}" -o "${DATA}"
SCRIPT_MODE="offcpu-injected"
set +e
"${PERF_WRAPPER[@]}" "${PERF_BIN}" script -F "${PERF_SCRIPT_FIELDS}" -i "${DATA}" > "${SCRIPT_TXT}"
SCRIPT_RC=$?
set -e
if [[ "${SCRIPT_RC}" -ne 0 || ! -s "${SCRIPT_TXT}" ]]; then
  SCRIPT_MODE="raw-fallback"
  "${PERF_WRAPPER[@]}" "${PERF_BIN}" script -F "${PERF_SCRIPT_FIELDS}" -i "${RAW_DATA}" > "${SCRIPT_TXT}"
fi

awk -v demo_comm="${DEMO_COMM}" '
    BEGIN { keep=0; exec=""; period_ms=0; }
    /sched:sched_stat_sleep:/ {
      keep=1; exec=""; period_ms=0;
      if (match($0, /comm=([^ ]+)/, c)) exec=c[1]; else exec=$1;
      if (match($0, /delay=([0-9]+)/, d)) period_ms=int(d[1]/1000000);
      else if ($5 + 0 > 0) period_ms=int($5/1000000);
      if (demo_comm != "" && exec != demo_comm) keep=0;
      next;
    }
    keep && NF > 1 && NF <= 4 && period_ms > 0 { print $2; next; }
    keep && NF < 2 && period_ms > 0 {
      printf "%s\n%d\n\n", exec, period_ms;
      keep=0; exec=""; period_ms=0;
    }
' "${SCRIPT_TXT}" > "${STACKS_TXT}"

"${STACKCOLLAPSE_PL}" "${STACKS_TXT}" > "${FOLDED}"

if [[ -s "${TRACE_DATA}" ]]; then
  "${PERF_WRAPPER[@]}" "${PERF_BIN}" script -F comm,pid,tid,cpu,time,event,trace -i "${TRACE_DATA}" > "${TRACE_TXT}" || true
  awk '/sched:sched_switch:/' "${TRACE_TXT}" > "${TRACE_SCHED_SWITCH}" || true
  awk '/block:block_rq_issue:|block:block_rq_complete:/' "${TRACE_TXT}" > "${TRACE_BLOCK}" || true
fi

{
  echo "Available Events"
  echo ""
  if [[ -s "${TRACE_DATA}" ]]; then
    echo "Status: trace event collection enabled."
  else
    echo "Status: trace event collection enabled but no trace data captured."
    echo "Hint: see ${TRACE_RECORD_LOG}"
  fi
  echo ""
  echo "Lock Contention:"
  for ev in "${EVENT_LOCK[@]}"; do
    if event_available "${ev}"; then
      echo "${ev} - $(event_desc "${ev}") [available, total=$(count_event "${ev}"), demo_comm=$(count_event_demo "${ev}")]"
    else
      echo "${ev} - $(event_desc "${ev}") [unavailable, total=0, demo_comm=0]"
    fi
  done
  echo ""
  echo "Disk I/O:"
  for ev in "${EVENT_DISK[@]}"; do
    if event_available "${ev}"; then
      echo "${ev} - $(event_desc "${ev}") [available, total=$(count_event "${ev}"), demo_comm=$(count_event_demo "${ev}")]"
    else
      echo "${ev} - $(event_desc "${ev}") [unavailable, total=0, demo_comm=0]"
    fi
  done
  echo ""
  echo "Scheduler:"
  for ev in "${EVENT_SCHED[@]}"; do
    if event_available "${ev}"; then
      echo "${ev} - $(event_desc "${ev}") [available, total=$(count_event "${ev}"), demo_comm=$(count_event_demo "${ev}")]"
    else
      echo "${ev} - $(event_desc "${ev}") [unavailable, total=0, demo_comm=0]"
    fi
  done
  echo ""
  echo "Page Faults (Memory-Mapped I/O):"
  for ev in "${EVENT_FAULT[@]}"; do
    if event_available "${ev}"; then
      echo "${ev} - $(event_desc "${ev}") [available, total=$(count_perfstat_event "${TRACE_PERFSTAT_TOTAL}" "${ev}"), demo_comm=$(count_perfstat_event "${TRACE_PERFSTAT_DEMO}" "${ev}")]"
    else
      echo "${ev} - $(event_desc "${ev}") [unavailable, total=0, demo_comm=0]"
    fi
  done
  echo ""
  echo "Memory Pressure:"
  for ev in "${EVENT_VMSCAN[@]}"; do
    if event_available "${ev}"; then
      echo "${ev} - $(event_desc "${ev}") [available, total=$(count_event "${ev}"), demo_comm=$(count_event_demo "${ev}")]"
    else
      echo "${ev} - $(event_desc "${ev}") [unavailable, total=0, demo_comm=0]"
    fi
  done
  echo ""
  echo "Notes:"
  echo "- total = event count across the whole system during the capture window."
  echo "- demo_comm = event count where the perf 'comm' column equals ${DEMO_COMM}."
  echo "- Page-fault counters in this perf summary come from perf stat for better comparability."
} > "${TRACE_SUMMARY}"

echo "[5/5] rendering flamegraph ..."
"${FLAMEGRAPH_PL}" --countname=ms --title "Off-CPU Time Flame Graph (${SCRIPT_MODE})" --colors=io "${FOLDED}" > "${SVG}"

echo "done."
echo "raw data      : ${RAW_DATA}"
echo "merged        : ${DATA}"
echo "perf script   : ${SCRIPT_TXT}"
echo "stacks        : ${STACKS_TXT}"
echo "folded        : ${FOLDED}"
echo "svg           : ${SVG}"
echo "demo log      : ${DEMO_LOG}"
echo "trace data    : ${TRACE_DATA}"
echo "trace text    : ${TRACE_TXT}"
echo "sched_switch  : ${TRACE_SCHED_SWITCH}"
echo "block events  : ${TRACE_BLOCK}"
echo "trace summary : ${TRACE_SUMMARY}"
echo "trace log     : ${TRACE_RECORD_LOG}"

if [[ -n "${SUDO_UID:-}" && -n "${SUDO_GID:-}" ]]; then
  chown -R "${SUDO_UID}:${SUDO_GID}" "${OUT_DIR}" || true
fi
