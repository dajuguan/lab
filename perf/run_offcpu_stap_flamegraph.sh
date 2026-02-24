#!/usr/bin/env bash
set -euo pipefail

echo 1 | sudo tee /proc/sys/kernel/sched_schedstats
echo 1 | sudo tee /proc/sys/kernel/perf_event_paranoid


ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
OUT_DIR="${ROOT_DIR}/out"
SECONDS_TO_RECORD="${1:-10}"

DEMO_BIN_NAME="${DEMO_BIN_NAME:-offcpu-demo}"
SAMPLE_BT_OFFCPU="${SAMPLE_BT_OFFCPU:-$(command -v sample-bt-off-cpu || true)}"
STACKCOLLAPSE_STAP_PL="${STACKCOLLAPSE_STAP_PL:-/opt/FlameGraph/stackcollapse-stap.pl}"
FLAMEGRAPH_PL="${FLAMEGRAPH_PL:-/opt/FlameGraph/flamegraph.pl}"
# Include kernel stack by default so syscall names (e.g. __x64_sys_futex) appear in the flamegraph.
OFFCPU_INCLUDE_KERNEL="${OFFCPU_INCLUDE_KERNEL:-1}"

OUT_STAP="${OUT_DIR}/out.stap"
OUT_FOLDED="${OUT_DIR}/out.folded"
OUT_SVG="${OUT_DIR}/offcpu.svg"
DEMO_LOG="${OUT_DIR}/demo.log"

mkdir -p "${OUT_DIR}"

KERNEL_RELEASE="$(uname -r)"
if [[ ! -f "/lib/modules/${KERNEL_RELEASE}/build/.config" ]]; then
  echo "SystemTap kernel build config missing: /lib/modules/${KERNEL_RELEASE}/build/.config"
  echo "Current kernel cannot run sample-bt-off-cpu directly (common on WSL2 custom kernels)."
  echo "Use the perf-based script instead: sudo -E ./run_offcpu_flamegraph.sh ${SECONDS_TO_RECORD}"
  exit 1
fi

if [[ ! -x "${SAMPLE_BT_OFFCPU}" ]]; then
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

echo "[1/4] building demo ..."
cargo build --manifest-path "${ROOT_DIR}/Cargo.toml" --release

echo "[2/4] launching demo ..."
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
  echo "Run with sudo, or configure stap permissions (stapusr/stapsys/stapdev)."
  echo "If on WSL2 and kernel build config is unavailable, use run_offcpu_flamegraph.sh."
  exit 1
fi

echo "[4/4] rendering flamegraph ..."
"${STACKCOLLAPSE_STAP_PL}" "${OUT_STAP}" > "${OUT_FOLDED}"
"${FLAMEGRAPH_PL}" --colors=io "${OUT_FOLDED}" > "${OUT_SVG}"

echo "done."
echo "stap   : ${OUT_STAP}"
echo "folded : ${OUT_FOLDED}"
echo "svg    : ${OUT_SVG}"
echo "demo   : ${DEMO_LOG}"

if [[ -n "${SUDO_UID:-}" && -n "${SUDO_GID:-}" ]]; then
  chown -R "${SUDO_UID}:${SUDO_GID}" "${OUT_DIR}" || true
fi
