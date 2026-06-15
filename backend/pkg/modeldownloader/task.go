package modeldownloader

import (
    "context"
    "encoding/json"
    "fmt"
    "io"
    "net/http"
    "os"
    "path/filepath"
    "sync"
    "time"
)

const (
    // chunkSize 单个文件分片下载的大小（也用作单个文件 write buffer）
    chunkSize = 8 * 1024 * 1024
    // stateFile 保存下载状态的文件名
    stateFile = ".download_state.json"
)

// DownloadTask 表示一个进行中的下载任务
type DownloadTask struct {
    ID          string
    ModelID     string
    ModelName   string
    Source      ModelSource
    Ref         string
    Files       []string
    TargetDir   string
    TotalBytes  int64
    DoneBytes   int64
    SpeedBPS    int64
    Status      DownloadStatus
    StartedAt   time.Time
    UpdatedAt   time.Time
    Error       string
    CancelFunc  context.CancelFunc
    mu          sync.Mutex
}

// stateJSON 持久化到磁盘的下载状态
type stateJSON struct {
    TaskID     string         `json:"task_id"`
    ModelID    string         `json:"model_id"`
    ModelName  string         `json:"model_name"`
    Source     ModelSource    `json:"source"`
    Ref        string         `json:"ref"`
    Files      []string       `json:"files"`
    TargetDir  string         `json:"target_dir"`
    TotalBytes int64          `json:"total_bytes"`
    DoneBytes  int64          `json:"done_bytes"`
    Status     DownloadStatus `json:"status"`
    StartedAt  time.Time      `json:"started_at"`
    UpdatedAt  time.Time      `json:"updated_at"`
    FileBytes  map[string]int64 `json:"file_bytes"` // 每个文件已下载的字节
}

// start 启动下载；可断点续传
func (t *DownloadTask) start(ctx context.Context, progress chan<- ProgressEvent) {
    t.mu.Lock()
    t.Status = StatusRunning
    t.StartedAt = time.Now()
    t.UpdatedAt = t.StartedAt
    t.mu.Unlock()
    t.persist()

    defer func() {
        t.mu.Lock()
        if t.Status != StatusCompleted && t.Status != StatusCancelled {
            if ctx.Err() == context.Canceled {
                t.Status = StatusCancelled
            } else {
                t.Status = StatusPaused
            }
        }
        t.UpdatedAt = time.Now()
        t.mu.Unlock()
        t.persist()
    }()

    fileBytes := make(map[string]int64)
    // 读取旧状态，支持断点续传
    if state, err := t.loadState(); err == nil && state.FileBytes != nil {
        fileBytes = state.FileBytes
        t.DoneBytes = state.DoneBytes
    }

    for _, f := range t.Files {
        if ctx.Err() != nil { return }

        target := filepath.Join(t.TargetDir, f)
        if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
            t.Error = fmt.Sprintf("mkdir %s: %v", filepath.Dir(target), err)
            t.mu.Lock(); t.Status = StatusFailed; t.UpdatedAt = time.Now(); t.mu.Unlock()
            progress <- ProgressEvent{Type: "failed", TaskID: t.ID, Error: t.Error, Status: StatusFailed}
            return
        }

        done, err := t.downloadFile(ctx, t.urlFor(f), target, fileBytes, f, progress)
        if err != nil {
            t.mu.Lock()
            t.Error = err.Error()
            t.Status = StatusFailed
            t.UpdatedAt = time.Now()
            t.mu.Unlock()
            progress <- ProgressEvent{Type: "failed", TaskID: t.ID, Error: t.Error, Status: StatusFailed}
            return
        }
        t.mu.Lock()
        t.DoneBytes += done
        t.UpdatedAt = time.Now()
        t.mu.Unlock()
        fileBytes[f] = fileBytes[f] + done
        t.persist()
    }

    t.mu.Lock()
    t.Status = StatusCompleted
    t.UpdatedAt = time.Now()
    t.mu.Unlock()
    t.persist()
    progress <- ProgressEvent{
        Type: "completed", TaskID: t.ID, Status: StatusCompleted,
        DoneBytes: t.DoneBytes, TotalBytes: t.TotalBytes,
        Message: fmt.Sprintf("✅ %s 下载完成", t.ModelName),
    }
}

// urlFor 根据 source 生成实际下载 URL
func (t *DownloadTask) urlFor(file string) string {
    switch t.Source {
    case SourceHuggingFace:
        return fmt.Sprintf("https://huggingface.co/%s/resolve/main/%s", t.Ref, file)
    case SourceModelScope:
        return fmt.Sprintf("https://www.modelscope.cn/api/v1/models/%s/repo?Revision=master&FilePath=%s", t.Ref, file)
    case SourceCustomURL:
        // Ref 作为基础 URL，file 为相对路径；如果 file 是完整 URL 则直接使用
        if file != "" && (len(file) > 8 && (file[:8] == "https://" || file[:7] == "http://")) {
            return file
        }
        return t.Ref + "/" + file
    default:
        return file
    }
}

// downloadFile 下载单个文件，支持断点续传（Range header）
func (t *DownloadTask) downloadFile(ctx context.Context, url, target string, fileBytes map[string]int64, fileKey string, progress chan<- ProgressEvent) (int64, error) {
    existing := fileBytes[fileKey]

    // 第一次请求 HEAD 拿大小
    size, err := t.getRemoteSize(url)
    if err != nil {
        return 0, fmt.Errorf("get size for %s: %w", fileKey, err)
    }

    // 已有文件大小等于远端，跳过
    if fi, err := os.Stat(target); err == nil && fi.Size() == size {
        return size, nil
    }
    // 已有部分下载，从断点继续
    startPos := existing
    if fi, err := os.Stat(target); err == nil {
        startPos = fi.Size()
    }

    flag := os.O_CREATE | os.O_WRONLY
    if startPos > 0 {
        flag |= os.O_APPEND
    } else {
        flag |= os.O_TRUNC
    }
    f, err := os.OpenFile(target, flag, 0o644)
    if err != nil {
        return 0, fmt.Errorf("open %s: %w", target, err)
    }
    defer f.Close()

    req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
    if err != nil { return 0, err }
    if startPos > 0 {
        req.Header.Set("Range", fmt.Sprintf("bytes=%d-", startPos))
    }
    resp, err := http.DefaultClient.Do(req)
    if err != nil { return 0, fmt.Errorf("http GET %s: %w", fileKey, err) }
    defer resp.Body.Close()

    if startPos > 0 && resp.StatusCode != http.StatusPartialContent {
        // 服务器不支持断点，重新从头下载
        f.Close()
        os.Remove(target)
        f, _ = os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
        startPos = 0
    } else if resp.StatusCode >= 400 {
        return 0, fmt.Errorf("%s returned %s", fileKey, resp.Status)
    }

    // 拷贝数据 + 统计速度
    buf := make([]byte, 128*1024)
    var n int64
    ticker := time.NewTicker(500 * time.Millisecond)
    defer ticker.Stop()
    var lastTickBytes int64
    lastTickTime := time.Now()

    go func() {
        for range ticker.C {
            t.mu.Lock()
            elapsed := time.Since(lastTickTime).Seconds()
            if elapsed > 0 {
                deltaBytes := t.DoneBytes + n - lastTickBytes
                t.SpeedBPS = int64(float64(deltaBytes) / elapsed)
                remaining := t.TotalBytes - (t.DoneBytes + n)
                var eta string
                if t.SpeedBPS > 0 && remaining > 0 {
                    eta = (time.Duration(remaining/t.SpeedBPS) * time.Second).String()
                }
                t.UpdatedAt = time.Now()
                curDone := t.DoneBytes + n
                curSpeed := t.SpeedBPS
                curStatus := t.Status
                t.mu.Unlock()
                progress <- ProgressEvent{
                    Type: "progress", TaskID: t.ID, Status: curStatus,
                    DoneBytes: curDone, TotalBytes: t.TotalBytes,
                    SpeedBPS: curSpeed, ETA: eta,
                }
            } else {
                t.mu.Unlock()
            }
        }
    }()

    for {
        nr, er := resp.Body.Read(buf)
        if nr > 0 {
            nw, ew := f.Write(buf[:nr])
            if ew != nil { return n, ew }
            n += int64(nw)
        }
        if er == io.EOF { break }
        if er != nil { return n, er }
        if ctx.Err() != nil { return n, ctx.Err() }
    }

    t.mu.Lock()
    lastTickBytes = t.DoneBytes + n
    t.mu.Unlock()
    return n, nil
}

// getRemoteSize HEAD 请求获取文件大小
func (t *DownloadTask) getRemoteSize(url string) (int64, error) {
    req, err := http.NewRequest("HEAD", url, nil)
    if err != nil { return 0, err }
    resp, err := http.DefaultClient.Do(req)
    if err != nil { return 0, err }
    defer resp.Body.Close()
    if resp.StatusCode >= 400 {
        // 回退：尝试 GET + Content-Length
        return 0, fmt.Errorf("HEAD %s", resp.Status)
    }
    return resp.ContentLength, nil
}

// persist 把状态写入磁盘 JSON
func (t *DownloadTask) persist() {
    state := stateJSON{
        TaskID: t.ID, ModelID: t.ModelID, ModelName: t.ModelName,
        Source: t.Source, Ref: t.Ref, Files: t.Files, TargetDir: t.TargetDir,
        TotalBytes: t.TotalBytes, DoneBytes: t.DoneBytes,
        Status: t.Status, StartedAt: t.StartedAt, UpdatedAt: t.UpdatedAt,
    }
    b, err := json.MarshalIndent(state, "", "  ")
    if err != nil { return }
    os.MkdirAll(t.TargetDir, 0o755)
    os.WriteFile(filepath.Join(t.TargetDir, stateFile), b, 0o644)
}

// loadState 从磁盘读回状态
func (t *DownloadTask) loadState() (*stateJSON, error) {
    b, err := os.ReadFile(filepath.Join(t.TargetDir, stateFile))
    if err != nil { return nil, err }
    var s stateJSON
    if err := json.Unmarshal(b, &s); err != nil { return nil, err }
    return &s, nil
}

// snapshot 对外暴露的只读副本
func (t *DownloadTask) snapshot() TaskInfo {
    t.mu.Lock()
    defer t.mu.Unlock()
    var eta string
    if t.SpeedBPS > 0 && t.TotalBytes > 0 {
        remaining := t.TotalBytes - t.DoneBytes
        eta = (time.Duration(remaining/t.SpeedBPS) * time.Second).String()
    }
    return TaskInfo{
        ID: t.ID, ModelID: t.ModelID, ModelName: t.ModelName,
        Source: t.Source, Status: t.Status, TotalBytes: t.TotalBytes,
        DoneBytes: t.DoneBytes, SpeedBPS: t.SpeedBPS,
        StartedAt: t.StartedAt, UpdatedAt: t.UpdatedAt,
        ETA: eta, Error: t.Error, InstallPath: t.TargetDir,
    }
}
