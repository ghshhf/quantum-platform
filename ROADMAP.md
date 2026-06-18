# Quantum Platform — 路线图

> 一切产生数据的，都可以交互。
> 不碰终端，只碰数据，把一切交换量化。

---

## ✅ 第一版（v0.1）— 已完成

### 核心能力

| 能力 | 说明 |
|------|------|
| **Entity 接口体系** | Profile() + Match() + Execute() 统一交互标准 |
| **Bridge 调度引擎** | 多 Entity 并行调度、打分选择、LLM 整合 |
| **TerminalEntity** | 桌面终端智能体：读写文件、执行命令、列目录 |
| **Chat UI** | DeepSeek 风格对话界面：消息列表、流式输出、文件拖拽、模型切换 |
| **p2p 网络层** | TCP/WebSocket 传输、DHT 发现、PRP 路由、心跳重连 |
| **CI/CD 流水线** | Go 后端 + 前端构建、Release 多平台发布、golangci-lint |
| **测试覆盖** | Transport 14 用例、Router 16 用例、TerminalEntity 30 用例 |
| **MiMoCode 插件** | 桌面端 5 个 tool（连接/读文件/写文件/执行命令/拖拽处理） |

### 启动方式

```bash
# 独立模式（无需数据库，推荐）
双击 E:\quantum-build\一键启动.bat
# 浏览器访问 http://localhost:8889

# 完整模式（需 PostgreSQL + Redis）
cd backend && go run ./cmd/server
```

---

## 🚀 第二版（v0.2）— Android APP 版

### 核心理念

**网页端受限于浏览器沙箱，无法真正做到"一切数据交互"。**
Android 端可以：

```
手机用户
   │
   ├── 读取本地文件（存储权限）
   ├── 拍照/录音（传感器）
   ├── 剪贴板交互
   ├── 文件拖拽/分享
   ├── 通知推送
   └── p2p 连接桌面端 ↔ 手机端 → 协同工作
```

### 架构设计

```
┌─────────────────────────┐
│   Android APP           │
│  ┌───────────────────┐  │
│  │ Chat UI (WebView) │  │  ← 复用现有前端代码
│  ├───────────────────┤  │
│  │ AndroidEntity     │  │  ← 手机专属 Entity
│  │ ├── 文件操作      │  │
│  │ ├── 相机/媒体     │  │
│  │ └── 传感器数据    │  │
│  ├───────────────────┤  │
│  │ WebSocket Client  │  │  ← 通过 p2p 连桌面端
│  └───────────────────┘  │
└──────────┬──────────────┘
           │ WebSocket / QUIC
           ▼
┌─────────────────────────┐
│   桌面端 Bridge          │
│   ├── TerminalEntity     │  ← 桌面文件/命令
│   ├── LLMEntity          │  ← AI 对话
│   ├── AndroidEntity      │  ← 手机端能力
│   └── WebpageEntity      │  ← 未来：联网搜索
└─────────────────────────┘
```

### 小任务分解

| # | 任务 | 预估 |
|---|------|------|
| 1 | **AndroidEntity** — 实现 Entity 接口，手机端能力抽象 | 2h |
| 2 | **Android 项目脚手架** — Kotlin/Compose + WebSocket Client | 3h |
| 3 | **WebView 嵌入 Chat UI** — 复用现有前端，通过 JS Bridge 通信 | 2h |
| 4 | **文件操作能力** — 读取本地文件、分享接收、存储写入 | 2h |
| 5 | **拍照/录音** — 相机权限、媒体文件转 Entity 数据 | 1.5h |
| 6 | **p2p 连接** — 手机端通过 WebSocket/QUIC 连桌面或直连其他手机 | 3h |
| 7 | **通知推送** — 后台 Entity 消息推送到手机通知栏 | 1h |
| 8 | **剪贴板同步** — 手机/桌面剪贴板互通 | 1h |
| 9 | **基础 UI 完善** — 本地设置页、连接管理、主题适配 | 2h |
| 10 | **端到端测试** — 手机 ↔ 桌面 数据互通验证 | 1h |

### 关键技术选择

| 技术 | 选择理由 |
|------|---------|
| **Kotlin + Jetpack Compose** | 现代 Android 开发，声明式 UI |
| **WebSocket (gorilla/websocket)** | 与现有 p2p transport_ws.go 兼容 |
| **WebView + JS Bridge** | 直接复用已构建的 React 前端 |
| **Material 3** | 与现有 shadcn/ui 风格接近 |
| **AGP 8.x** | 最新 Android Gradle Plugin |

### 进度追踪

> 具体进度将在此更新。

---

## 🔮 未来版本规划

### v0.3 — 联网搜索
- WebpageEntity（网页抓取 + 信息提取）
- 联网搜索集成（Search API / 爬虫）

### v0.4 — 多端协同
- 桌面 ↔ 手机 ↔ 平板 数据同步
- real-time 协作编辑
- 文件双向同步

### v0.5 — 插件市场
- Entity 插件商店
- 社区贡献的 Entity 类型
- 可视化 Entity 组装

---

## 理念

> 你是量子平台的使用者，不是平台的管理者。
> 你只管说话、拖拽、交互——背后哪个 Entity 在干活，你不用管。

---

*最后更新: 2026-06-19*
