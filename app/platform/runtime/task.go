package runtime

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"maps"
	"slices"
	"sync"
	"time"
)

type TaskSnapshotStore interface {
	Publish(task *AsyncTask, event map[string]any)
}

type TaskSnapshotReader interface {
	GetSnapshot(ctx context.Context, taskID string) (map[string]any, error)
}

type TaskSnapshotWriter interface {
	PublishSnapshot(ctx context.Context, taskID string, snapshot map[string]any) error
}

type AsyncTaskOptions struct {
	ID            string
	SnapshotStore TaskSnapshotStore
}

type TaskRecordOptions struct {
	Item   any
	Detail any
	Error  string
	Count  int
}

type AsyncTask struct {
	mu            sync.Mutex
	ctx           context.Context
	cancel        context.CancelFunc
	ID            string
	Total         int
	Processed     int
	OK            int
	Fail          int
	Status        string
	Warning       any
	Result        map[string]any
	Error         string
	CreatedAt     float64
	Cancelled     bool
	queues        []chan map[string]any
	finalEvent    map[string]any
	snapshotStore TaskSnapshotStore
}

func NewAsyncTask(total int, options AsyncTaskOptions) *AsyncTask {
	id := options.ID
	if id == "" {
		id = randomTaskID()
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &AsyncTask{
		ID:            id,
		ctx:           ctx,
		cancel:        cancel,
		Total:         total,
		Status:        "running",
		CreatedAt:     float64(time.Now().UnixNano()) / 1e9,
		snapshotStore: options.SnapshotStore,
	}
}

func (t *AsyncTask) Context() context.Context {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.ctx == nil {
		return context.Background()
	}
	return t.ctx
}

func (t *AsyncTask) IsCancelled() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.Cancelled
}

func (t *AsyncTask) Attach() chan map[string]any {
	ch := make(chan map[string]any, 200)
	t.mu.Lock()
	t.queues = append(t.queues, ch)
	t.mu.Unlock()
	return ch
}

func (t *AsyncTask) Detach(ch chan map[string]any) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for i, candidate := range t.queues {
		if candidate == ch {
			t.queues = append(t.queues[:i], t.queues[i+1:]...)
			return
		}
	}
}

func (t *AsyncTask) Record(success bool, options TaskRecordOptions) {
	delta := max(1, options.Count)
	t.mu.Lock()
	t.Processed += delta
	if success {
		t.OK += delta
	} else {
		t.Fail += delta
	}
	event := t.baseEvent("progress")
	if options.Item != nil {
		event["item"] = options.Item
	}
	if options.Detail != nil {
		event["detail"] = options.Detail
	}
	if options.Error != "" {
		event["error"] = options.Error
	}
	t.mu.Unlock()
	t.publish(event)
}

func (t *AsyncTask) Finish(result map[string]any, warning ...string) {
	var warningValue any
	if len(warning) > 0 {
		warningValue = warning[0]
	}
	t.mu.Lock()
	t.Status = "done"
	t.Result = result
	t.Warning = warningValue
	event := t.baseEvent("done")
	event["warning"] = warningValue
	event["result"] = result
	t.finalEvent = event
	t.mu.Unlock()
	t.publish(event)
}

func (t *AsyncTask) FailTask(message string) {
	t.mu.Lock()
	t.Status = "error"
	t.Error = message
	event := t.baseEvent("error")
	event["error"] = message
	t.finalEvent = event
	t.mu.Unlock()
	t.publish(event)
}

func (t *AsyncTask) Cancel() {
	t.mu.Lock()
	t.Cancelled = true
	cancel := t.cancel
	t.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (t *AsyncTask) FinishCancelled() {
	t.mu.Lock()
	t.Status = "cancelled"
	event := t.baseEvent("cancelled")
	t.finalEvent = event
	t.mu.Unlock()
	t.publish(event)
}

func (t *AsyncTask) Snapshot() map[string]any {
	t.mu.Lock()
	defer t.mu.Unlock()
	return map[string]any{
		"task_id":   t.ID,
		"status":    t.Status,
		"total":     t.Total,
		"processed": t.Processed,
		"ok":        t.OK,
		"fail":      t.Fail,
		"warning":   t.Warning,
	}
}

func (t *AsyncTask) FinalEvent() map[string]any {
	t.mu.Lock()
	defer t.mu.Unlock()
	return cloneTaskMap(t.finalEvent)
}

func (t *AsyncTask) baseEvent(kind string) map[string]any {
	return map[string]any{
		"type":      kind,
		"task_id":   t.ID,
		"total":     t.Total,
		"processed": t.Processed,
		"ok":        t.OK,
		"fail":      t.Fail,
	}
}

func (t *AsyncTask) publish(event map[string]any) {
	t.mu.Lock()
	queues := slices.Clone(t.queues)
	store := t.snapshotStore
	t.mu.Unlock()
	for _, ch := range queues {
		select {
		case ch <- event:
		default:
		}
	}
	if store != nil {
		store.Publish(t, event)
	}
}

var taskRegistry = struct {
	sync.Mutex
	tasks         map[string]*AsyncTask
	snapshotStore TaskSnapshotStore
}{tasks: map[string]*AsyncTask{}}

func SetTaskSnapshotStore(store TaskSnapshotStore) {
	taskRegistry.Lock()
	taskRegistry.snapshotStore = store
	taskRegistry.Unlock()
}

func CreateTask(total int) *AsyncTask {
	taskRegistry.Lock()
	store := taskRegistry.snapshotStore
	task := NewAsyncTask(total, AsyncTaskOptions{SnapshotStore: store})
	taskRegistry.tasks[task.ID] = task
	taskRegistry.Unlock()
	if store != nil {
		store.Publish(task, nil)
	}
	return task
}

func GetTask(taskID string) *AsyncTask {
	taskRegistry.Lock()
	defer taskRegistry.Unlock()
	return taskRegistry.tasks[taskID]
}

func GetTaskSnapshot(ctx context.Context, taskID string) (map[string]any, bool, error) {
	if task := GetTask(taskID); task != nil {
		return task.Snapshot(), true, nil
	}
	taskRegistry.Lock()
	store := taskRegistry.snapshotStore
	taskRegistry.Unlock()
	reader, ok := store.(TaskSnapshotReader)
	if !ok || reader == nil {
		return nil, false, nil
	}
	snapshot, err := reader.GetSnapshot(ctx, taskID)
	if err != nil {
		return nil, false, err
	}
	if snapshot == nil {
		return nil, false, nil
	}
	return snapshot, true, nil
}

func PublishTaskSnapshot(ctx context.Context, taskID string, snapshot map[string]any) error {
	taskRegistry.Lock()
	store := taskRegistry.snapshotStore
	taskRegistry.Unlock()
	writer, ok := store.(TaskSnapshotWriter)
	if !ok || writer == nil {
		return nil
	}
	return writer.PublishSnapshot(ctx, taskID, snapshot)
}

func ExpireTask(ctx context.Context, taskID string, ttl time.Duration) error {
	timer := time.NewTimer(ttl)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		taskRegistry.Lock()
		delete(taskRegistry.tasks, taskID)
		taskRegistry.Unlock()
		return nil
	}
}

func cloneTaskMap(input map[string]any) map[string]any {
	return maps.Clone(input)
}

func randomTaskID() string {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err == nil {
		return hex.EncodeToString(raw[:])
	}
	return fmt.Sprintf("%032s", NextHex(32))
}
