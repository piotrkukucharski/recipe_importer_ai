package import_recipe

import (
	"sync"
	"time"
)

type ImportTask struct {
	URL           string    `json:"url"`
	CorrelationID string    `json:"correlation_id"`
	Status        string    `json:"status"` // "started", "imported", "finished"
	CreatedAt     time.Time `json:"created_at"`
	User          string    `json:"user"`
	Space         string    `json:"space"`
}

type TaskManager struct {
	tasks []*ImportTask
	mu    sync.RWMutex
}

func NewTaskManager() *TaskManager {
	return &TaskManager{}
}

func (tm *TaskManager) AddTask(task *ImportTask) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	tm.tasks = append(tm.tasks, task)
}

func (tm *TaskManager) GetTask(cid string) *ImportTask {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	for _, t := range tm.tasks {
		if t.CorrelationID == cid {
			return t
		}
	}
	return nil
}

func (tm *TaskManager) GetTasks() []*ImportTask {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	tasksCopy := make([]*ImportTask, len(tm.tasks))
	copy(tasksCopy, tm.tasks)
	return tasksCopy
}
