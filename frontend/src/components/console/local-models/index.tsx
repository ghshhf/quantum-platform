import { useEffect, useState } from "react"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Badge } from "@/components/ui/badge"
import { Progress } from "@/components/ui/progress"
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle, DialogTrigger } from "@/components/ui/dialog"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogTrigger,
} from "@/components/ui/alert-dialog"
import {
  CloudDownload,
  Cpu,
  Download,
  HardDrive,
  MoreVertical,
  Pause,
  RotateCw,
  Trash2,
  Wand2,
} from "lucide-react"
import { toast } from "sonner"
import { Empty, EmptyContent, EmptyDescription, EmptyHeader, EmptyMedia } from "@/components/ui/empty"
import { Spinner } from "@/components/ui/spinner"

// —— Types ——

interface CatalogEntry {
  id: string
  name: string
  description: string
  source: string
  ref: string
  size: number
  quant: string
  family: string
  param_count: string
}

interface TaskInfo {
  id: string
  model_id: string
  model_name: string
  source: string
  status: string
  total_bytes: number
  done_bytes: number
  speed_bps: number
  started_at: string
  updated_at: string
  eta: string
  error?: string
  install_path?: string
}

interface ProgressEvent {
  type: string
  task_id?: string
  status?: string
  tasks?: TaskInfo[]
  done_bytes?: number
  total_bytes?: number
  speed_bps?: number
  eta?: string
  error?: string
  message?: string
}

// —— Utils ——

function formatBytes(b: number): string {
  if (!b || b <= 0) return "0 B"
  const units = ["B", "KB", "MB", "GB", "TB"]
  let i = 0
  let n = b
  while (n >= 1024 && i < units.length - 1) {
    n /= 1024
    i++
  }
  return `${n.toFixed(n >= 10 || i === 0 ? 0 : 1)} ${units[i]}`
}

function formatSpeed(bps: number): string {
  if (!bps || bps <= 0) return "—"
  return `${formatBytes(bps)}/s`
}

// —— API ——
async function get<T>(path: string): Promise<T> {
  const r = await fetch(path, { headers: { Accept: "application/json" } })
  if (!r.ok) throw new Error(`HTTP ${r.status}`)
  const data = await r.json()
  if (data.code !== undefined && data.code !== 0) {
    throw new Error(data.message || `Error ${data.code}`)
  }
  return (data.data !== undefined ? data.data : data) as T
}

async function post<T>(path: string, body: unknown): Promise<T> {
  const r = await fetch(path, {
    method: "POST",
    headers: { "Content-Type": "application/json", Accept: "application/json" },
    body: JSON.stringify(body),
  })
  if (!r.ok) throw new Error(`HTTP ${r.status}`)
  const data = await r.json()
  if (data.code !== undefined && data.code !== 0) {
    throw new Error(data.message || `Error ${data.code}`)
  }
  return (data.data !== undefined ? data.data : data) as T
}

async function del<T>(path: string): Promise<T> {
  const r = await fetch(path, {
    method: "DELETE",
    headers: { Accept: "application/json" },
  })
  if (!r.ok) throw new Error(`HTTP ${r.status}`)
  const data = await r.json()
  if (data.code !== undefined && data.code !== 0) {
    throw new Error(data.message || `Error ${data.code}`)
  }
  return (data.data !== undefined ? data.data : data) as T
}

// —— Main Component ——

export default function LocalModels() {
  const [catalog, setCatalog] = useState<CatalogEntry[]>([])
  const [tasks, setTasks] = useState<TaskInfo[]>([])
  const [loading, setLoading] = useState(true)
  const [selectedCatalog, setSelectedCatalog] = useState<CatalogEntry | null>(null)

  // Load initial
  useEffect(() => {
    let mounted = true
    const load = async () => {
      try {
        const [c, t] = await Promise.all([
          get<CatalogEntry[]>("/api/v1/local-models/catalog"),
          get<TaskInfo[]>("/api/v1/local-models/tasks"),
        ])
        if (!mounted) return
        setCatalog(c || [])
        setTasks(t || [])
      } catch (e) {
        if (mounted) toast.error(`加载本地模型失败: ${(e as Error).message}`)
      } finally {
        if (mounted) setLoading(false)
      }
    }
    load()
    return () => { mounted = false }
  }, [])

  // SSE
  useEffect(() => {
    let es: EventSource | null = null
    try {
      es = new EventSource("/api/v1/local-models/events")
      es.addEventListener("progress", (e) => {
        try {
          const ev: ProgressEvent = JSON.parse(e.data)
          if (ev.task_id) {
            setTasks((prev) =>
              prev.map((t) =>
                t.id === ev.task_id
                  ? {
                      ...t,
                      done_bytes: ev.done_bytes ?? t.done_bytes,
                      total_bytes: ev.total_bytes ?? t.total_bytes,
                      speed_bps: ev.speed_bps ?? t.speed_bps,
                      eta: ev.eta ?? t.eta,
                      status: ev.status ?? t.status,
                    }
                  : t
              )
            )
          }
        } catch {}
      })
      es.addEventListener("completed", (e) => {
        try {
          const ev: ProgressEvent = JSON.parse(e.data)
          toast.success(ev.message || `下载完成`)
          if (ev.task_id) {
            setTasks((prev) =>
              prev.map((t) =>
                t.id === ev.task_id
                  ? { ...t, status: "completed", done_bytes: ev.total_bytes ?? t.total_bytes }
                  : t
              )
            )
          }
        } catch {}
      })
      es.addEventListener("failed", (e) => {
        try {
          const ev: ProgressEvent = JSON.parse(e.data)
          toast.error(ev.error || `下载失败`)
          if (ev.task_id) {
            setTasks((prev) =>
              prev.map((t) =>
                t.id === ev.task_id
                  ? { ...t, status: "failed", error: ev.error || t.error }
                  : t
              )
            )
          }
        } catch {}
      })
      es.addEventListener("task_list", (e) => {
        try {
          const ev: ProgressEvent = JSON.parse(e.data)
          if (ev.tasks && ev.tasks.length > 0) {
            setTasks((prev) => {
              // 合并：已有 task 保留，新增 tasks 追加
              const map = new Map<string, TaskInfo>()
              prev.forEach((t) => map.set(t.id, t))
              ev.tasks!.forEach((t) => map.set(t.id, t))
              return Array.from(map.values())
            })
          }
        } catch {}
      })
    } catch (e) {
      // 静默；SSE 失败不阻塞页面
    }
    return () => {
      if (es) es.close()
    }
  }, [])

  const startDownload = async (entry: CatalogEntry) => {
    try {
      const info = await post<TaskInfo>("/api/v1/local-models/download", { model_id: entry.id })
      setTasks((prev) => [info, ...prev])
      toast.success(`开始下载: ${entry.name}`)
      setSelectedCatalog(null)
    } catch (e) {
      toast.error(`启动失败: ${(e as Error).message}`)
    }
  }

  const cancelTask = async (id: string) => {
    try {
      await post(`/api/v1/local-models/tasks/${id}/cancel`, {})
      toast.success("已取消")
    } catch (e) {
      toast.error(`取消失败: ${(e as Error).message}`)
    }
  }

  const deleteTask = async (id: string) => {
    try {
      await del(`/api/v1/local-models/tasks/${id}`)
      setTasks((prev) => prev.filter((t) => t.id !== id))
      toast.success("已删除")
    } catch (e) {
      toast.error(`删除失败: ${(e as Error).message}`)
    }
  }

  const retryTask = async (task: TaskInfo) => {
    const entry = catalog.find((c) => c.id === task.model_id)
    if (entry) {
      startDownload(entry)
    } else {
      toast.error("找不到模型定义，无法重试")
    }
  }

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <h2 className="text-xl font-semibold flex items-center gap-2">
            <HardDrive className="size-5" />
            本地模型
          </h2>
          <p className="text-sm text-muted-foreground mt-1">
            在本地机器上直接运行大模型，无需外网 API Key。支持 Ollama 本地推理引擎。
          </p>
        </div>
        <Dialog open={!!selectedCatalog} onOpenChange={(v) => !v && setSelectedCatalog(null)}>
          <DialogTrigger asChild>
            <Button onClick={() => setSelectedCatalog(null)}>
              <Download className="mr-2 size-4" /> 浏览可下载模型
            </Button>
          </DialogTrigger>
          <DialogContent className="max-w-4xl max-h-[85vh] overflow-y-auto">
            <DialogHeader>
              <DialogTitle className="flex items-center gap-2">
                <CloudDownload className="size-5" /> 可下载模型目录
              </DialogTitle>
              <DialogDescription>
                所有模型均通过公开源下载。Ollama 来源的模型由 ollama.com 提供。
              </DialogDescription>
            </DialogHeader>
            {loading ? (
              <Empty>
                <EmptyHeader><EmptyMedia variant="icon"><Spinner /></EmptyMedia></EmptyHeader>
                <EmptyContent><EmptyDescription>加载中…</EmptyDescription></EmptyContent>
              </Empty>
            ) : (
              <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
                {catalog.map((c) => (
                  <Card key={c.id} className="hover:shadow-md transition">
                    <CardHeader className="pb-2">
                      <div className="flex items-start justify-between gap-2">
                        <div>
                          <CardTitle className="text-base flex items-center gap-2">
                            <Wand2 className="size-4 text-muted-foreground" />
                            {c.name}
                          </CardTitle>
                          <CardDescription className="text-xs mt-1">{c.description}</CardDescription>
                        </div>
                        <Badge variant="secondary">{c.source}</Badge>
                      </div>
                    </CardHeader>
                    <CardContent>
                      <div className="flex items-center justify-between text-xs text-muted-foreground mb-3">
                        <span>参数量: <span className="font-medium text-foreground">{c.param_count}</span></span>
                        <span>量化: <span className="font-medium text-foreground">{c.quant}</span></span>
                      </div>
                      <div className="flex items-center justify-between">
                        <Badge variant="outline">{c.family}</Badge>
                        <div className="flex items-center gap-2">
                          <span className="text-xs text-muted-foreground">{formatBytes(c.size)}</span>
                          <Button size="sm" onClick={() => startDownload(c)}>
                            <Download className="mr-1 size-3" /> 下载
                          </Button>
                        </div>
                      </div>
                    </CardContent>
                  </Card>
                ))}
              </div>
            )}
          </DialogContent>
        </Dialog>
      </div>

      {/* Stats */}
      <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
        <Card>
          <CardContent className="pt-6">
            <div className="flex items-center justify-between">
              <div>
                <p className="text-sm text-muted-foreground">正在下载</p>
                <p className="text-2xl font-bold mt-1">
                  {tasks.filter((t) => t.status === "queued" || t.status === "running").length}
                </p>
              </div>
              <Spinner className="size-8 text-muted-foreground opacity-60" />
            </div>
          </CardContent>
        </Card>
        <Card>
          <CardContent className="pt-6">
            <div className="flex items-center justify-between">
              <div>
                <p className="text-sm text-muted-foreground">已完成</p>
                <p className="text-2xl font-bold mt-1">{tasks.filter((t) => t.status === "completed").length}</p>
              </div>
              <Cpu className="size-8 text-green-500 opacity-60" />
            </div>
          </CardContent>
        </Card>
        <Card>
          <CardContent className="pt-6">
            <div className="flex items-center justify-between">
              <div>
                <p className="text-sm text-muted-foreground">可用模型</p>
                <p className="text-2xl font-bold mt-1">{catalog.length}</p>
              </div>
              <CloudDownload className="size-8 text-blue-500 opacity-60" />
            </div>
          </CardContent>
        </Card>
      </div>

      {/* Download Task Table */}
      <Card>
        <CardHeader>
          <CardTitle className="text-base flex items-center gap-2">
            <Download className="size-4" /> 下载任务
          </CardTitle>
          <CardDescription>正在下载或已下载的本地模型</CardDescription>
        </CardHeader>
        <CardContent>
          {tasks.length === 0 ? (
            <Empty>
              <EmptyHeader>
                <EmptyMedia variant="icon">
                  <HardDrive className="size-8 text-muted-foreground opacity-50" />
                </EmptyMedia>
              </EmptyHeader>
              <EmptyContent>
                <EmptyDescription>暂无下载任务，点击右上角"浏览可下载模型"开始</EmptyDescription>
              </EmptyContent>
            </Empty>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>模型</TableHead>
                  <TableHead>状态</TableHead>
                  <TableHead>进度</TableHead>
                  <TableHead className="w-24">速度</TableHead>
                  <TableHead className="w-24">ETA</TableHead>
                  <TableHead className="w-16"></TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {tasks.map((t) => {
                  const pct = t.total_bytes > 0 ? Math.min(100, Math.round((t.done_bytes / t.total_bytes) * 100)) : 0
                  return (
                    <TableRow key={t.id}>
                      <TableCell>
                        <div className="font-medium">{t.model_name}</div>
                        <div className="text-xs text-muted-foreground">ID: {t.id} · {t.source}</div>
                      </TableCell>
                      <TableCell>
                        <StatusBadge status={t.status} error={t.error} />
                      </TableCell>
                      <TableCell>
                        <div className="flex items-center gap-3 min-w-[160px]">
                          <Progress value={pct} className="h-2" />
                          <span className="text-xs text-muted-foreground whitespace-nowrap">{pct}%</span>
                        </div>
                        <div className="text-xs text-muted-foreground mt-1">
                          {formatBytes(t.done_bytes)} / {formatBytes(t.total_bytes)}
                        </div>
                      </TableCell>
                      <TableCell className="text-sm">{formatSpeed(t.speed_bps)}</TableCell>
                      <TableCell className="text-sm">{t.eta || "—"}</TableCell>
                      <TableCell>
                        <TaskMenu task={t} onCancel={cancelTask} onDelete={deleteTask} onRetry={retryTask} />
                      </TableCell>
                    </TableRow>
                  )
                })}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>
    </div>
  )
}

function StatusBadge({ status, error }: { status: string; error?: string }) {
  const map: Record<string, { label: string; variant: "default" | "secondary" | "destructive" | "outline" }> = {
    queued:    { label: "排队中",   variant: "secondary" },
    running:   { label: "下载中",   variant: "default" },
    paused:    { label: "已暂停",   variant: "outline" },
    completed: { label: "已完成",   variant: "default" },
    failed:    { label: "失败",     variant: "destructive" },
    cancelled: { label: "已取消",   variant: "outline" },
  }
  const info = map[status] || { label: status, variant: "outline" as const }
  return (
    <div className="flex flex-col gap-1">
      <Badge variant={info.variant}>{info.label}</Badge>
      {error && <span className="text-xs text-destructive max-w-[240px] truncate">{error}</span>}
    </div>
  )
}

function TaskMenu({
  task,
  onCancel,
  onDelete,
  onRetry,
}: {
  task: TaskInfo
  onCancel: (id: string) => void
  onDelete: (id: string) => void
  onRetry: (t: TaskInfo) => void
}) {
  const isActive = task.status === "queued" || task.status === "running"
  return (
    <AlertDialog>
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button variant="ghost" size="icon" className="h-8 w-8">
            <MoreVertical className="size-4" />
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end">
          {isActive ? (
            <DropdownMenuItem onClick={() => onCancel(task.id)}>
              <Pause className="mr-2 size-4" /> 取消下载
            </DropdownMenuItem>
          ) : (
            <DropdownMenuItem onClick={() => onRetry(task)}>
              <RotateCw className="mr-2 size-4" /> 重新下载
            </DropdownMenuItem>
          )}
          <AlertDialogTrigger asChild>
            <DropdownMenuItem onSelect={(e) => e.preventDefault()} className="text-destructive">
              <Trash2 className="mr-2 size-4" /> 删除
            </DropdownMenuItem>
          </AlertDialogTrigger>
        </DropdownMenuContent>
      </DropdownMenu>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>删除 {task.model_name}？</AlertDialogTitle>
          <AlertDialogDescription>
            已下载的模型文件将被删除，此操作无法撤销。
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>取消</AlertDialogCancel>
          <AlertDialogAction onClick={() => onDelete(task.id)} className="bg-destructive hover:bg-destructive/90">
            <Trash2 className="mr-2 size-4" /> 确认删除
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}
