import { useEffect, useState } from "react"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Field, FieldContent, FieldLabel } from "@/components/ui/field"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { Spinner } from "@/components/ui/spinner"
import { Badge } from "@/components/ui/badge"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { modelProviderPresets } from "@/utils/common"
import { apiRequest } from "@/utils/requestUtils"
import { toast } from "sonner"
import { ConstsInterfaceType, ConstsModelProvider } from "@/api/Api"
import {
  CircleCheck,
  CircleAlert,
  CircleArrowRight,
  Cpu,
  Cloud,
  Wrench,
} from "lucide-react"

const SETUP_DONE_KEY = "mcai_setup_done"
const STEP_TITLES = ["欢迎", "选择模型来源", "配置模型", "完成"]
const TOTAL_STEPS = STEP_TITLES.length

type SourceKind = "local" | "cloud" | "manual"

interface RecommendedModel {
  name: string
  params: string
  size: string
  description: string
}

const RECOMMENDED_MODELS: RecommendedModel[] = [
  {
    name: "qwen2.5:7b",
    params: "7B",
    size: "~4.7GB",
    description: "轻量通用，4核16G可跑",
  },
  {
    name: "qwen2.5-coder:7b",
    params: "7B",
    size: "~4.7GB",
    description: "代码优化版，适合本地开发",
  },
  {
    name: "llama3.2",
    params: "3B",
    size: "~2.0GB",
    description: "超轻模型，快速响应",
  },
  {
    name: "deepseek-coder-v2",
    params: "16B",
    size: "~8.2GB",
    description: "专业代码模型，需要 24G 内存",
  },
]

interface OllamaStatus {
  ok: boolean
  models: string[]
}

export default function SetupWizard() {
  const [step, setStep] = useState<number>(1)
  const [source, setSource] = useState<SourceKind | null>(null)

  // 云端 Provider 配置
  const [cloudProvider, setCloudProvider] = useState<ConstsModelProvider | "">("")
  const [cloudBaseUrl, setCloudBaseUrl] = useState<string>("")
  const [cloudApiKey, setCloudApiKey] = useState<string>("")
  const [cloudChecking, setCloudChecking] = useState<boolean>(false)
  const [cloudOk, setCloudOk] = useState<boolean | null>(null)

  // 本地 Ollama 状态
  const [ollamaStatus, setOllamaStatus] = useState<OllamaStatus>({
    ok: false,
    models: [],
  })
  const [enablingModel, setEnablingModel] = useState<string | null>(null)
  const [enabledModels, setEnabledModels] = useState<string[]>([])

  // 轮询 Ollama 状态
  useEffect(() => {
    if (step !== 3 || source !== "local") return
    let cancelled = false

    const poll = async () => {
      try {
        const resp = await fetch("http://localhost:11434/api/tags")
        if (!resp.ok) throw new Error("bad status")
        const data = (await resp.json()) as { models?: { name?: string }[] }
        const names = (data.models || [])
          .map((m) => m.name)
          .filter((n): n is string => Boolean(n))
        if (!cancelled) {
          setOllamaStatus({ ok: true, models: names })
        }
      } catch {
        if (!cancelled) {
          setOllamaStatus({ ok: false, models: [] })
        }
      }
    }

    poll()
    const timer = window.setInterval(poll, 1000)
    return () => {
      cancelled = true
      window.clearInterval(timer)
    }
  }, [step, source])

  const handleCloudProviderChange = (p: ConstsModelProvider | "") => {
    setCloudProvider(p)
    const preset = p ? modelProviderPresets[p] : undefined
    setCloudBaseUrl(preset?.baseUrl || "")
    setCloudOk(null)
  }

  const handleCheckCloud = async () => {
    if (!cloudProvider) {
      toast.error("请选择 Provider")
      return
    }
    if (!cloudApiKey.trim()) {
      toast.error("请输入 API Key")
      return
    }
    if (!cloudBaseUrl.trim()) {
      toast.error("请输入模型 API 地址")
      return
    }
    setCloudChecking(true)
    setCloudOk(null)

    await apiRequest(
      "v1UsersModelsHealthCheckCreate",
      {
        api_key: cloudApiKey.trim(),
        base_url: cloudBaseUrl.trim(),
        provider: cloudProvider,
        interface_type: ConstsInterfaceType.InterfaceTypeOpenAIChat,
        model: "",
      },
      [],
      (resp) => {
        if (resp.code === 0 && resp.data?.success) {
          setCloudOk(true)
          toast.success("连接成功")
        } else {
          setCloudOk(false)
          toast.error("连接失败：" + (resp.data?.error || resp.message))
        }
      },
    )
    setCloudChecking(false)
  }

  const handleSaveCloud = async () => {
    if (!cloudProvider || !cloudApiKey.trim() || !cloudBaseUrl.trim()) {
      toast.error("请先填完配置并测试连接")
      return
    }
    await apiRequest(
      "v1UsersModelsCreate",
      {
        provider: cloudProvider,
        model: "auto",
        remark: `${modelProviderPresets[cloudProvider]?.label || cloudProvider}（向导）`,
        base_url: cloudBaseUrl.trim(),
        api_key: cloudApiKey.trim(),
        interface_type: ConstsInterfaceType.InterfaceTypeOpenAIChat,
        context_limit: 128000,
        output_limit: 32000,
      },
      [],
      (resp) => {
        if (resp.code === 0) {
          toast.success("模型已添加")
          setCloudOk(true)
          finishSetup()
        } else {
          toast.error("保存失败：" + resp.message)
        }
      },
    )
  }

  const handleEnableLocalModel = async (modelName: string) => {
    if (enablingModel) return
    setEnablingModel(modelName)
    await apiRequest(
      "v1UsersModelsCreate",
      {
        provider: ConstsModelProvider.ModelProviderOllama,
        model: modelName,
        remark: `Ollama 本地模型（${modelName}）`,
        base_url: "http://localhost:11434/v1",
        api_key: "ollama",
        interface_type: ConstsInterfaceType.InterfaceTypeOpenAIChat,
        context_limit: 128000,
        output_limit: 32000,
      },
      [],
      (resp) => {
        if (resp.code === 0) {
          toast.success(`已启用：${modelName}`)
          setEnabledModels((prev) => Array.from(new Set([...prev, modelName])))
        } else {
          toast.error("保存失败：" + resp.message)
        }
      },
    )
    setEnablingModel(null)
  }

  const finishSetup = () => {
    try {
      localStorage.setItem(SETUP_DONE_KEY, "1")
    } catch {
      // ignore
    }
    setStep(TOTAL_STEPS)
  }

  const handleGotoConsole = () => {
    try {
      localStorage.setItem(SETUP_DONE_KEY, "1")
    } catch {
      // ignore
    }
    window.location.replace("/console")
  }

  const canGoNextFromStep2 = source !== null
  const canGoNextFromStep3 = (() => {
    if (source === "manual") return true
    if (source === "cloud") return cloudOk === true
    if (source === "local") return enabledModels.length > 0 || ollamaStatus.models.length > 0
    return false
  })()

  const renderStepProgress = () => (
    <div className="w-full max-w-3xl">
      <div className="flex items-center justify-between">
        {STEP_TITLES.map((title, idx) => {
          const stepNum = idx + 1
          const isActive = step === stepNum
          const isDone = step > stepNum
          return (
            <div key={stepNum} className="flex flex-1 items-center">
              <div className="flex flex-col items-center">
                <div
                  className={[
                    "flex h-9 w-9 items-center justify-center rounded-full border text-sm font-semibold transition-colors",
                    isActive
                      ? "border-primary bg-primary text-primary-foreground"
                      : isDone
                        ? "border-primary bg-primary text-primary-foreground"
                        : "border-muted-foreground/40 text-muted-foreground",
                  ].join(" ")}
                >
                  {isDone ? <CircleCheck className="size-4" /> : stepNum}
                </div>
                <span
                  className={[
                    "mt-2 text-xs",
                    isActive ? "font-semibold text-foreground" : "text-muted-foreground",
                  ].join(" ")}
                >
                  {title}
                </span>
              </div>
              {stepNum < TOTAL_STEPS && (
                <div
                  className={[
                    "mx-2 mb-6 h-0.5 flex-1 rounded-full",
                    step > stepNum ? "bg-primary" : "bg-muted-foreground/20",
                  ].join(" ")}
                />
              )}
            </div>
          )
        })}
      </div>
    </div>
  )

  const renderStep1 = () => (
    <div className="flex flex-col items-center text-center">
      <div className="text-4xl font-bold">欢迎使用 量子平台</div>
      <p className="mt-4 max-w-xl text-muted-foreground">
        量子平台 是一个支持本地与局域网部署的 AI 开发平台。你可以用自己的大模型
        （通过 Ollama 在本机跑），也可以接入云端厂商的 API。下面用几步配置好你的模型。
      </p>
      <div className="mt-8 flex gap-3">
        <Badge variant="outline" className="px-3 py-1 text-sm">
          支持本地部署
        </Badge>
        <Badge variant="outline" className="px-3 py-1 text-sm">
          支持云 API
        </Badge>
        <Badge variant="outline" className="px-3 py-1 text-sm">
          完全可控
        </Badge>
      </div>
    </div>
  )

  const renderSourceCard = (
    kind: SourceKind,
    title: string,
    description: string,
    icon: React.ReactNode,
  ) => {
    const selected = source === kind
    return (
      <button
        type="button"
        onClick={() => setSource(kind)}
        className={[
          "flex flex-col items-start gap-3 rounded-xl border bg-card p-5 text-left shadow-sm transition-all hover:shadow-md",
          selected
            ? "border-primary ring-2 ring-primary/20"
            : "border-border hover:border-primary/50",
        ].join(" ")}
      >
        <div className="flex size-10 items-center justify-center rounded-lg bg-primary/10 text-primary">
          {icon}
        </div>
        <div className="text-base font-semibold">{title}</div>
        <div className="text-sm leading-relaxed text-muted-foreground">{description}</div>
        {selected && (
          <div className="mt-1 flex items-center gap-1 text-xs font-medium text-primary">
            <CircleCheck className="size-4" /> 已选择
          </div>
        )}
      </button>
    )
  }

  const renderStep2 = () => (
    <div className="flex w-full flex-col">
      <div className="mb-4">
        <div className="text-2xl font-bold">选择模型来源</div>
        <div className="mt-1 text-sm text-muted-foreground">
          请选择你打算使用的模型来源。配置完成后，随时可以在「设置 → 模型」里调整。
        </div>
      </div>
      <div className="grid w-full gap-4 sm:grid-cols-3">
        {renderSourceCard(
          "local",
          "本地运行（Ollama）",
          "模型跑在本机，不需要 API Key，完全离线。推荐配置：4核16G 可跑 qwen2.5:7b（~4.7GB），有 24G 内存可跑 qwen2.5:14b。",
          <Cpu className="size-5" />,
        )}
        {renderSourceCard(
          "cloud",
          "云端 API",
          "使用 OpenAI / DeepSeek / 百知云 / SiliconFlow 等平台提供的 API Key，启动快、模型强、不需本地资源。",
          <Cloud className="size-5" />,
        )}
        {renderSourceCard(
          "manual",
          "我自己配置",
          "跳过向导，稍后在「设置 → 模型」页面手工绑定。",
          <Wrench className="size-5" />,
        )}
      </div>
    </div>
  )

  const renderStep3Local = () => (
    <div className="flex w-full flex-col gap-4">
      <div>
        <div className="text-2xl font-bold">检测本地 Ollama</div>
        <div className="mt-1 text-sm text-muted-foreground">
          向导会每秒轮询 http://localhost:11434/api/tags。请先在本机安装并启动 Ollama。
        </div>
      </div>

      <div
        className={[
          "flex items-center gap-3 rounded-xl border p-4",
          ollamaStatus.ok ? "border-green-500/40 bg-green-500/5" : "border-amber-500/40 bg-amber-500/5",
        ].join(" ")}
      >
        <div
          className={[
            "flex size-9 items-center justify-center rounded-full",
            ollamaStatus.ok ? "bg-green-500/15 text-green-600" : "bg-amber-500/15 text-amber-600",
          ].join(" ")}
        >
          {ollamaStatus.ok ? <CircleCheck className="size-5" /> : <CircleAlert className="size-5" />}
        </div>
        <div className="flex flex-col">
          {ollamaStatus.ok ? (
            <>
              <div className="font-semibold text-green-700">检测到本地 Ollama</div>
              <div className="text-sm text-muted-foreground">
                当前共有 {ollamaStatus.models.length} 个可用模型
                {ollamaStatus.models.length > 0 && `：${ollamaStatus.models.slice(0, 3).join("、")}${ollamaStatus.models.length > 3 ? "…" : ""}`}
              </div>
            </>
          ) : (
            <>
              <div className="font-semibold text-amber-700">未检测到 Ollama</div>
              <div className="text-sm text-muted-foreground">
                请先安装并启动 Ollama（<a className="underline" href="https://ollama.com" target="_blank" rel="noreferrer">ollama.com</a>），或在下方启用模型。
              </div>
            </>
          )}
        </div>
      </div>

      <div>
        <div className="mb-2 text-sm font-semibold">推荐模型</div>
        <div className="grid gap-3 sm:grid-cols-2">
          {RECOMMENDED_MODELS.map((m) => {
            const alreadyEnabled = enabledModels.includes(m.name)
            const isEnabling = enablingModel === m.name
            return (
              <div
                key={m.name}
                className="flex flex-col gap-2 rounded-lg border bg-card p-4"
              >
                <div className="flex items-center justify-between">
                  <div className="font-semibold">{m.name}</div>
                  <div className="flex gap-2">
                    <Badge variant="outline">{m.params}</Badge>
                    <Badge variant="outline">{m.size}</Badge>
                  </div>
                </div>
                <div className="text-sm text-muted-foreground">{m.description}</div>
                <div className="mt-2">
                  <Button
                    size="sm"
                    onClick={() => handleEnableLocalModel(m.name)}
                    disabled={alreadyEnabled || isEnabling || ollamaStatus.models.length === 0 && !ollamaStatus.ok}
                  >
                    {isEnabling ? (
                      <>
                        <Spinner className="mr-1 size-4" /> 启用中…
                      </>
                    ) : alreadyEnabled ? (
                      <>
                        <CircleCheck className="mr-1 size-4" /> 已启用
                      </>
                    ) : (
                      <>
                        <CircleArrowRight className="mr-1 size-4" /> 启用
                      </>
                    )}
                  </Button>
                </div>
              </div>
            )
          })}
        </div>
      </div>

      <Alert>
        <AlertTitle>使用提示</AlertTitle>
        <AlertDescription>
          启用模型后，量子平台 会创建一条记录（Provider=Ollama，base_url=http://localhost:11434/v1）。
          如果你还没 <code className="rounded bg-muted px-1.5 py-0.5 text-xs">ollama pull qwen2.5:7b</code>，请先在终端执行。
        </AlertDescription>
      </Alert>
    </div>
  )

  const renderStep3Cloud = () => (
    <div className="flex w-full flex-col gap-4">
      <div>
        <div className="text-2xl font-bold">配置云 API</div>
        <div className="mt-1 text-sm text-muted-foreground">
          选择 Provider 后会自动填入推荐的 base_url，然后输入 API Key 并测试连接。
        </div>
      </div>

      <Field>
        <FieldLabel>模型服务（Provider）</FieldLabel>
        <FieldContent>
          <Select value={cloudProvider} onValueChange={handleCloudProviderChange}>
            <SelectTrigger>
              <SelectValue placeholder="请选择模型服务提供商" />
            </SelectTrigger>
            <SelectContent>
              {Object.entries(modelProviderPresets).map(([key, preset]) => (
                <SelectItem key={key} value={key}>
                  {preset.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </FieldContent>
      </Field>

      <Field>
        <FieldLabel>模型 API 地址</FieldLabel>
        <FieldContent>
          <Input
            placeholder="请输入模型 API 地址"
            value={cloudBaseUrl}
            onChange={(e) => {
              setCloudBaseUrl(e.target.value)
              setCloudOk(null)
            }}
          />
        </FieldContent>
      </Field>

      <Field>
        <FieldLabel>API Key</FieldLabel>
        <FieldContent>
          <Input
            placeholder="请输入 API Key"
            value={cloudApiKey}
            onChange={(e) => {
              setCloudApiKey(e.target.value)
              setCloudOk(null)
            }}
          />
        </FieldContent>
      </Field>

      <div className="flex flex-wrap items-center gap-3">
        <Button onClick={handleCheckCloud} disabled={cloudChecking}>
          {cloudChecking ? (
            <>
              <Spinner className="mr-1 size-4" /> 测试中…
            </>
          ) : (
            "测试连接"
          )}
        </Button>
        {cloudOk === true && (
          <span className="flex items-center gap-1 text-sm text-green-600">
            <CircleCheck className="size-4" /> 连接成功
          </span>
        )}
        {cloudOk === false && (
          <span className="flex items-center gap-1 text-sm text-destructive">
            <CircleAlert className="size-4" /> 连接失败，请检查配置
          </span>
        )}
      </div>
    </div>
  )

  const renderStep3Manual = () => (
    <div className="flex w-full flex-col items-center text-center">
      <div className="text-2xl font-bold">稍后再配置</div>
      <p className="mt-3 max-w-xl text-muted-foreground">
        点击「完成」进入主页面。你可以在「设置 → 模型」中随时添加和管理模型。
      </p>
    </div>
  )

  const renderStep3 = () => {
    if (source === "local") return renderStep3Local()
    if (source === "cloud") return renderStep3Cloud()
    if (source === "manual") return renderStep3Manual()
    return null
  }

  const renderStep4 = () => (
    <div className="flex flex-col items-center text-center">
      <div className="flex size-16 items-center justify-center rounded-full bg-green-500/10 text-green-600">
        <CircleCheck className="size-8" />
      </div>
      <div className="mt-4 text-3xl font-bold">🎉 准备好了！</div>
      <p className="mt-3 max-w-md text-muted-foreground">
        你的 量子平台 已经配置完成，现在可以开始使用 AI 协助开发了。
      </p>
      <Button size="lg" className="mt-8" onClick={handleGotoConsole}>
        开始使用
      </Button>
    </div>
  )

  const renderFooter = () => {
    if (step === TOTAL_STEPS) return null
    return (
      <div className="mt-8 flex w-full items-center justify-between">
        <Button
          variant="outline"
          onClick={() => setStep((s) => Math.max(1, s - 1))}
          disabled={step === 1}
        >
          上一步
        </Button>
        <div className="flex items-center gap-2 text-xs text-muted-foreground">
          <span>第 {step} / {TOTAL_STEPS} 步</span>
        </div>
        {step === 3 && source === "cloud" ? (
          <Button
            onClick={handleSaveCloud}
            disabled={!canGoNextFromStep3 || cloudChecking}
          >
            保存并完成
          </Button>
        ) : step === 3 && source === "manual" ? (
          <Button onClick={finishSetup}>完成</Button>
        ) : step === 3 && source === "local" ? (
          <Button onClick={finishSetup} disabled={!canGoNextFromStep3}>
            完成
          </Button>
        ) : (
          <Button
            onClick={() => setStep((s) => Math.min(TOTAL_STEPS, s + 1))}
            disabled={(step === 2 && !canGoNextFromStep2) || (step === 3 && !canGoNextFromStep3)}
          >
            下一步
          </Button>
        )}
      </div>
    )
  }

  return (
    <div className="flex min-h-screen flex-col items-center justify-start bg-background px-4 py-10">
      <div className="mb-8 flex items-center gap-2 text-lg font-semibold">
        <span className="rounded-md bg-primary px-2 py-0.5 text-sm text-primary-foreground">
          量子平台
        </span>
        <span className="text-muted-foreground">首次启动向导</span>
      </div>

      {renderStepProgress()}

      <div className="mt-10 flex w-full max-w-3xl flex-1 flex-col items-center rounded-2xl border bg-card p-8 shadow-sm">
        {step === 1 && renderStep1()}
        {step === 2 && renderStep2()}
        {step === 3 && renderStep3()}
        {step === 4 && renderStep4()}
        {renderFooter()}
      </div>

      <div className="mt-6 flex items-center gap-3 text-xs text-muted-foreground">
        <Button
          variant="ghost"
          size="sm"
          onClick={() => {
            try {
              localStorage.setItem(SETUP_DONE_KEY, "1")
            } catch {
              // ignore
            }
            window.location.replace("/console")
          }}
        >
          跳过向导，直接使用
        </Button>
      </div>
    </div>
  )
}
