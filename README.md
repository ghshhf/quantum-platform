# MonkeyCode

<p align="center">
  <img src="./frontend/public/logo-dark.png" alt="MonkeyCode" width="200" />
</p>

<p align="center">
  <a href="./LICENSE"><img src="https://img.shields.io/badge/license-AGPL--3.0-blue.svg" alt="License: AGPL-3.0" /></a>
  <a href="https://github.com/ghshhf/MonkeyCode"><img src="https://img.shields.io/badge/fork-ghshhf%2FMonkeyCode-purple" alt="Fork: ghshhf/MonkeyCode" /></a>
</p>

## 🦍 最新更新（2026-06-15）

> commit: [`0e1291bd`](https://github.com/ghshhf/MonkeyCode/commit/0e1291bd)

**backend/biz DI 依赖注入标准化重构**

将散落在 `register.go` 中的散列 `Provide`/`Invoke` 调用，统一为 `[]Module` 注册模式，降低维护成本和遗漏风险。

| 改动 | 说明 |
|------|------|
| 新增 `biz/di/di.go` | 定义 `Module` 接口（`RegisterServices` + `RegisterRoutes`） |
| 重构 `biz/register.go` | 主注册函数从 ~130 行散列调用 → 30 行 `for _, m := range modules {...}` |
| task/notify/host/user | 每个子包的 `register.go` 简化为单一 `Module` 变量 |
| 方法签名修正 | `taskhook` 的方法参数对齐 `domain.TaskHook` 接口定义 |

**文件变化**：
```
 backend/biz/register.go        | +67 / -155   ← 主入口改用 []Module
 backend/biz/task/register.go   | +29 / -45    ← 简化为 Module 变量
 backend/biz/notify/register.go | +35 / -60    ← 同上
 backend/biz/host/register.go   | +25 / -40    ← 同上
 backend/biz/user/register.go   | +23 / -35    ← 同上
 backend/biz/di/di.go           | +30 / -0     ← 新增：Module 接口定义
```

**维护收益**：新增一个 biz 子包只需 `NewModule()` 加入 `[]Module` 数组，不需要在 3 个地方分别添加 `Provide/Invoke`。

## MonkeyCode 是什么

MonkeyCode 是一款开源的**企业级 AI 开发平台**，内置了开发环境管理、AI 模型管理、AI 任务管理、项目需求管理等能力，区别于其他的 vibe coding 工具，MonkeyCode 是真正面向专业开发团队的 AI 助手。

- 你可以部署在**企业内网**，分享给研发团队使用，让你的研发团队可以方便、快捷地启动开发任务；作为研发负责人的你可以对企业内的 AI 开发流程进行统一管理。
- 所有数据保留在本地，完全由你掌控。

## 界面展示

<table>
  <tr>
    <td align="center">
      <img src="./frontend/public/monkeycode-1.png" alt="AI 任务工作台" />
      <br />
      <sub>AI 任务工作台</sub>
    </td>
    <td align="center">
      <img src="./frontend/public/monkeycode-2.png" alt="云端终端与任务执行" />
      <br />
      <sub>云端终端与任务执行</sub>
    </td>
  </tr>
  <tr>
    <td align="center">
      <img src="./frontend/public/monkeycode-3.png" alt="项目协作与文件管理" />
      <br />
      <sub>项目协作与文件管理</sub>
    </td>
    <td align="center">
      <img src="./frontend/public/monkeycode-mobile.png" alt="移动端任务与文件管理" />
      <br />
      <sub>移动端任务与文件管理</sub>
    </td>
  </tr>
</table>

## 功能与特色

你不需要自己拼工具、搭环境、来回切流程。把需求交给 MonkeyCode，它会从开发到验证一路接住，真正把 AI 编程变成可持续的工作流。

- **免费即用**：无需下载客户端，也不用折腾环境。浏览器打开、注册账号，几秒钟就能开始执行第一个 AI 开发任务。
- **云端开发环境**：不依赖本地开发机。每个任务背后都有一台真实服务器提供运行环境，编译、测试、预览都在云上完成。
- **全量主流模型**：支持配置 GLM、Kimi、MiniMax、Qwen、DeepSeek 等主流大模型，支持按任务类型切换，也能手动指定。
- **移动端原生支持**：深度适配 iOS / Android，PC 和手机数据实时同步。通勤路上也能把任务交给 Agent 继续跑。
- **完全开源**：核心代码全部公开在 GitHub。任何人都能审计、fork、二次开发，技术选型和安全策略自己掌控。
- **私有化离线部署**：对数据隐私要求高的企业和团队，可以把 MonkeyCode 独立部署到自己的内网中，数据不出本地。

## 独立部署

配置建议：

- MonkeyCode 控制台：最低 `2C / 4 GB / 40 GB`
- 开发环境宿主机：最低建议 `8C / 16 GB / 100 GB`

请参考项目内的部署脚本和配置文件进行自托管部署。

## 同类项目对比

| 对比维度 | MonkeyCode | Cursor | Claude Code | Codex |
|---|:---:|:---:|:---:|:---:|
| 在线使用 | 🟢 | 🟢 | 🟢 | 🟢 |
| 本地 IDE | 🔴 | 🟢 | 🟢 | 🟢 |
| 本地 CLI | 🔴 | 🟢 | 🟢 | 🟢 |
| 需求与 SPEC 管理 | 🟢 | 🔴 | 🔴 | 🔴 |
| 云端开发环境 | 🟢 | 🟡 | 🟡 | 🟡 |
| 代码补全 | 🔴 | 🟢 | 🔴 | 🔴 |
| PR / MR 自动代码审查 | 🟢 | 🟡 | 🟡 | 🟡 |
| 团队协作 | 🟢 | 🔴 | 🔴 | 🔴 |
| 适配国产大模型 | 🟢 | 🔴 | 🔴 | 🔴 |
| 私有化部署 | 🟢 | 🔴 | 🔴 | 🔴 |
| 开源 | 🟢 | 🔴 | 🔴 | 🔴 |

## 🚀 本地一键部署

MonkeyCode 提供 `scripts/install.sh`，一条命令即可在 Linux（x86_64 / aarch64）或 macOS（Intel / Apple Silicon）上部署一套完整的本地实例，内置 PostgreSQL、Redis、前端 Web UI 和可选本地 Ollama 推理。

**前置要求：** 已安装 [Docker 与 Docker Compose（v2 插件或独立二进制均可）](https://docs.docker.com/compose/install/)。

### 最快方式

```bash
# 方式一：从仓库 clone 后执行
git clone https://github.com/ghshhf/MonkeyCode.git
cd MonkeyCode
bash scripts/install.sh

# 方式二：直接用 curl 管道执行（需要可访问 GitHub）
curl -sSL https://raw.githubusercontent.com/ghshhf/MonkeyCode/main/scripts/install.sh | bash
```

脚本会自动：

1. 检测架构 / 系统，不兼容时报错退出
2. 检查 Docker 与 docker compose 是否可用
3. 创建 `~/.monkeycode` 作为安装目录（含 `data/` 子目录）
4. 从 GitHub 拉取 `docker-compose.local.yml` 与 `.env.local` 模板
5. 自动探测本机局域网 IP（优先 `192.168.x.x` / `10.x.x.x`）
6. 交互式设置初始管理员邮箱 / 密码（留空自动生成强随机值）
7. `docker compose pull` + `docker compose up -d`，并等待健康检查
8. 输出向日葵风格的「首次使用清单」，包括 Web 访问地址、管理员账号、下一步建议

### 命令行参数

| 参数 | 说明 |
| --- | --- |
| `--dir /path/to/install` | 自定义安装目录（默认 `~/.monkeycode`） |
| `--no-ollama` | 不启动本地 Ollama 服务，仅拉起核心 4 个容器 |
| `--yes` / `-y` | 跳过所有交互提示，直接使用默认值 |

示例：

```bash
# 指定安装目录 + 全自动（适合在服务器上后台跑）
bash scripts/install.sh --dir /opt/monkeycode --yes

# 仅部署核心服务，不启动本地 Ollama
bash scripts/install.sh --no-ollama
```

### 部署产物与后续管理

脚本执行完毕后，在安装目录（默认 `~/.monkeycode`）中可找到：

- `docker-compose.local.yml` — 本机构建使用的 compose 模板
- `.env.local` — 包含 `POSTGRES_PASSWORD` / `REDIS_PASSWORD` / `MCAI_INIT_TEAM_*` 等环境变量
- `.credentials.txt` — 首访信息（权限 `600`，登录后请尽快修改密码）
- `data/postgres` / `data/redis` / `data/ollama` / `data/uploads` — 持久化数据

日常运维：

```bash
# 查看后端日志
docker compose -f ~/.monkeycode/docker-compose.local.yml --env-file ~/.monkeycode/.env.local logs -f backend

# 停止 / 启动
docker compose -f ~/.monkeycode/docker-compose.local.yml --env-file ~/.monkeycode/.env.local down
docker compose -f ~/.monkeycode/docker-compose.local.yml --env-file ~/.monkeycode/.env.local up -d

# 下载本地大模型（需要已启用 Ollama 服务）
docker exec -it monkeycode-local-ollama ollama pull qwen2.5:7b
```

### 首次登录

- Web 控制台：`http://<本机局域网 IP>:8080`
- 管理员邮箱 / 密码：见脚本输出或 `~/.monkeycode/.credentials.txt`
- 登录后建议第一时间在个人设置中修改密码，并前往「设置 - 模型」配置大模型 API Key

---

## Issues 与反馈

欢迎通过 [GitHub Issues](https://github.com/ghshhf/MonkeyCode/issues) 提交问题和反馈。

## License

MonkeyCode 使用 [GNU Affero General Public License v3.0](./LICENSE) 开源。
