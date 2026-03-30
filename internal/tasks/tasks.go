package tasks

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/Waasaabii/CLIProxyApi-OPS/internal/ops"
)

type Task struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Status     string    `json:"status"`
	Error      string    `json:"error,omitempty"`
	LogPath    string    `json:"logPath"`
	CreatedAt  time.Time `json:"createdAt"`
	StartedAt  time.Time `json:"startedAt,omitempty"`
	FinishedAt time.Time `json:"finishedAt,omitempty"`
	UpdatedAt  time.Time `json:"updatedAt"`
}

type Manager struct {
	baseDir string

	mu            sync.RWMutex
	runnerMu      sync.Mutex
	mutatingTask  string
	tasks         map[string]*Task
}

func New(baseDir string) (*Manager, error) {
	manager := &Manager{
		baseDir: filepath.Join(baseDir, "ops", "tasks"),
		tasks:   map[string]*Task{},
	}
	if err := os.MkdirAll(manager.baseDir, 0o755); err != nil {
		return nil, err
	}
	if err := manager.loadExisting(); err != nil {
		return nil, err
	}
	return manager, nil
}

func (m *Manager) StartMutatingTask(ctx context.Context, name string, fn func(context.Context, ops.Logger) error) (*Task, error) {
	m.runnerMu.Lock()
	defer m.runnerMu.Unlock()

	if m.mutatingTask != "" {
		return nil, fmt.Errorf("已有任务正在执行: %s", m.mutatingTask)
	}

	id, err := generateTaskID()
	if err != nil {
		return nil, err
	}
	task := &Task{
		ID:        id,
		Name:      name,
		Status:    "running",
		LogPath:   filepath.Join(m.baseDir, id+".log"),
		CreatedAt: time.Now(),
		StartedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	m.mu.Lock()
	m.tasks[id] = task
	m.mutatingTask = id
	m.mu.Unlock()
	if err = m.persist(task); err != nil {
		return nil, err
	}

	go func(taskCtx context.Context) {
		logger := &taskLogger{path: task.LogPath}
		runErr := fn(taskCtx, logger)

		m.runnerMu.Lock()
		defer m.runnerMu.Unlock()

		m.mu.Lock()
		defer m.mu.Unlock()

		if runErr != nil {
			task.Status = "failed"
			task.Error = runErr.Error()
			logger.Printf("任务失败: %v", runErr)
		} else {
			task.Status = "succeeded"
			logger.Printf("任务完成")
		}
		task.FinishedAt = time.Now()
		task.UpdatedAt = time.Now()
		m.mutatingTask = ""
		_ = m.persist(task)
	}(context.WithoutCancel(ctx))

	return cloneTask(task), nil
}

func (m *Manager) Get(id string) (*Task, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	task, ok := m.tasks[id]
	if !ok {
		return nil, errors.New("任务不存在")
	}
	return cloneTask(task), nil
}

func (m *Manager) ReadLog(id string, maxBytes int64) (string, error) {
	task, err := m.Get(id)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(task.LogPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil
		}
		return "", err
	}
	if maxBytes > 0 && int64(len(data)) > maxBytes {
		data = data[len(data)-int(maxBytes):]
	}
	return string(data), nil
}

func (m *Manager) loadExisting() error {
	entries, err := os.ReadDir(m.baseDir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || !stringsHasSuffix(entry.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(m.baseDir, entry.Name()))
		if err != nil {
			return err
		}
		var task Task
		if err = json.Unmarshal(data, &task); err != nil {
			return err
		}
		copyTask := task
		m.tasks[task.ID] = &copyTask
		if task.Status == "running" {
			copyTask.Status = "failed"
			copyTask.Error = "服务重启导致任务中断"
			copyTask.FinishedAt = time.Now()
			copyTask.UpdatedAt = time.Now()
			m.tasks[task.ID] = &copyTask
			_ = m.persist(&copyTask)
		}
	}
	return nil
}

func (m *Manager) persist(task *Task) error {
	task.UpdatedAt = time.Now()
	data, err := json.MarshalIndent(task, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(m.baseDir, task.ID+".json"), data, 0o644)
}

func cloneTask(task *Task) *Task {
	if task == nil {
		return nil
	}
	copyTask := *task
	return &copyTask
}

func generateTaskID() (string, error) {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

type taskLogger struct {
	path string
	mu   sync.Mutex
}

func (l *taskLogger) Printf(format string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if err := os.MkdirAll(filepath.Dir(l.path), 0o755); err != nil {
		return
	}
	file, err := os.OpenFile(l.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer func() { _ = file.Close() }()

	_, _ = fmt.Fprintf(file, "%s %s\n", time.Now().Format("2006-01-02 15:04:05"), fmt.Sprintf(format, args...))
}

func stringsHasSuffix(value, suffix string) bool {
	return len(value) >= len(suffix) && value[len(value)-len(suffix):] == suffix
}
