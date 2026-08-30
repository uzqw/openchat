#!/usr/bin/env bash
#
# 真实 Gemini smoke（人工 opt-in 专用，绝不默认运行）。
#
# 主流程（真实发送 prompt 到真实 Gemini 账号）：
#   LIVE_GEMINI_SMOKE=1 scripts/smoke-gemini.sh
# 依次验证（prompts/implement-gemini-v1.md §真实 Gemini Smoke）：
#   1. opencli 固定版本探针（v1.8.7）
#   2. opencli doctor
#   3. gemini status / whoami（JSON）
#   4. gemini models（JSON）
#   5. 新会话第一轮 gemini ask（--new true）
#   6. 同一会话第二轮追问，确认上下文生效
#
# 独立 auth 分支（只读 whoami，不发送 prompt；仅用于另一专用未登录 profile）：
#   LIVE_GEMINI_AUTH_SMOKE=1 scripts/smoke-gemini.sh
#
# 纪律：
#   - 主流程未显式设置 LIVE_GEMINI_SMOKE=1 时拒绝运行（即使检测到 Chrome/登录态）。
#   - 不自动退出登录、不制造 timeout、不改变账号安全状态；不调用 login/logout。
#   - 与后端写守卫同源：本地 adapter override（~/.opencli/clis/gemini）或
#     已安装 OpenCLI plugin 存在时，拒绝任何写操作（ask），fail closed。
#   - auth_required 只在另一专用未登录 profile 下以 LIVE_GEMINI_AUTH_SMOKE=1
#     单独人工验证（whoami 期望锁定合同退出码 77）。
#   - 不打印 cookie / profile 内容 / 敏感路径；opencli 输出即命令 stdout。
set -euo pipefail

SENTINEL='💬 [NO RESPONSE]'
VERSION_REQUIRED='1.8.7'

die() { echo "smoke-gemini: $*" >&2; exit 1; }

OPENCLI_PATH="${OPENCLI_PATH:-opencli}"

# ---------- auth_required 独立人工分支（不做其他步骤） ----------
if [[ "${LIVE_GEMINI_AUTH_SMOKE:-}" == "1" ]]; then
  if [[ -z "${OPENCLI_PROFILE:-}" ]]; then
    die "OPENCLI_PROFILE is required (dedicated logged-out OpenCLI profile)"
  fi
  echo "== auth smoke (LIVE_GEMINI_AUTH_SMOKE=1, profile: ${OPENCLI_PROFILE})"
  set +e
  who_out="$("$OPENCLI_PATH" --profile "$OPENCLI_PROFILE" gemini whoami --format json 2>&1)"
  rc=$?
  set -e
  if [[ $rc -eq 77 ]]; then
    echo "    PASS: whoami exited 77 (auth_required) as expected for a logged-out profile"
    echo "    note: 确认退出码来自未登录 profile，而非账号安全状态的任何变更。"
    exit 0
  fi
  die "expected exit 77 (auth_required), got $rc; output: $(echo "$who_out" | head -c 300)"
fi

# ---------- opt-in 闸门（主流程） ----------
if [[ "${LIVE_GEMINI_SMOKE:-}" != "1" ]]; then
  die "refusing to run without opt-in: set LIVE_GEMINI_SMOKE=1 to send real prompts"
fi
if [[ -z "${OPENCLI_PROFILE:-}" ]]; then
  die "OPENCLI_PROFILE is required (dedicated OpenCLI profile)"
fi

echo "== openchat Gemini smoke (profile: ${OPENCLI_PROFILE}, opt-in)"
echo

# ---------- 1. 固定版本探针 ----------
echo "== [1/6] opencli --version"
version_out="$("$OPENCLI_PATH" --version)"
echo "$version_out" | sed 's/^/    /'
case "$version_out" in
  *"$VERSION_REQUIRED"*) ;;
  *) die "OpenCLI version mismatch: require ${VERSION_REQUIRED}, got $(echo "$version_out" | head -c 200)" ;;
esac

# ---------- 2. doctor ----------
echo "== [2/6] opencli --profile \"\$OPENCLI_PROFILE\" doctor"
"$OPENCLI_PATH" --profile "$OPENCLI_PROFILE" doctor

# ---------- 3. status / whoami ----------
echo "== [3/6] gemini status / whoami (JSON)"
for cmd in status whoami; do
  out="$("$OPENCLI_PATH" --profile "$OPENCLI_PROFILE" gemini "$cmd" --format json)"
  echo "  -- $cmd: $(echo "$out" | head -c 400)"
done

# ---------- 4. models ----------
echo "== [4/6] gemini models --format json"
models="$("$OPENCLI_PATH" --profile "$OPENCLI_PROFILE" gemini models --format json)"
echo "$models" | head -c 600
echo

# ---------- 写守卫检查（写操作前 fail closed，与后端同源） ----------
if [[ -e "$HOME/.opencli/clis/gemini" ]]; then
  echo "    refusing write: local adapter override exists at \"\$HOME/.opencli/clis/gemini\"" >&2
  exit 1
fi
if [[ -d "$HOME/.opencli/plugins" ]] && [[ -n "$(ls -A "$HOME/.opencli/plugins" 2>/dev/null)" ]]; then
  echo "    refusing write: an OpenCLI plugin is installed under \"\$HOME/.opencli/plugins\"" >&2
  exit 1
fi

# ---------- 5. 新会话第一轮 ----------
# 展示前缀仅在识别固定 sentinel 时使用（与后端 IsSentinel 同义），不剥正文。
p1='Reply with exactly one word: orchid.'
echo "== [5/6] first turn in a new session (--new true)"
r1="$("$OPENCLI_PATH" --profile "$OPENCLI_PROFILE" gemini ask "$p1" --new true --format json)"
case "$r1" in
  "$SENTINEL"*) die "first turn returned the no-response sentinel: ${SENTINEL}" ;;
esac
case "$r1" in
  *'"response"'*) ;;
  *) die "first turn returned no \"response\" field: $(echo "$r1" | head -c 300)" ;;
esac
echo "    ${r1:0:600}"

# ---------- 6. 同一会话第二轮追问 ----------
p2='What single word did I ask you to reply with in your previous message? Reply with only that word.'
echo "== [6/6] follow-up turn in the same session (no --new)"
r2="$("$OPENCLI_PATH" --profile "$OPENCLI_PROFILE" gemini ask "$p2" --format json)"
case "$r2" in
  "$SENTINEL"*) die "follow-up turn returned the no-response sentinel: ${SENTINEL}" ;;
esac
case "$r2" in
  *'"response"'*) ;;
  *) die "follow-up turn returned no \"response\" field: $(echo "$r2" | head -c 300)" ;;
esac
echo "    ${r2:0:600}"
case "$r2" in
  *orchid*|*Orchid*|*ORCHID*) echo "    context check: PASS (follow-up referenced the first turn)" ;;
  *) die "context check FAILED: follow-up did not carry the marker word. Inspect both replies above." ;;
esac

echo
echo "live Gemini smoke: PASS (version/doctor/status/whoami/models/first/follow-up)"
echo "note: 本脚本未退出登录、未制造 timeout、未改变账号安全状态。"
