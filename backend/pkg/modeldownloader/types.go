// Package modeldownloader 提供大语言模型文件的下载与管理能力
// 支持断点续传、多源（HuggingFace / ModelScope / 本地 Ollama）、进度广播。
package modeldownloader

import "time"

// ModelSource 模型下载来源
type ModelSource string

const (
    SourceHuggingFace ModelSource = "huggingface"
    SourceModelScope  ModelSource = "modelscope"
    SourceOllama       ModelSource = "ollama" // 通过 ollama pull 而非直接下载文件
    SourceCustomURL    ModelSource = "custom"
)

// ModelCatalogEntry 表示一个可下载的模型条目（内置在注册表中）
type ModelCatalogEntry struct {
    ID          string      `json:"id"`           // 唯一 ID，如 "qwen2.5:7b"
    Name        string      `json:"name"`         // 显示名
    Description string      `json:"description"`  // 简介
    Source      ModelSource `json:"source"`       // 来源
    Ref         string      `json:"ref"`          // 源引用（HuggingFace repo 或 Ollama model 名）
    Size        int64       `json:"size"`         // 预估大小（字节）
    Quant       string      `json:"quant"`        // 量化等级（q4_0 / q5_0 / fp16）
    Family      string      `json:"family"`       // 模型家族（qwen / llama / deepseek 等）
    ParamCount  string      `json:"param_count"`  // 参数量显示
    Files       []string    `json:"files"`        // 需要下载的文件列表（相对 ref 的路径）
    URL         string      `json:"url"`          // 自定义 URL（当 Source == SourceCustomURL 时）
}

// DownloadStatus 下载任务的整体状态
type DownloadStatus string

const (
    StatusQueued    DownloadStatus = "queued"
    StatusRunning   DownloadStatus = "running"
    StatusPaused    DownloadStatus = "paused"
    StatusCompleted DownloadStatus = "completed"
    StatusFailed    DownloadStatus = "failed"
    StatusCancelled DownloadStatus = "cancelled"
)

// TaskInfo 下载任务信息（对外部暴露）
type TaskInfo struct {
    ID          string         `json:"id"`
    ModelID     string         `json:"model_id"`
    ModelName   string         `json:"model_name"`
    Source      ModelSource    `json:"source"`
    Status      DownloadStatus `json:"status"`
    TotalBytes  int64          `json:"total_bytes"`
    DoneBytes   int64          `json:"done_bytes"`
    SpeedBPS    int64          `json:"speed_bps"`
    StartedAt   time.Time      `json:"started_at"`
    UpdatedAt   time.Time      `json:"updated_at"`
    ETA         string         `json:"eta"`
    Error       string         `json:"error,omitempty"`
    InstallPath string         `json:"install_path,omitempty"`
}

// ProgressEvent 下载进度事件（用于 SSE 广播）
type ProgressEvent struct {
    Type     string   `json:"type"`     // "progress" | "completed" | "failed" | "task_list"
    TaskID   string   `json:"task_id"`
    Status   DownloadStatus `json:"status,omitempty"`
    Tasks    []TaskInfo `json:"tasks,omitempty"`
    DoneBytes  int64    `json:"done_bytes,omitempty"`
    TotalBytes int64    `json:"total_bytes,omitempty"`
    SpeedBPS   int64    `json:"speed_bps,omitempty"`
    ETA        string   `json:"eta,omitempty"`
    Error      string   `json:"error,omitempty"`
    Message    string   `json:"message,omitempty"`
}

// DownloadRequest 启动下载的请求参数
type DownloadRequest struct {
    ModelID string `json:"model_id"` // 需要对应 registry 中的 ID
}
