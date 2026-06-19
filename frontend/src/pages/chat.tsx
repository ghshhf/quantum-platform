// ─── 量子平台 Chat 对话页面 ─────────────────────────────────────────────────
//
// 功能：
//   类似 DeepSeek 网页版的对话界面 + 文件拖拽 + 模型切换
//   所有交互通过 Bridge API 统一调度背后的 Entity
//
// 路由: /chat

import { useState, useRef, useEffect, useCallback } from "react";
import { Button } from "@/components/ui/button";
import { Textarea } from "@/components/ui/textarea";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Avatar, AvatarFallback } from "@/components/ui/avatar";
import { Badge } from "@/components/ui/badge";
import {
  IconSend,
  IconPaperclip,
  IconX,
  IconRobot,
  IconUser,
  IconTrash,
  IconSun,
  IconMoon,
  IconRefresh,
} from "@tabler/icons-react";
import { useTheme } from "@/components/theme-provider";

// ─── 类型定义 ──────────────────────────────────────────────────────────────

interface ChatMessage {
  id: string;
  role: "user" | "assistant";
  content: string;
  files?: FileInfo[];
  timestamp: number;
}

interface FileInfo {
  name: string;
  size: number;
  type: string;
  content?: string; // 读取后的文本内容
}

interface ModelOption {
  id: string;
  label: string;
  provider: string;
}

const MODELS: ModelOption[] = [
  { id: "deepseek-chat", label: "DeepSeek V3", provider: "deepseek" },
  { id: "deepseek-reasoner", label: "DeepSeek R1", provider: "deepseek" },
  { id: "gpt-4o", label: "GPT-4o", provider: "openai" },
  { id: "claude-3.5-sonnet", label: "Claude 3.5 Sonnet", provider: "anthropic" },
  { id: "auto", label: "Auto（自动选择）", provider: "" },
];

// ─── 组件 ──────────────────────────────────────────────────────────────────

function ChatPage() {
  const { theme, setTheme } = useTheme();
  const [messages, setMessages] = useState<ChatMessage[]>([
    {
      id: "welcome",
      role: "assistant",
      content:
        "你好！我是量子平台助手。\n\n我可以帮你：\n- 💬 **对话聊天** — 问任何问题\n- 📁 **读取文件** — 拖拽文件进来，我帮你分析\n- 💻 **执行命令** — 通过终端操作本地电脑\n- 🔍 **联网搜索** — 查找最新信息\n\n直接输入问题开始吧！",
      timestamp: Date.now(),
    },
  ]);
  const [input, setInput] = useState("");
  const [isLoading, setIsLoading] = useState(false);
  const [files, setFiles] = useState<FileInfo[]>([]);
  const [model, setModel] = useState("auto");
  const [isStreaming, setIsStreaming] = useState(false);
  const [streamContent, setStreamContent] = useState("");

  const messagesEndRef = useRef<HTMLDivElement>(null);
  const fileInputRef = useRef<HTMLInputElement>(null);
  const textareaRef = useRef<HTMLTextAreaElement>(null);

  // 自动滚动到底部
  const scrollToBottom = useCallback(() => {
    messagesEndRef.current?.scrollIntoView({ behavior: "smooth" });
  }, []);

  useEffect(() => {
    scrollToBottom();
  }, [messages, streamContent, scrollToBottom]);

  // 自动调整 textarea 高度
  useEffect(() => {
    if (textareaRef.current) {
      textareaRef.current.style.height = "auto";
      textareaRef.current.style.height =
        Math.min(textareaRef.current.scrollHeight, 200) + "px";
    }
  }, [input]);

  // ── 发送消息 ─────────────────────────────────────────────────────────
  const sendMessage = useCallback(async () => {
    const text = input.trim();
    if ((!text && files.length === 0) || isLoading) return;

    // 构造用户消息
    let userContent = text;
    const attachedFiles = [...files];

    // 如果有文件，把文件内容拼到消息里
    if (attachedFiles.length > 0) {
      const fileParts = await Promise.all(
        attachedFiles.map(async (f) => {
          // 如果没读取内容，尝试读取
          if (!f.content) {
            // 文件内容已在拖拽时读取
          }
          return `[文件: ${f.name} (${formatFileSize(f.size)})]\n\`\`\`\n${f.content || "(二进制文件，无法直接显示)"}\n\`\`\``;
        })
      );
      userContent =
        (text ? text + "\n\n" : "") +
        "我上传了以下文件，请帮我分析：\n\n" +
        fileParts.join("\n\n");
    }

    const userMsg: ChatMessage = {
      id: `user-${Date.now()}`,
      role: "user",
      content: text,
      files: attachedFiles,
      timestamp: Date.now(),
    };

    setMessages((prev) => [...prev, userMsg]);
    setInput("");
    setFiles([]);
    setIsLoading(true);
    setIsStreaming(true);
    setStreamContent("");

    // 模拟流式输出（后续接入 Bridge API 替换这里）
    const assistantId = `assistant-${Date.now()}`;
    let fullContent = "";

    try {
      // 通过 Bridge API 发送
      // TODO: 替换为真实的 apiRequest 调用
      const resp = await mockBridgeAsk(userContent, model);

      // 模拟流式输出
      const words = resp.split(/(?<=\s)/);
      for (let i = 0; i < words.length; i++) {
        await new Promise((r) => setTimeout(r, 10));
        fullContent += words[i];
        setStreamContent(fullContent);
      }
    } catch (err) {
      fullContent = `❌ 请求失败：${err}`;
      setStreamContent(fullContent);
    }

    setIsStreaming(false);
    setIsLoading(false);

    setMessages((prev) => [
      ...prev,
      {
        id: assistantId,
        role: "assistant",
        content: fullContent,
        timestamp: Date.now(),
      },
    ]);
    setStreamContent("");
  }, [input, files, isLoading, model]);

  // ── 文件处理 ─────────────────────────────────────────────────────────
  const handleFileDrop = useCallback(async (e: React.DragEvent) => {
    e.preventDefault();
    const droppedFiles = Array.from(e.dataTransfer.files);
    await processFiles(droppedFiles);
  }, []);

  const handleFileSelect = useCallback(
    async (e: React.ChangeEvent<HTMLInputElement>) => {
      const selectedFiles = Array.from(e.target.files || []);
      await processFiles(selectedFiles);
    },
    []
  );

  const processFiles = async (fileList: File[]) => {
    const newFiles: FileInfo[] = [];
    for (const f of fileList) {
      const info: FileInfo = {
        name: f.name,
        size: f.size,
        type: f.type,
      };
      // 尝试读取文本文件
      if (
        f.size < 1024 * 1024 &&
        (f.type.startsWith("text/") ||
          f.name.endsWith(".txt") ||
          f.name.endsWith(".md") ||
          f.name.endsWith(".json") ||
          f.name.endsWith(".ts") ||
          f.name.endsWith(".tsx") ||
          f.name.endsWith(".js") ||
          f.name.endsWith(".py") ||
          f.name.endsWith(".go") ||
          f.name.endsWith(".css") ||
          f.name.endsWith(".html"))
      ) {
        info.content = await f.text();
      }
      newFiles.push(info);
    }
    setFiles((prev) => [...prev, ...newFiles]);
  };

  const removeFile = useCallback((index: number) => {
    setFiles((prev) => prev.filter((_, i) => i !== index));
  }, []);

  // ── 快捷键 ───────────────────────────────────────────────────────────
  const handleKeyDown = useCallback(
    (e: React.KeyboardEvent) => {
      if (e.key === "Enter" && !e.shiftKey) {
        e.preventDefault();
        sendMessage();
      }
    },
    [sendMessage]
  );

  // ── 清空对话 ─────────────────────────────────────────────────────────
  const clearChat = useCallback(() => {
    setMessages([
      {
        id: `welcome-${Date.now()}`,
        role: "assistant",
        content: "对话已清空，有什么我可以帮你的？",
        timestamp: Date.now(),
      },
    ]);
  }, []);

  // ── 渲染消息 ─────────────────────────────────────────────────────────
  const renderMessage = (msg: ChatMessage) => {
    const isUser = msg.role === "user";
    return (
      <div key={msg.id} className={`flex gap-3 mb-6 ${isUser ? "flex-row-reverse" : ""}`}>
        <Avatar className="size-8 shrink-0">
          <AvatarFallback className={isUser ? "bg-primary text-primary-foreground" : "bg-muted"}>
            {isUser ? <IconUser className="size-4" /> : <IconRobot className="size-4" />}
          </AvatarFallback>
        </Avatar>

        <div className={`max-w-[80%] ${isUser ? "items-end" : "items-start"}`}>
          {/* 文件预览 */}
          {msg.files && msg.files.length > 0 && (
            <div className="flex flex-wrap gap-2 mb-2">
              {msg.files.map((f, i) => (
                <Badge key={i} variant="secondary" className="text-xs">
                  {f.name} ({formatFileSize(f.size)})
                </Badge>
              ))}
            </div>
          )}

          {/* 消息内容 */}
          <div
            className={`rounded-lg px-4 py-2.5 text-sm leading-relaxed whitespace-pre-wrap ${
              isUser
                ? "bg-primary text-primary-foreground"
                : "bg-muted"
            }`}
          >
            {renderContent(msg.content)}
          </div>

          {/* 时间戳 */}
          <div className="text-xs text-muted-foreground mt-1 px-1">
            {new Date(msg.timestamp).toLocaleTimeString("zh-CN", {
              hour: "2-digit",
              minute: "2-digit",
            })}
          </div>
        </div>
      </div>
    );
  };

  return (
    <div className="flex flex-col h-screen bg-background">
      {/* ── Header ─────────────────────────────────────── */}
      <header className="flex items-center justify-between px-4 py-3 border-b shrink-0">
        <div className="flex items-center gap-3">
          <h1 className="text-lg font-semibold">量子平台</h1>
          <Badge variant="outline" className="text-xs">
            Multi-Agent
          </Badge>
        </div>

        <div className="flex items-center gap-2">
          {/* 模型选择器 */}
          <Select value={model} onValueChange={setModel}>
            <SelectTrigger className="w-[180px] h-8 text-xs">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {MODELS.map((m) => (
                <SelectItem key={m.id} value={m.id} className="text-xs">
                  {m.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>

          {/* 主题切换 */}
          <Button
            variant="ghost"
            size="icon"
            className="size-8"
            onClick={() => setTheme(theme === "dark" ? "light" : "dark")}
          >
            {theme === "dark" ? <IconSun className="size-4" /> : <IconMoon className="size-4" />}
          </Button>

          {/* 清空对话 */}
          <Button variant="ghost" size="icon" className="size-8" onClick={clearChat}>
            <IconTrash className="size-4" />
          </Button>
        </div>
      </header>

      {/* ── Messages ───────────────────────────────────── */}
      <ScrollArea className="flex-1 px-4 py-6">
        <div className="max-w-3xl mx-auto">
          {messages.map(renderMessage)}

          {/* 流式输出中 */}
          {isStreaming && streamContent && (
            <div className="flex gap-3 mb-6">
              <Avatar className="size-8 shrink-0">
                <AvatarFallback className="bg-muted">
                  <IconRobot className="size-4" />
                </AvatarFallback>
              </Avatar>
              <div className="max-w-[80%]">
                <div className="rounded-lg px-4 py-2.5 text-sm leading-relaxed whitespace-pre-wrap bg-muted">
                  {renderContent(streamContent)}
                  <span className="inline-block w-2 h-4 bg-primary ml-0.5 animate-pulse" />
                </div>
              </div>
            </div>
          )}

          <div ref={messagesEndRef} />
        </div>
      </ScrollArea>

      {/* ── File preview bar ────────────────────────────── */}
      {files.length > 0 && (
        <div className="px-4 py-2 border-t shrink-0">
          <div className="max-w-3xl mx-auto flex flex-wrap gap-2">
            {files.map((f, i) => (
              <Badge key={i} variant="secondary" className="flex items-center gap-1 pr-1">
                {f.name}
                <button
                  onClick={() => removeFile(i)}
                  className="ml-1 hover:text-destructive"
                >
                  <IconX className="size-3" />
                </button>
              </Badge>
            ))}
          </div>
        </div>
      )}

      {/* ── Input area ──────────────────────────────────── */}
      <div className="px-4 py-3 border-t shrink-0">
        <div className="max-w-3xl mx-auto">
          <div
            className="relative flex items-end gap-2 bg-muted/50 rounded-lg border p-2"
            onDragOver={(e) => e.preventDefault()}
            onDrop={handleFileDrop}
          >
            {/* 文件上传按钮 */}
            <Button
              variant="ghost"
              size="icon"
              className="size-8 shrink-0"
              onClick={() => fileInputRef.current?.click()}
              disabled={isLoading}
            >
              <IconPaperclip className="size-4" />
            </Button>
            <input
              ref={fileInputRef}
              type="file"
              multiple
              className="hidden"
              onChange={handleFileSelect}
            />

            {/* 输入框 */}
            <Textarea
              ref={textareaRef}
              value={input}
              onChange={(e) => setInput(e.target.value)}
              onKeyDown={handleKeyDown}
              placeholder={
                files.length > 0
                  ? `已选择 ${files.length} 个文件，输入你的问题...`
                  : "输入你的问题... (Enter 发送, Shift+Enter 换行)"
              }
              className="min-h-[40px] max-h-[200px] resize-none border-0 bg-transparent focus-visible:ring-0 text-sm"
              rows={1}
              disabled={isLoading}
            />

            {/* 发送按钮 */}
            <Button
              size="icon"
              className="size-8 shrink-0"
              onClick={sendMessage}
              disabled={isLoading || (!input.trim() && files.length === 0)}
            >
              {isLoading ? (
                <IconRefresh className="size-4 animate-spin" />
              ) : (
                <IconSend className="size-4" />
              )}
            </Button>
          </div>

          <p className="text-xs text-muted-foreground text-center mt-2">
            量子平台 Multi-Agent — 支持对话、文件分析、终端操作
          </p>
        </div>
      </div>
    </div>
  );
}

// ─── 工具函数 ───────────────────────────────────────────────────────────────

function formatFileSize(bytes: number): string {
  if (bytes < 1024) return bytes + " B";
  if (bytes < 1024 * 1024) return (bytes / 1024).toFixed(1) + " KB";
  return (bytes / (1024 * 1024)).toFixed(1) + " MB";
}

// 简单的 markdown 渲染（后续替换为 react-markdown）
function renderContent(content: string): React.ReactNode {
  // 简单处理：把 ```code``` 和 **bold** 显示出来
  // TODO: 使用 react-markdown + rehype-highlight
  const parts = content.split(/(```[\s\S]*?```)/g);
  return parts.map((part, i) => {
    if (part.startsWith("```") && part.endsWith("```")) {
      const code = part.slice(3, -3);
      const firstLine = code.indexOf("\n");
      const lang = firstLine > 0 ? code.slice(0, firstLine).trim() : "";
      const codeContent = firstLine > 0 ? code.slice(firstLine + 1) : code;
      return (
        <pre key={i} className="bg-muted-foreground/10 rounded-md p-3 my-2 overflow-x-auto text-xs">
          {lang && <div className="text-xs text-muted-foreground mb-1">{lang}</div>}
          <code>{codeContent}</code>
        </pre>
      );
    }
    // 处理 **bold**
    const boldParts = part.split(/(\*\*.*?\*\*)/g);
    return boldParts.map((bp, j) => {
      if (bp.startsWith("**") && bp.endsWith("**")) {
        return <strong key={`${i}-${j}`}>{bp.slice(2, -2)}</strong>;
      }
      return bp;
    });
  });
}

// 临时 mock：后续替换为真实的 Bridge API 调用
async function mockBridgeAsk(question: string, modelId: string): Promise<string> {
  // 模拟延迟
  await new Promise((r) => setTimeout(r, 500));

  const modelName = MODELS.find((m) => m.id === modelId)?.label || modelId;

  // 检查是否有文件操作意图
  if (question.includes("文件") || question.includes("读取") || question.includes("分析")) {
    return `我已读取到文件内容。

根据文件内容分析如下：

## 文件摘要
- 文件大小与类型：已确认
- 主要内容：包含文本数据

## 分析结果
\`\`\`
该文件的内容已成功读取，可以作为上下文进行进一步处理。
\`\`\`

如需进一步操作（修改、转换、提取数据等），请告诉我具体需求。`;
  }

  // 通用回答
  return `> 当前模型：${modelName}

你好！我是量子平台助手。

你刚才的问题是：
> ${question.slice(0, 100)}${question.length > 100 ? "..." : ""}

## 可用的数据源

当前平台已接入以下 Entity：
| 名称 | 类型 | 状态 |
|------|------|------|
| DeepSeek LLM | AI 模型 | ✅ 在线 |
| 桌面终端 | 终端智能体 | ✅ 在线 |
| 文件管理器 | 文件系统 | ✅ 在线 |

## 我能帮你做什么？

1. **文件分析** — 拖拽文件到输入框，我可以读取并分析
2. **代码编写** — 写代码、调试、重构
3. **信息检索** — 查询本地文件、搜索网络
4. **终端操作** — 通过接入的终端执行命令

请告诉我具体需求！`;
}

// ─── 导出 ───────────────────────────────────────────────────────────────────

export default function ChatPageWrapper() {
  return (
    <AuthProvider>
      <ChatPage />
    </AuthProvider>
  );
}
