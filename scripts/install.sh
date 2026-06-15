#!/bin/bash
# =============================================================================
# MonkeyCode - 一键本地部署脚本
# -----------------------------------------------------------------------------
# 兼容：Linux x86_64 / aarch64、macOS (Intel / Apple Silicon)
# 用法：bash install.sh [--dir /path] [--no-ollama] [--yes]
# =============================================================================

set -e
set -o pipefail

# -----------------------------------------------------------------------------
# 0. 颜色 / 日志工具
# -----------------------------------------------------------------------------
NC='\033[0m'
GREEN='\033[32m'
YELLOW='\033[33m'
RED='\033[31m'
BOLD='\033[1m'

log_info()  { printf "${BOLD}[%s]${NC} %s\n" "$(date '+%Y-%m-%d %H:%M:%S')" "$*"; }
log_ok()    { printf "${GREEN}[%s] ✓${NC} %s\n" "$(date '+%Y-%m-%d %H:%M:%S')" "$*"; }
log_warn()  { printf "${YELLOW}[%s] !${NC} %s\n" "$(date '+%Y-%m-%d %H:%M:%S')" "$*"; }
log_error() { printf "${RED}[%s] ✗${NC} %s\n" "$(date '+%Y-%m-%d %H:%M:%S')" "$*" 1>&2; }
die()       { log_error "$*"; exit 1; }

# -----------------------------------------------------------------------------
# 1. 打印 Banner
# -----------------------------------------------------------------------------
printf "${BOLD}%s${NC}\n" "======================================================"
printf "${BOLD}%s${NC}\n" "   🦍  MonkeyCode 本地一键部署脚本  "
printf "${BOLD}%s${NC}\n" "   一条命令，在本机拉一套完整 AI 开发平台  "
printf "${BOLD}%s${NC}\n" "======================================================"
echo

# -----------------------------------------------------------------------------
# 2. 解析参数
# -----------------------------------------------------------------------------
INSTALL_DIR="$HOME/.monkeycode"
NO_OLLAMA=0
AUTO_YES=0

while [ $# -gt 0 ]; do
  case "$1" in
    --dir)
      [ -z "$2" ] && die "--dir 需要一个路径参数"
      INSTALL_DIR="$2"
      shift 2
      ;;
    --no-ollama)
      NO_OLLAMA=1
      shift
      ;;
    --yes|-y)
      AUTO_YES=1
      shift
      ;;
    --help|-h)
      cat <<'HELP'
用法: bash install.sh [选项]

  --dir <path>    自定义安装目录 (默认 ~/.monkeycode)
  --no-ollama     不启动本地 Ollama 服务
  --yes, -y       跳过所有交互，使用默认值
  --help, -h      显示本帮助

示例:
  bash install.sh                            # 默认安装 + 交互
  bash install.sh --no-ollama                # 不启动 Ollama
  bash install.sh --dir /opt/monkeycode --yes  # 指定目录 + 全自动
HELP
      exit 0
      ;;
    *)
      die "未知参数: $1 (使用 --help 查看用法)"
      ;;
  esac
done

log_info "安装目录: $INSTALL_DIR"
if [ "$NO_OLLAMA" -eq 1 ]; then
  log_warn "已指定 --no-ollama：将不启动本地 Ollama 服务"
fi
if [ "$AUTO_YES" -eq 1 ]; then
  log_warn "已指定 --yes：将跳过所有交互式提示，使用默认值"
fi

# -----------------------------------------------------------------------------
# 3. 架构 & 系统检测
# -----------------------------------------------------------------------------
log_info ">>> 检测运行环境"

MACHINE_ARCH="$(uname -m)"
case "$MACHINE_ARCH" in
  x86_64|amd64)
    ARCH="amd64"
    ;;
  aarch64|arm64)
    ARCH="arm64"
    ;;
  *)
    die "不支持的架构: $MACHINE_ARCH (仅支持 x86_64 / aarch64)"
    ;;
esac

OS_NAME="$(uname -s)"
case "$OS_NAME" in
  Linux)  OS="linux" ;;
  Darwin) OS="macos" ;;
  *)      die "不支持的操作系统: $OS_NAME (仅支持 Linux / macOS)" ;;
esac

log_ok "系统: $OS / $ARCH"

# -----------------------------------------------------------------------------
# 4. Docker & docker compose 检测
# -----------------------------------------------------------------------------
log_info ">>> 检测 Docker / Docker Compose"

if ! command -v docker >/dev/null 2>&1; then
  cat <<'EOF'
Docker 未安装。请先在本机安装 Docker Desktop 或 Docker Engine：

  Linux  : https://docs.docker.com/engine/install/
  macOS  : https://docs.docker.com/desktop/install/mac-install/

安装完成后重新执行本脚本。
EOF
  die "Docker 未找到"
fi

# 支持 "docker compose v2" 插件；也兼容独立的 "docker-compose" 命令
DOCKER_COMPOSE_CMD=""
if docker compose version >/dev/null 2>&1; then
  DOCKER_COMPOSE_CMD="docker compose"
  log_ok "Docker Compose (v2 插件): $(docker compose version --short 2>/dev/null || true)"
elif command -v docker-compose >/dev/null 2>&1; then
  DOCKER_COMPOSE_CMD="docker-compose"
  log_ok "Docker Compose (独立二进制): $(docker-compose --version --short 2>/dev/null || true)"
else
  die "未检测到 docker compose (v2) 或 docker-compose。请先安装 Docker Compose。"
fi

# 确认 Docker daemon 可用
if ! docker info >/dev/null 2>&1; then
  die "Docker 守护进程未启动或当前用户无权限访问。请启动 Docker Desktop / dockerd，并确认当前用户在 docker 组。"
fi
log_ok "Docker daemon 可正常访问"

# -----------------------------------------------------------------------------
# 5. 创建安装目录
# -----------------------------------------------------------------------------
log_info ">>> 创建安装目录与数据目录"

mkdir -p "$INSTALL_DIR" || die "无法创建目录: $INSTALL_DIR"
mkdir -p "$INSTALL_DIR/data" || die "无法创建目录: $INSTALL_DIR/data"

# 将 INSTALL_DIR 规范化为绝对路径（docker-compose.yml 中挂载使用）
if command -v greadlink >/dev/null 2>&1; then
  INSTALL_DIR="$(greadlink -f "$INSTALL_DIR")"
elif command -v readlink >/dev/null 2>&1; then
  # macOS 的 readlink 不支持 -f，退化为 pwd 方式
  _abs="$(cd "$INSTALL_DIR" && pwd)"
  [ -n "$_abs" ] && INSTALL_DIR="$_abs"
fi

log_ok "安装目录: $INSTALL_DIR"
log_ok "数据目录: $INSTALL_DIR/data"

# -----------------------------------------------------------------------------
# 6. 拉取 docker-compose.local.yml 与 .env.local 模板
# -----------------------------------------------------------------------------
log_info ">>> 拉取部署模板（docker-compose.local.yml / .env.local）"

GITHUB_RAW_BASE="https://raw.githubusercontent.com/ghshhf/MonkeyCode/main/backend"
COMPOSE_LOCAL_FILE="$INSTALL_DIR/docker-compose.local.yml"
ENV_LOCAL_FILE="$INSTALL_DIR/.env.local"

# 如果当前脚本目录是仓库本地 clone，则优先 cp，避免 GitHub 网络问题
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
LOCAL_BACKEND_DIR="$(cd "$SCRIPT_DIR/../backend" 2>/dev/null && pwd || true)"

fetch_file() {
  local _name="$1"
  local _dst="$2"
  if [ -n "$LOCAL_BACKEND_DIR" ] && [ -f "$LOCAL_BACKEND_DIR/$_name" ]; then
    log_info "本地模板存在，直接复制: $LOCAL_BACKEND_DIR/$_name"
    cp "$LOCAL_BACKEND_DIR/$_name" "$_dst" || die "复制 $_name 失败"
    return 0
  fi
  if command -v curl >/dev/null 2>&1; then
    curl -sSL --fail "$GITHUB_RAW_BASE/$_name" -o "$_dst" || die "从 GitHub 拉取 $_name 失败"
    return 0
  fi
  if command -v wget >/dev/null 2>&1; then
    wget -q "$GITHUB_RAW_BASE/$_name" -O "$_dst" || die "从 GitHub 拉取 $_name 失败"
    return 0
  fi
  die "未找到 curl / wget，无法下载部署模板。请先安装 curl。"
}

fetch_file "docker-compose.local.yml" "$COMPOSE_LOCAL_FILE"
fetch_file ".env.local" "$ENV_LOCAL_FILE"

log_ok "部署模板已就绪"

# -----------------------------------------------------------------------------
# 7. 检测本机局域网 IP
# -----------------------------------------------------------------------------
log_info ">>> 检测本机局域网 IP"

detect_lan_ip() {
  # 优先级：192.168.x.x → 10.x.x.x → 172.16~31.x.x → 回退 127.0.0.1
  # macOS: ifconfig / ipconfig
  # Linux: ip addr / ifconfig
  local _candidates=""

  if command -v ip >/dev/null 2>&1; then
    _candidates="$(ip -4 addr show up scope global 2>/dev/null \
      | grep -oE 'inet [0-9]+\.[0-9]+\.[0-9]+\.[0-9]+' \
      | awk '{print $2}')"
  fi
  if [ -z "$_candidates" ] && command -v ifconfig >/dev/null 2>&1; then
    _candidates="$(ifconfig 2>/dev/null \
      | grep -oE 'inet (addr:)?[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+' \
      | grep -oE '[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+')"
  fi

  for _prio in '^192\.168\.' '^10\.' '^172\.(1[6-9]|2[0-9]|3[0-1])\.'; do
    for _ip in $_candidates; do
      case "$_ip" in
        127.*|0.0.0.0) continue ;;
      esac
      if echo "$_ip" | grep -qE "$_prio" 2>/dev/null; then
        echo "$_ip"
        return 0
      fi
    done
  done

  # 兜底：任选一个非 loopback；仍无果返回 127.0.0.1
  for _ip in $_candidates; do
    case "$_ip" in
      127.*|0.0.0.0) continue ;;
    esac
    echo "$_ip"
    return 0
  done

  echo "127.0.0.1"
}

LOCAL_IP="$(detect_lan_ip)"
log_ok "检测到本机局域网 IP: $LOCAL_IP"

# -----------------------------------------------------------------------------
# 8. 交互式 / 默认值：初始管理员账号、密码
# -----------------------------------------------------------------------------
prompt_input() {
  # $1: 提示文案  $2: 默认值
  local _prompt="$1"
  local _default="$2"
  local _val=""

  if [ "$AUTO_YES" -eq 1 ]; then
    echo "$_default"
    return 0
  fi

  printf "${YELLOW}[%s] ?${NC} %s [默认: ${BOLD}%s${NC}]: " "$(date '+%Y-%m-%d %H:%M:%S')" "$_prompt" "$_default"
  read -r _val
  [ -z "$_val" ] && _val="$_default"
  echo "$_val"
}

rand_str() {
  # 生成 $1 位随机字符串（字母 + 数字）
  local _len="${1:-12}"
  if command -v openssl >/dev/null 2>&1; then
    openssl rand -base64 48 2>/dev/null | tr -dc 'A-Za-z0-9' | head -c "$_len"
    echo
  else
    LC_ALL=C awk -v l="$_len" 'BEGIN {
      s="ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"
      n=length(s)
      srand()
      out=""
      for (i=1;i<=l;i++) out=out substr(s,int(rand()*n)+1,1)
      print out
    }'
  fi
}

log_info ">>> 配置初始管理员账号"

DEFAULT_EMAIL="admin@local.mc"
DEFAULT_NAME="Local Team"
DEFAULT_PASSWORD="$(rand_str 12)"

INIT_EMAIL="$(prompt_input "管理员邮箱" "$DEFAULT_EMAIL")"
INIT_NAME="$(prompt_input "团队名称"   "$DEFAULT_NAME")"

if [ "$AUTO_YES" -eq 1 ]; then
  INIT_PASSWORD="$DEFAULT_PASSWORD"
else
  printf "${YELLOW}[%s] ?${NC} 管理员密码（留空使用随机值）: " "$(date '+%Y-%m-%d %H:%M:%S')"
  read -r INIT_PASSWORD
  [ -z "$INIT_PASSWORD" ] && INIT_PASSWORD="$DEFAULT_PASSWORD"
fi

# 生成两套强随机密码，写入 .env.local（PostgreSQL / Redis）
POSTGRES_PASSWORD="$(rand_str 20)"
REDIS_PASSWORD="$(rand_str 16)"

log_info "邮箱: $INIT_EMAIL"
log_info "团队: $INIT_NAME"
log_info "密码: $INIT_PASSWORD"

# -----------------------------------------------------------------------------
# 9. 写 .env.local
# -----------------------------------------------------------------------------
log_info ">>> 生成 $INSTALL_DIR/.env.local"

cat > "$ENV_LOCAL_FILE" <<ENV_EOF
# =====================================================================
# MonkeyCode 本地局域网部署 - 环境变量
# 由 install.sh 在 $(date '+%Y-%m-%d %H:%M:%S') 自动生成
# =====================================================================

# ---- 安装目录（volumes 的根）----
INSTALL_DIR=$INSTALL_DIR

# ---- PostgreSQL 配置 ----
POSTGRES_DB=monkeycode
POSTGRES_USER=monkeycode
POSTGRES_PASSWORD=$POSTGRES_PASSWORD

# ---- Redis 配置 ----
REDIS_PASSWORD=$REDIS_PASSWORD

# ---- 本地主机 IP（前端 base_url / 回调地址会用到）----
LOCAL_IP=$LOCAL_IP

# ---- 镜像 ----
BACKEND_IMAGE=ghcr.io/ghshhf/monkeycode/backend:latest
FRONTEND_IMAGE=ghcr.io/ghshhf/monkeycode/frontend:latest

# ---- 初始团队账号（backend 首次启动时自动创建）----
MCAI_INIT_TEAM_EMAIL=$INIT_EMAIL
MCAI_INIT_TEAM_NAME=$INIT_NAME
MCAI_INIT_TEAM_PASSWORD=$INIT_PASSWORD

# ---- 可选 Ollama 本地推理默认模型 ----
MCAI_LLM_MODEL=qwen2.5:7b
ENV_EOF

log_ok ".env.local 已写入"

# 同时把账号 / 密码保存一份为可读文本，便于用户事后查找
CREDS_FILE="$INSTALL_DIR/.credentials.txt"
cat > "$CREDS_FILE" <<CREDS_EOF
MonkeyCode 本地部署 - 首次访问信息
生成时间: $(date '+%Y-%m-%d %H:%M:%S')

Web 地址 : http://$LOCAL_IP:8080
管理员邮箱: $INIT_EMAIL
管理员密码: $INIT_PASSWORD
团队名称 : $INIT_NAME

Ollama    : http://$LOCAL_IP:11434
安装目录  : $INSTALL_DIR

⚠️  请在首次登录后尽快修改默认密码！
CREDS_EOF
chmod 600 "$CREDS_FILE" 2>/dev/null || true
log_info "访问凭据已保存到: $CREDS_FILE (权限 600)"

# -----------------------------------------------------------------------------
# 10. 用户确认（除非 --yes）
# -----------------------------------------------------------------------------
if [ "$AUTO_YES" -ne 1 ]; then
  echo
  echo "--- 部署概要 -------------------------------------------------------------"
  echo "  安装目录 : $INSTALL_DIR"
  echo "  本机 IP  : $LOCAL_IP"
  echo "  Web 访问 : http://$LOCAL_IP:8080"
  echo "  Ollama   : $([ "$NO_OLLAMA" -eq 1 ] && echo "禁用" || echo "http://$LOCAL_IP:11434")"
  echo "  管理员   : $INIT_EMAIL / $INIT_PASSWORD"
  echo "-------------------------------------------------------------------------"
  printf "${YELLOW}[%s] ?${NC} 确认开始部署？[Y/n]: " "$(date '+%Y-%m-%d %H:%M:%S')"
  read -r _confirm
  case "$_confirm" in
    ""|[Yy]|[Yy][Ee][Ss]) log_ok "开始部署..." ;;
    *) die "已取消部署" ;;
  esac
fi

# -----------------------------------------------------------------------------
# 11. 拉取镜像 + 启动服务
# -----------------------------------------------------------------------------
log_info ">>> 拉取容器镜像"

SERVICES_UP="db redis backend frontend"
if [ "$NO_OLLAMA" -ne 1 ]; then
  SERVICES_UP="db redis ollama backend frontend"
fi

cd "$INSTALL_DIR" || die "无法进入目录: $INSTALL_DIR"

# docker compose pull 可接受服务列表；失败时给出明确提示
if ! $DOCKER_COMPOSE_CMD -f docker-compose.local.yml --env-file .env.local pull $SERVICES_UP; then
  die "镜像拉取失败。请检查网络 / 镜像仓库访问，或手动 docker pull 镜像后重试。"
fi
log_ok "镜像拉取完成"

log_info ">>> 启动服务: $SERVICES_UP"

if ! $DOCKER_COMPOSE_CMD -f docker-compose.local.yml --env-file .env.local up -d $SERVICES_UP; then
  log_error "docker compose up -d 失败，正在打印最新日志以便排查..."
  $DOCKER_COMPOSE_CMD -f docker-compose.local.yml --env-file .env.local logs --tail=50 || true
  die "服务启动失败，请根据日志手动排查后重试。"
fi

log_ok "服务启动命令已发出，等待服务健康检查..."

# 简易等待：轮询 backend 健康接口，最长 180 秒
_backend_ready=0
for _i in $(seq 1 60); do
  _status="$($DOCKER_COMPOSE_CMD -f "$INSTALL_DIR/docker-compose.local.yml" \
                --env-file "$INSTALL_DIR/.env.local" ps --format json 2>/dev/null \
              | tr -d '\n' \
              | grep -oE '"Health":"[a-zA-Z]+"' | head -n1 || true)"
  if echo "$_status" | grep -q "healthy"; then
    _backend_ready=1
    break
  fi
  # 备用：直接 http 检查
  if command -v curl >/dev/null 2>&1; then
    if curl -sf "http://$LOCAL_IP:8080/" >/dev/null 2>&1; then
      _backend_ready=1
      break
    fi
  fi
  sleep 3
done

if [ "$_backend_ready" -eq 1 ]; then
  log_ok "服务已就绪"
else
  log_warn "等待超时，但服务可能仍在启动中；稍后用 docker compose ps 确认状态。"
fi

# -----------------------------------------------------------------------------
# 12. 向日葵风格 - 首次使用清单
# -----------------------------------------------------------------------------
BOX_L="======================================================================"
SUN_L="🌻  🌻  🌻"

echo
printf "${GREEN}${BOLD}%s${NC}\n" "$BOX_L"
printf "${GREEN}${BOLD}%s${NC}\n" "  部署完成！ 🎉  MonkeyCode 正在你的机器上运行。"
printf "${GREEN}${BOLD}%s${NC}\n" "$BOX_L"
echo

printf "${YELLOW}%s${NC}\n" "【1/3  🌐  访问地址】"
printf "    浏览器打开：${BOLD}http://%s:8080${NC}\n" "$LOCAL_IP"
if [ "$NO_OLLAMA" -ne 1 ]; then
  printf "    本地 Ollama：${BOLD}http://%s:11434${NC}\n" "$LOCAL_IP"
fi
echo

printf "${YELLOW}%s${NC}\n" "【2/3  🔑  管理员账号】"
printf "    邮箱    ：${BOLD}%s${NC}\n" "$INIT_EMAIL"
printf "    密码    ：${BOLD}%s${NC} ${RED}(⚠️  请登录后立即修改密码！)${NC}\n" "$INIT_PASSWORD"
printf "    团队名称：${BOLD}%s${NC}\n" "$INIT_NAME"
echo

printf "${YELLOW}%s${NC}\n" "【3/3  🚀  接下来可以做什么】"
printf "    ① 进入「设置 - 模型」，配置大模型 API Key\n"
printf "         （GLM / Kimi / MiniMax / Qwen / DeepSeek / OpenAI 兼容）\n"
if [ "$NO_OLLAMA" -ne 1 ]; then
  printf "    ② 在宿主机执行下方命令下载本地大模型：\n"
  printf "         ${BOLD}docker exec -it monkeycode-local-ollama ollama pull qwen2.5:7b${NC}\n"
fi
printf "    ③ 为企业内网伙伴分享：http://%s:8080\n" "$LOCAL_IP"
printf "    ④ 如需启用 P2P 组网 / 外网访问，参考 README 的高级部署章节\n"
echo

printf "${YELLOW}%s${NC}\n" "【常用命令速查】"
printf "    查看日志 ：${BOLD}docker compose -f $INSTALL_DIR/docker-compose.local.yml --env-file $INSTALL_DIR/.env.local logs -f backend${NC}\n"
printf "    停止服务 ：${BOLD}docker compose -f $INSTALL_DIR/docker-compose.local.yml --env-file $INSTALL_DIR/.env.local down${NC}\n"
printf "    启动服务 ：${BOLD}docker compose -f $INSTALL_DIR/docker-compose.local.yml --env-file $INSTALL_DIR/.env.local up -d${NC}\n"
echo

printf "${GREEN}${BOLD}%s${NC}\n" "$SUN_L  祝你玩得开心！有问题请在 GitHub Issues 提交反馈。"
echo

# -----------------------------------------------------------------------------
# 结束
# -----------------------------------------------------------------------------
exit 0
