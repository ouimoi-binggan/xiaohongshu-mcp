#!/bin/bash

set -u

REPO_URL="https://github.com/ouimoi-binggan/xiaohongshu-mcp.git"
BRANCH="feature/favorites-albums"
RUNTIME_DIR="$HOME/Downloads/xiaohongshu_mcp"
RUNTIME_BIN="$RUNTIME_DIR/xiaohongshu-mcp-darwin-arm64"
LAUNCHER="$HOME/Desktop/地瓜炉.command"
BACKUP_DIR="$RUNTIME_DIR/backups"
STAGE_BIN="$RUNTIME_DIR/.xiaohongshu-mcp-darwin-arm64.new.$$"

pause() {
  printf '\n按回车关闭窗口…'
  read -r _
}

fail() {
  printf '\n✗ 升级停止：%s\n' "$1"
  printf '旧炉芯没有被替换。\n'
  pause
  exit 1
}

cleanup() {
  if [ -n "${WORK_DIR:-}" ] && [ -d "$WORK_DIR" ]; then
    rm -rf "$WORK_DIR"
  fi
  if [ -f "$STAGE_BIN" ]; then
    rm -f "$STAGE_BIN"
  fi
}
trap cleanup EXIT

printf '🍠 地瓜炉安全升级器\n'
printf '====================\n\n'

command -v git >/dev/null 2>&1 || fail "没有找到 git"
command -v go >/dev/null 2>&1 || fail "没有找到 Go，请先确认 go version 可用"
[ -f "$RUNTIME_BIN" ] || fail "没有找到正在使用的主程序：$RUNTIME_BIN"
[ -f "$LAUNCHER" ] || fail "没有找到启动脚本：$LAUNCHER"

WORK_DIR="$(mktemp -d "${TMPDIR:-/tmp}/xiaohongshu-upgrade.XXXXXX")" || fail "无法建立临时目录"

printf '① 下载哥哥保存的最新版…\n'
git clone --quiet --depth 1 --branch "$BRANCH" "$REPO_URL" "$WORK_DIR/source" || fail "下载源码失败"

cd "$WORK_DIR/source" || fail "无法进入源码目录"

printf '② 运行测试…\n'
go test ./... || fail "测试没有通过"

printf '③ 为 Apple 芯片编译新炉芯…\n'
GOOS=darwin GOARCH=arm64 go build -o "$WORK_DIR/new-binary" . || fail "编译失败"
file "$WORK_DIR/new-binary" | grep -q 'Mach-O 64-bit executable arm64' || fail "编译结果不是 Apple ARM64 程序"

printf '④ 测试通过，备份旧炉芯…\n'
mkdir -p "$BACKUP_DIR" || fail "无法建立备份目录"
STAMP="$(date '+%Y%m%d-%H%M%S')"
BACKUP_BIN="$BACKUP_DIR/xiaohongshu-mcp-darwin-arm64.$STAMP.backup"
cp -p "$RUNTIME_BIN" "$BACKUP_BIN" || fail "备份旧炉芯失败"

printf '⑤ 暂时熄火并原子换芯…\n'
pkill -f 'tunnel-client run --profile xiaohongshu' >/dev/null 2>&1 || true
pkill -f 'xiaohongshu-mcp-darwin-arm64' >/dev/null 2>&1 || true
sleep 1

cp "$WORK_DIR/new-binary" "$STAGE_BIN" || fail "无法把新炉芯送入运行目录"
chmod +x "$STAGE_BIN" || fail "无法设置新炉芯权限"
mv -f "$STAGE_BIN" "$RUNTIME_BIN" || fail "替换炉芯失败"

printf '⑥ 重新点火并检查两个进程…\n'
open "$LAUNCHER" || true
sleep 4

if ! pgrep -f 'xiaohongshu-mcp-darwin-arm64' >/dev/null 2>&1 || \
   ! pgrep -f 'tunnel-client run --profile xiaohongshu' >/dev/null 2>&1; then
  printf '新炉芯未能正常点火，正在恢复备份…\n'
  pkill -f 'tunnel-client run --profile xiaohongshu' >/dev/null 2>&1 || true
  pkill -f 'xiaohongshu-mcp-darwin-arm64' >/dev/null 2>&1 || true
  cp -p "$BACKUP_BIN" "$RUNTIME_BIN" || fail "自动恢复失败；备份位于 $BACKUP_BIN"
  chmod +x "$RUNTIME_BIN"
  open "$LAUNCHER" || true
  fail "新版启动失败，已经恢复并重启旧版"
fi

printf '\n✓ 升级成功，地瓜炉已经重新点燃。\n'
printf '旧炉芯备份：%s\n' "$BACKUP_BIN"
printf '当前版本提交：%s\n' "$(git rev-parse --short HEAD)"
pause
