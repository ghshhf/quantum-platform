package modeldownloader

import (
    "context"
    "encoding/json"
    "fmt"
    "os"
    "os/exec"
    "path/filepath"
    "sort"
    "sync"
    "time"

    "github.com/google/uuid"
)

// Manager 管理所有本地模型的下载、列表、状态查询
type Manager struct {
    rootDir  string                      // 模型存储根目录
    ollamaURL string                     // Ollama API 地址
    tasks    map[string]*DownloadTask    // 所有下载任务
    taskMu   sync.RWMutex
    listeners map[chan ProgressEvent]struct{} // SSE 订阅者
    lisMu    sync.RWMutex
}

// NewManager 创建一个下载管理器
// rootDir：模型下载存储根目录（如 ~/.monkeycode/models）
// ollamaURL：Ollama API 地址（如 http://127.0.0.1:11434）
func NewManager(rootDir, ollamaURL string) (*Manager, error) {
    if rootDir == "" {
        home, err := os.UserHomeDir()
        if err != nil { return nil, err }
        rootDir = filepath.Join(home, ".monkeycode", "models")
    }
    if err := os.MkdirAll(rootDir, 0o755); err != nil {
        return nil, fmt.Errorf("mkdir models root: %w", err)
    }
    if ollamaURL == "" { ollamaURL = "http://127.0.0.1:11434" }
    m := &Manager{
        rootDir:   rootDir,
        ollamaURL: ollamaURL,
        tasks:     make(map[string]*DownloadTask),
        listeners: make(map[chan ProgressEvent]struct{}),
    }
    // 从磁盘恢复未完成任务
    m.restoreFromDisk()
    return m, nil
}

// Catalog 返回所有可下载模型的目录
func (m *Manager) Catalog() []ModelCatalogEntry {
    return builtinCatalog
}

// Tasks 返回当前所有下载任务（含已完成）
func (m *Manager) Tasks() []TaskInfo {
    m.taskMu.RLock()
    defer m.taskMu.RUnlock()
    infos := make([]TaskInfo, 0, len(m.tasks))
    for _, t := range m.tasks {
        infos = append(infos, t.snapshot())
    }
    sort.Slice(infos, func(i, j int) bool {
        return infos[i].UpdatedAt.After(infos[j].UpdatedAt)
    })
    return infos
}

// Start 启动一个下载任务
func (m *Manager) Start(modelID string) (*TaskInfo, error) {
    entry, ok := builtinCatalogIndex[modelID]
    if !ok {
        return nil, fmt.Errorf("unknown model %q (available: %v)", modelID, catalogIDs())
    }

    // 若同一模型已在下载/下载完成，直接返回
    m.taskMu.RLock()
    for _, t := range m.tasks {
        if t.ModelID == modelID && (t.Status == StatusRunning || t.Status == StatusCompleted) {
            s := t.snapshot()
            m.taskMu.RUnlock()
            return &s, nil
        }
    }
    m.taskMu.RUnlock()

    ctx, cancel := context.WithCancel(context.Background())
    task := &DownloadTask{
        ID:         uuid.New().String()[:8],
        ModelID:    entry.ID,
        ModelName:  entry.Name,
        Source:     entry.Source,
        Ref:        entry.Ref,
        Files:      append([]string(nil), entry.Files...),
        TargetDir:  filepath.Join(m.rootDir, entry.ID),
        TotalBytes: entry.Size,
        Status:     StatusQueued,
        CancelFunc: cancel,
    }
    // Ollama 不走文件下载，而是 exec ollama pull
    if entry.Source == SourceOllama {
        task.TargetDir = "<ollama>" // 实际路径由 Ollama 管理
        task.Files = []string{entry.Ref}
    }

    m.taskMu.Lock()
    m.tasks[task.ID] = task
    m.taskMu.Unlock()

    progress := make(chan ProgressEvent, 128)
    go m.broadcast(progress)

    go func() {
        if task.Source == SourceOllama {
            m.runOllamaPull(ctx, task, progress)
        } else {
            task.start(ctx, progress)
        }
        close(progress)
    }()
    info := task.snapshot()
    return &info, nil
}

// Cancel 取消一个下载任务
func (m *Manager) Cancel(taskID string) error {
    m.taskMu.Lock()
    t, ok := m.tasks[taskID]
    m.taskMu.Unlock()
    if !ok { return fmt.Errorf("task %s not found", taskID) }
    if t.CancelFunc != nil { t.CancelFunc() }
    t.mu.Lock()
    t.Status = StatusCancelled
    t.mu.Unlock()
    t.persist()
    m.broadcastOne(ProgressEvent{Type: "cancelled", TaskID: taskID, Status: StatusCancelled})
    return nil
}

// Delete 删除一个已下载模型
func (m *Manager) Delete(taskID string) error {
    m.taskMu.Lock()
    t, ok := m.tasks[taskID]
    m.taskMu.Unlock()
    if !ok { return fmt.Errorf("task %s not found", taskID) }
    if t.Status == StatusRunning {
        if t.CancelFunc != nil { t.CancelFunc() }
    }
    if t.TargetDir != "" && t.TargetDir != "<ollama>" {
        os.RemoveAll(t.TargetDir)
    }
    if t.Source == SourceOllama {
        exec.Command("ollama", "rm", t.Ref).Run()
    }
    m.taskMu.Lock()
    delete(m.tasks, taskID)
    m.taskMu.Unlock()
    return nil
}

// Subscribe 订阅下载事件；返回一个只读 channel；需要调用 Unsubscribe 释放
func (m *Manager) Subscribe() <-chan ProgressEvent {
    ch := make(chan ProgressEvent, 128)
    m.lisMu.Lock()
    m.listeners[ch] = struct{}{}
    m.lisMu.Unlock()
    // 立即推送一次完整任务列表
    go func() {
        ch <- ProgressEvent{Type: "task_list", Tasks: m.Tasks()}
    }()
    return ch
}

// Unsubscribe 取消订阅
func (m *Manager) Unsubscribe(ch <-chan ProgressEvent) {
    // 由于 chan 类型的限制，我们通过匹配行为释放：保留监听者集合里相同通道实例
    m.lisMu.Lock()
    defer m.lisMu.Unlock()
    for k := range m.listeners {
        if k == ch { delete(m.listeners, k); break }
    }
}

// broadcast 把单个 progress channel 的事件转发给所有订阅者
func (m *Manager) broadcast(src <-chan ProgressEvent) {
    for ev := range src {
        m.broadcastOne(ev)
    }
    // 每次下载结束后再推送一次任务列表快照
    m.broadcastOne(ProgressEvent{Type: "task_list", Tasks: m.Tasks()})
}

func (m *Manager) broadcastOne(ev ProgressEvent) {
    m.lisMu.RLock()
    for k := range m.listeners {
        select {
        case k <- ev:
        default: // 订阅者读取慢就丢事件，避免阻塞下载
        }
    }
    m.lisMu.RUnlock()
}

// runOllamaPull 执行 ollama pull 命令，解析 stdout 估算进度
func (m *Manager) runOllamaPull(ctx context.Context, t *DownloadTask, progress chan<- ProgressEvent) {
    t.mu.Lock()
    t.Status = StatusRunning
    t.StartedAt = time.Now()
    t.UpdatedAt = t.StartedAt
    t.mu.Unlock()

    cmd := exec.CommandContext(ctx, "ollama", "pull", t.Ref)
    stdout, err := cmd.StdoutPipe()
    if err != nil {
        t.mu.Lock(); t.Status = StatusFailed; t.Error = err.Error(); t.mu.Unlock()
        progress <- ProgressEvent{Type: "failed", TaskID: t.ID, Error: err.Error(), Status: StatusFailed}
        return
    }
    if err := cmd.Start(); err != nil {
        t.mu.Lock(); t.Status = StatusFailed; t.Error = err.Error(); t.mu.Unlock()
        progress <- ProgressEvent{Type: "failed", TaskID: t.ID, Error: err.Error(), Status: StatusFailed}
        return
    }
    // 轮询 ollama /api/show 或简单按 stdout 解析
    buf := make([]byte, 4096)
    for {
        n, err := stdout.Read(buf)
        if n > 0 {
            t.mu.Lock()
            t.DoneBytes += int64(n)
            t.UpdatedAt = time.Now()
            t.mu.Unlock()
            progress <- ProgressEvent{
                Type: "progress", TaskID: t.ID, Status: StatusRunning,
                DoneBytes: t.DoneBytes, TotalBytes: t.TotalBytes,
                Message: "ollama pull: " + string(buf[:n]),
            }
        }
        if err != nil { break }
    }
    if err := cmd.Wait(); err != nil {
        t.mu.Lock(); t.Status = StatusFailed; t.Error = err.Error(); t.mu.Unlock()
        progress <- ProgressEvent{Type: "failed", TaskID: t.ID, Error: err.Error(), Status: StatusFailed}
        return
    }
    t.mu.Lock()
    t.Status = StatusCompleted
    t.DoneBytes = t.TotalBytes
    t.UpdatedAt = time.Now()
    t.mu.Unlock()
    progress <- ProgressEvent{
        Type: "completed", TaskID: t.ID, Status: StatusCompleted,
        DoneBytes: t.TotalBytes, TotalBytes: t.TotalBytes,
        Message: "✅ ollama pull " + t.Ref + " 完成",
    }
}

// restoreFromDisk 从磁盘恢复历史下载任务
func (m *Manager) restoreFromDisk() {
    entries, err := os.ReadDir(m.rootDir)
    if err != nil { return }
    for _, e := range entries {
        if !e.IsDir() { continue }
        p := filepath.Join(m.rootDir, e.Name(), stateFile)
        b, err := os.ReadFile(p)
        if err != nil { continue }
        var s stateJSON
        if err := json.Unmarshal(b, &s); err != nil { continue }
        task := &DownloadTask{
            ID: s.TaskID, ModelID: s.ModelID, ModelName: s.ModelName,
            Source: s.Source, Ref: s.Ref, Files: s.Files,
            TargetDir: s.TargetDir, TotalBytes: s.TotalBytes, DoneBytes: s.DoneBytes,
            Status: s.Status, StartedAt: s.StartedAt, UpdatedAt: s.UpdatedAt,
        }
        if task.Status == StatusRunning { task.Status = StatusPaused }
        m.tasks[task.ID] = task
    }
}
