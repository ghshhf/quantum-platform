# 量子平台 · Quantum Platform

<p align="center">
  <img src="./frontend/public/logo-dark.png" alt="量子平台" width="220" />
</p>

<p align="center">
  <strong>依托量子超距交互概念 · 无视格式/接口差异 · 任何产生数据交互的主体都能瞬间双向调度</strong>
</p>

<p align="center">
  <a href="./LICENSE"><img src="https://img.shields.io/badge/license-AGPL--3.0-blue.svg" alt="License: AGPL-3.0" /></a>
  <a href="https://github.com/ghshhf/quantum-platform"><img src="https://img.shields.io/badge/GitHub-ghshhf%2Fquantum--platform-blueviolet?logo=github" alt="GitHub Repo" /></a>
  <a href="https://github.com/ghshhf/quantum-platform/releases"><img src="https://img.shields.io/badge/releases-latest-brightgreen" alt="Releases" /></a>
</p>

---

## ⚡ 一句话介绍

**量子平台** 是一款开源的企业级 AI 开发与数据交互平台。

它把「AI 模型、开发环境、终端、API、文档、你的私有数据」全部统一抽象成可被智能调度的**量子节点（Entity）**，由一个中央**网桥（Bridge）**根据你的提问自动选择最相关的节点、并发执行、把结果融合成自然语言回答你——这就是「**量子超距交互**」：不管数据源是谁、格式是什么、接口怎么拼，都能瞬间双向打通。

> 以前：你要知道「我要访问哪个 API、怎么写调用、怎么拼参数、结果怎么解析」
>
> 现在：你只需要**提问**，量子平台 帮你做完中间的一切。

---

## 🌌 核心设计思想：Entity + Bridge 两级抽象

```
        ┌─────────────────────────── 量子平台 / Quantum Platform ───────────────────────────┐
        │                                                                                    │
用户提问 │  Bridge（网桥 / 中央调度器）                                                       │
 ───────►  ① 理解意图    ② 给所有 Entity 打分匹配 Top-N    ③ 并发调度    ④ 多源信息聚合 ◂──► 自然语言回答
        │                        │     │     │     │     │                                  │
        └────────────────────────┼─────┼─────┼─────┼─────┼──────────────────────────────────┘
                                 │     │     │     │     │
          ┌──────────────────┐   │  ┌──┴──┐  │  ┌──┴──┐  │  ┌──────────────────┐
          │  Entity（量子节点）│   │  │LLM │  │  │API  │  │  │  Entity         │
          │                    │   │  │Entity│  │Entity│  │  │                  │
          │ 文档 / Wiki        │   │  │     │  │     │  │  │ 终端 / SSH         │
          │ 代码仓库 / Git     │   │  │(DeepSeek│   │  │  │数据库 / SQL         │
          │ 本地文件           │   │  │  Kimi │   │  │  │Web 页面              │
          │ P2P 共享资源       │   │  │ Groq  │  │     │  │任何能产生数据交互的 │
          └────────────────────┘   │  │ Ollama│  │     │  │东西都是 Entity       │
                                  │  │ 等等) │  │     │  │                        │
                                  │  │      │   │     │  │                        │
                                  └──┬──────┘   └─────┘   └───────────────────────┘
                                     │
                              所有节点都是「平等的量子节点」
                          Bridge 根据问题语义自动选择、合并、回答
```

### Entity（量子节点）
任何能产生数据交互的实体都是一个 **Entity**。它只需要实现 3 个方法：

```go
type Entity interface {
    Profile() EntityProfile       // 描述自己是谁、擅长什么、能访问什么数据
    Match(question string) float64  // 对给定问题打分，0~1，决定是否被选中
    Execute(ctx context.Context, q EntityQuery) EntityResult  // 执行 → 返回结构化片段
}
```

你可以把**任意**东西封装成 Entity：文档、Git 仓库、数据库、内网 API、终端命令、甚至另一个 量子平台 实例。

### Bridge（网桥）
Bridge 是中央调度器。收到用户提问时：

1. 遍历所有已注册的 Entity，按 `Match()` 分数排序
2. 选取 Top-N（默认 3~5）并发执行 `Execute()`
3. 如果存在 LLM Entity，用它把多个源的结果合成为自然语言回答；否则返回结构化的「来源+内容」列表
4. 始终保留来源标注，可审计可回溯

### LLM Entity（AI 节点）
LLM Entity 是一种特殊的 Entity：它不直接产生数据，而是**把其它 Entity 产生的数据片段变成流畅的自然语言回答**。

我们在平台内预置了多家免费/开源 AI 接口的**目录**：

| Provider | 接入方式 | 建议场景 |
|---------|---------|---------|
| DeepSeek | API Key | 代码生成、推理 |
| 硅基流动 SiliconFlow | API Key | 开源模型、多种尺寸 |
| Groq | API Key | 高速推理 |
| 智谱 GLM | API Key | 中文优化 |
| 阿里 Qwen | API Key | 多模态、企业场景 |
| 字节火山方舟 | API Key | 国产模型 |
| Ollama | 本地（免 Key） | 完全离线、私有部署 |

用户只需在前端选一个、填入 API Key，就能开始使用。**不需要自己学习每个平台的 API 文档。**

---

## 🎯 功能与特色

| 能力 | 说明 |
|-----|------|
| **免费即用** | 浏览器打开即可，无需下载客户端。接入 7+ 家主流大模型，零门槛启动 AI 开发 |
| **云端开发环境** | 每个任务背后都是真实容器。编译、测试、预览都在云上完成，不依赖本机 |
| **全量国产大模型** | 支持 GLM、Kimi、MiniMax、Qwen、DeepSeek 等，按任务类型自动/手动切换 |
| **Entity × Bridge 调度** | 任意数据源都能被 Bridge 识别、调度、回答。真正的「问问题 → 得答案」 |
| **移动端原生支持** | iOS / Android 深度适配，PC 和手机数据实时同步 |
| **P2P 组网（可选）** | 多台 量子平台 实例可以互相发现、共享 Entity，形成真正的分布式智能体网络 |
| **完全开源** | 核心代码全部公开。任何人都能审计、fork、二次开发 |
| **私有化离线部署** | 企业内网一键部署，数据不出本地。配合 Ollama 本地模型可实现完全离线运行 |

---

## 🖥 界面预览

<table>
  <tr>
    <td align="center">
      <img src="./frontend/public/quantum-platform-1.png" alt="AI 任务工作台" width="320" />
      <br />
      <sub>AI 任务工作台</sub>
    </td>
    <td align="center">
      <img src="./frontend/public/quantum-platform-2.png" alt="云端终端与任务执行" width="320" />
      <br />
      <sub>云端终端与任务执行</sub>
    </td>
  </tr>
  <tr>
    <td align="center">
      <img src="./frontend/public/quantum-platform-3.png" alt="项目协作与文件管理" width="320" />
      <br />
      <sub>项目协作与文件管理</sub>
    </td>
    <td align="center">
      <img src="./frontend/public/quantum-platform-mobile.png" alt="移动端任务与文件管理" width="320" />
      <br />
      <sub>移动端任务与文件管理</sub>
    </td>
  </tr>
</table>

---

## 🧪 技术栈

| 层级 | 技术 |
|-----|------|
| **后端** | Go 1.25 · gin · gRPC · ent ORM · PostgreSQL · Redis |
| **前端** | React · Next.js · TypeScript · Tailwind CSS |
| **移动端** | React Native · Expo |
| **桌面端** | Electron |
| **AI 接入** | 统一 OpenAI-compatible Protocol 适配层 |
| **部署** | Docker Compose · Linux / macOS · x86_64 / aarch64 |

---

## 🚀 本地一键部署

### 前置要求

已安装 [Docker 与 Docker Compose（v2 插件或独立二进制均可）](https://docs.docker.com/compose/install/)。

### 最快方式

```bash
# 方式一：从仓库 clone 后执行
git clone https://github.com/ghshhf/quantum-platform.git
cd quantum-platform
bash scripts/install.sh

# 方式二：直接用 curl 管道执行（需要可访问 GitHub）
curl -sSL https://raw.githubusercontent.com/ghshhf/quantum-platform/main/scripts/install.sh | bash
```

**脚本会自动完成以下 8 步：**

1. 检测架构 / 系统，不兼容时报错退出
2. 检查 Docker 与 docker compose 是否可用
3. 创建 `~/.quantum-platform` 作为安装目录（含 `data/` 子目录）
4. 从 GitHub 拉取 `docker-compose.local.yml` 与 `.env.local` 模板
5. 自动探测本机局域网 IP（优先 `192.168.x.x` / `10.x.x.x`）
6. 交互式设置初始管理员邮箱 / 密码（留空自动生成强随机值）
7. `docker compose pull` + `docker compose up -d`，并等待健康检查
8. 输出「首次使用清单」，包括 Web 访问地址、管理员账号、下一步建议

### 命令行参数

| 参数 | 说明 |
| --- | --- |
| `--dir /path/to/install` | 自定义安装目录（默认 `~/.quantum-platform`） |
| `--no-ollama` | 不启动本地 Ollama 服务，仅拉起核心 4 个容器 |
| `--yes` / `-y` | 跳过所有交互提示，直接使用默认值（适合服务器后台部署） |

**示例：**

```bash
# 指定安装目录 + 全自动（适合在服务器上后台跑）
bash scripts/install.sh --dir /opt/quantum-platform --yes

# 仅部署核心服务，不启动本地 Ollama
bash scripts/install.sh --no-ollama
```

### 部署产物与后续管理

脚本执行完毕后，在安装目录（默认 `~/.quantum-platform`）中可找到：

- `docker-compose.local.yml` — 本机构建使用的 compose 模板
- `.env.local` — 包含 `POSTGRES_PASSWORD` / `REDIS_PASSWORD` / `QUANTUMPLATFORM_INIT_TEAM_*` 等环境变量
- `.credentials.txt` — 首访信息（权限 `600`，登录后请尽快修改密码）
- `data/postgres` / `data/redis` / `data/ollama` / `data/uploads` — 持久化数据

**日常运维：**

```bash
# 查看后端日志
docker compose -f ~/.quantum-platform/docker-compose.local.yml --env-file ~/.quantum-platform/.env.local logs -f backend

# 停止 / 启动
docker compose -f ~/.quantum-platform/docker-compose.local.yml --env-file ~/.quantum-platform/.env.local down
docker compose -f ~/.quantum-platform/docker-compose.local.yml --env-file ~/.quantum-platform/.env.local up -d

# 下载本地大模型（需要已启用 Ollama 服务）
docker exec -it quantum-platform-local-ollama ollama pull qwen2.5:7b
```

### 首次登录

- **Web 控制台**：`http://<本机局域网 IP>:8080`
- **管理员邮箱 / 密码**：见脚本输出或 `~/.quantum-platform/.credentials.txt`
- 登录后建议第一时间在个人设置中修改密码，并前往「设置 → 模型」配置大模型 API Key

---

## ⚙️ 硬件与部署建议

| 角色 | 最低配置 | 推荐配置 |
|-----|---------|---------|
| **量子平台 控制台（Web + API）** | 2C / 4 GB / 40 GB | 4C / 8 GB / 80 GB |
| **开发环境宿主机（容器池）** | 8C / 16 GB / 100 GB | 16C / 32 GB / 200 GB |
| **完全离线 + 本地 Ollama（7B 模型）** | 16C / 32 GB + 8 GB GPU VRAM | 24C / 64 GB + 16 GB GPU VRAM |

对于**纯在线 AI + 中小团队**：一台 4C/8GB 的普通服务器已足够运行整个平台 + 2~3 个并发开发任务。

---

## 🆚 同类项目对比

| 对比维度 | 量子平台 | Cursor | Claude Code |
|---------|:---:|:---:|:---:|
| 在线使用 | 🟢 | 🟢 | 🟢 |
| 本地 IDE | 🔴 | 🟢 | 🟢 |
| 需求与 SPEC 管理 | 🟢 | 🔴 | 🔴 |
| 云端开发环境 | 🟢 | 🟡 | 🟡 |
| PR / MR 自动代码审查 | 🟢 | 🟡 | 🟡 |
| 团队协作与权限 | 🟢 | 🔴 | 🔴 |
| 适配国产大模型 | 🟢 | 🔴 | 🔴 |
| 免费 AI 接口聚合 | 🟢 | 🔴 | 🔴 |
| 私有化部署 | 🟢 | 🔴 | 🔴 |
| 开源 | 🟢 | 🔴 | 🔴 |

区别于其他「代码补全工具」，量子平台 把 AI 编程提升到**项目级工作流**：从需求理解、SPEC 生成、代码实现、测试预览、PR 审查，整条链路都能被 AI Agent 接管。

---

## 🛠 开发模式（从源码构建）

```bash
# 1. 克隆源码
git clone https://github.com/ghshhf/quantum-platform.git
cd quantum-platform

# 2. 启动依赖（PostgreSQL + Redis）
cd frontend/docker
docker compose up -d

# 3. 启动后端（需要 Go 1.25）
cd ../../backend
go mod download
go run cmd/server/main.go

# 4. 启动前端（需要 Node.js 18+）
cd ../frontend
npm install
npm run dev

# 5. 浏览器访问 http://localhost:3000
```

---

## 📖 目录结构

```
quantum-platform/
├── backend/                    # Go 后端
│   ├── biz/                    # 业务逻辑层（task / user / team / notify / host ...）
│   ├── cmd/server/             # 启动入口
│   ├── config/                 # 配置与 DI 注册
│   ├── db/                     # 数据库模型（ent）
│   ├── domain/                 # 领域模型
│   ├── pkg/                    # 公共工具包
│   │   └── quantum/            # 量子平台核心: Entity / Bridge / FreeProvider / LLM
│   └── tests/                  # 集成测试
├── frontend/                   # Next.js Web 控制台
│   ├── src/pages/              # 路由页面
│   ├── src/components/         # UI 组件
│   └── public/                 # 静态资源
├── mobile/                     # React Native 移动端
├── desktop/                    # Electron 桌面端
├── scripts/                    # 一键部署脚本
└── LICENSE                     # AGPL-3.0
```

---

## 🌱 下一步方向（Roadmap）

- [ ] **Entity SDK（TypeScript / Python）**：让用户用 10 行代码就能把自家系统封装成量子节点
- [ ] **P2P 节点自动发现**：多台 量子平台 互相发现、共享 Entity，形成分布式智能体网络
- [ ] **自然语言 → SQL / API 自动翻译**：用户说「把近 7 天的订单额按地区聚合」，Bridge 自动走数据库 Entity
- [ ] **浏览器插件**：把网页内容变成一个临时 Entity，直接提问
- [ ] **更多免费 AI 目录**：持续补充国内外可用的免费 / 开源 AI 接口
- [ ] **性能优化**：Entity 选择的向量加速、大规模并发任务池、冷启动优化

---

## 🐛 Issues 与反馈

欢迎通过 [GitHub Issues](https://github.com/ghshhf/quantum-platform/issues) 提交问题和反馈。

---

## 📄 License

量子平台 使用 [GNU Affero General Public License v3.0](./LICENSE) 开源。
