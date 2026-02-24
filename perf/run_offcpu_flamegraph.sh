#!/usr/bin/env bash
set -euo pipefail

# A minimal off-cpu tracing pipeline inspired by:
# sched_stat_sleep + sched_switch + sched_process_exit -> perf inject --sched-stat
# Then fold stack and render flamegraph.

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
OUT_DIR="${ROOT_DIR}/out"
SECONDS_TO_RECORD="${1:-20}"
PERF_BIN="${PERF_BIN:-$(command -v perf || true)}"
PERF_WRAPPER=()
# Blog-compatible perf script fields for off-cpu synthesis.
PERF_SCRIPT_FIELDS="${PERF_SCRIPT_FIELDS:-comm,pid,tid,cpu,time,period,event,ip,sym,dso,trace}"
DEMO_BIN_NAME="${DEMO_BIN_NAME:-offcpu-demo}"
DEMO_COMM="${DEMO_COMM:-offcpu-demo}"

RAW_DATA="${OUT_DIR}/perf.data.raw"
DATA="${OUT_DIR}/perf.data"
SCRIPT_TXT="${OUT_DIR}/out.perf"
STACKS_TXT="${OUT_DIR}/offcpu.stacks"
FOLDED="${OUT_DIR}/offcpu.folded"
SVG="${OUT_DIR}/offcpu.svg"
DEMO_LOG="${OUT_DIR}/demo.log"
DEMO_BLOCK_IO_FILE="${OUT_DIR}/offcpu-demo.bin"
DEMO_SYSCALL_LOG="${OUT_DIR}/offcpu-syscall.log"

mkdir -p "${OUT_DIR}"

if [[ -z "${PERF_BIN}" || ! -x "${PERF_BIN}" ]]; then
  echo "perf not found. Please install linux perf tools first."
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
    cat <<EOF
No permission to read ${TRACE_EVENT_ID}, and sudo is unavailable.
Run this script as root, or grant access to tracefs event files.
EOF
    exit 1
  fi
fi

if [[ ! -r /proc/sys/kernel/perf_event_paranoid ]]; then
  echo "Cannot read /proc/sys/kernel/perf_event_paranoid."
  exit 1
fi

PARANOID="$(cat /proc/sys/kernel/perf_event_paranoid)"
if [[ "${PARANOID}" -gt 1 ]]; then
  cat <<EOF
perf_event_paranoid=${PARANOID} is restrictive for tracing.
Try (as root):
  echo 1 > /proc/sys/kernel/perf_event_paranoid
EOF
fi

# Off-CPU synthesis via `perf inject --sched-stat` needs schedstats enabled.
if [[ -r /proc/sys/kernel/sched_schedstats ]]; then
  SCHEDSTATS="$(cat /proc/sys/kernel/sched_schedstats)"
  if [[ "${SCHEDSTATS}" != "1" ]]; then
    echo "kernel.sched_schedstats=${SCHEDSTATS}; trying to enable it for off-cpu analysis ..."
    if [[ -w /proc/sys/kernel/sched_schedstats ]]; then
      echo 1 > /proc/sys/kernel/sched_schedstats || true
    elif command -v sudo >/dev/null 2>&1; then
      echo 1 | sudo tee /proc/sys/kernel/sched_schedstats >/dev/null || true
    fi
    SCHEDSTATS_NOW="$(cat /proc/sys/kernel/sched_schedstats)"
    if [[ "${SCHEDSTATS_NOW}" != "1" ]]; then
      cat <<EOF
failed to enable kernel.sched_schedstats (current=${SCHEDSTATS_NOW}).
Without it, perf inject --sched-stat may produce no events.
Try manually (root):
  echo 1 > /proc/sys/kernel/sched_schedstats
EOF
    fi
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
  cat <<EOF
flamegraph Perl tools not found in PATH.
Provide:
  FLAMEGRAPH_PL=/opt/FlameGraph/flamegraph.pl 
  STACKCOLLAPSE_PL=/opt/FlameGraph/stackcollapse.pl
EOF
  exit 1
fi

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
  tail -n 80 "${DEMO_LOG}" || true
  exit 1
fi

echo "[3/5] recording off-cpu events for ${SECONDS_TO_RECORD}s (pid=${DEMO_PID}) ..."
"${PERF_WRAPPER[@]}" "${PERF_BIN}" record \
  -e sched:sched_stat_sleep \
  -e sched:sched_switch \
  -e sched:sched_process_exit \
  -p "${DEMO_PID}" \
  -g \
  -o "${RAW_DATA}" \
  -- sleep "${SECONDS_TO_RECORD}"

echo "[4/5] injecting sched-stat and folding stacks ..."
"${PERF_WRAPPER[@]}" "${PERF_BIN}" inject -v -s -i "${RAW_DATA}" -o "${DATA}"
SCRIPT_MODE="offcpu-injected"
set +e
"${PERF_WRAPPER[@]}" "${PERF_BIN}" script -F "${PERF_SCRIPT_FIELDS}" -i "${DATA}" > "${SCRIPT_TXT}"
SCRIPT_RC=$?
set -e
if [[ "${SCRIPT_RC}" -ne 0 || ! -s "${SCRIPT_TXT}" ]]; then
  if [[ "${SCRIPT_RC}" -eq 0 ]]; then
    echo "warn: perf script returned rc=0 on injected data but produced no output; falling back to raw perf.data"
  else
    echo "warn: perf script failed on injected data (rc=${SCRIPT_RC}); falling back to raw perf.data"
  fi
  echo "warn: this fallback can still produce a flamegraph, but off-cpu synthesis may be incomplete."
  SCRIPT_MODE="raw-fallback"
  "${PERF_WRAPPER[@]}" "${PERF_BIN}" script -F "${PERF_SCRIPT_FIELDS}" -i "${RAW_DATA}" > "${SCRIPT_TXT}"
fi
if [[ ! -s "${SCRIPT_TXT}" ]]; then
  echo "perf script produced no events after --sched-stat inject."
  echo "This usually means the kernel/event combination does not provide sched-stat usable data."
  exit 1
fi

awk -v demo_comm="${DEMO_COMM}" '
    BEGIN {
      keep = 0;
      exec = "";
      period_ms = 0;
    }
    /sched:sched_stat_sleep:/ {
      keep = 1;
      exec = "";
      period_ms = 0;

      # Prefer trace payload from injected events: comm=<task> delay=<ns>.
      if (match($0, /comm=([^ ]+)/, c)) {
        exec = c[1];
      } else {
        exec = $1;
      }
      if (match($0, /delay=([0-9]+)/, d)) {
        period_ms = int(d[1] / 1000000);
      } else if ($5 + 0 > 0) {
        period_ms = int($5 / 1000000);
      }
      if (demo_comm != "" && exec != demo_comm) {
        keep = 0;
      }
      next;
    }
    keep && NF > 1 && NF <= 4 && period_ms > 0 {
      print $2;
      next;
    }
    keep && NF < 2 && period_ms > 0 {
      printf "%s\n%d\n\n", exec, period_ms;
      keep = 0;
      exec = "";
      period_ms = 0;
      next;
    }
' "${SCRIPT_TXT}" > "${STACKS_TXT}"

"${STACKCOLLAPSE_PL}" "${STACKS_TXT}" > "${FOLDED}"
if [[ ! -s "${FOLDED}" ]]; then
  echo "stack collapse produced no data."
  echo "Tip: check perf script output format and awk field mapping."
  exit 1
fi

echo "[5/5] rendering flamegraph ..."
"${FLAMEGRAPH_PL}" --countname=ms --title "Off-CPU Time Flame Graph (${SCRIPT_MODE})" --colors=io "${FOLDED}" > "${SVG}"

echo "done."
echo "raw data : ${RAW_DATA}"
echo "merged   : ${DATA}"
echo "perf txt : ${SCRIPT_TXT}"
echo "stacks   : ${STACKS_TXT}"
echo "folded   : ${FOLDED}"
echo "svg      : ${SVG}"
echo "demo log : ${DEMO_LOG}"
echo "mode     : ${SCRIPT_MODE}"

# If invoked via sudo, hand result files back to the original user for easier inspection.
if [[ -n "${SUDO_UID:-}" && -n "${SUDO_GID:-}" ]]; then
  chown -R "${SUDO_UID}:${SUDO_GID}" "${OUT_DIR}" || true
fi
