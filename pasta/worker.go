package pasta

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

var (
	// ErrWorkerNil reports that a nil worker function was passed to a worker
	// spawn method.
	ErrWorkerNil = errors.New("worker nil")
	// ErrWaitGroupNil reports that a nil wait group was passed to
	// SpawnNodeWorkerWg.
	ErrWaitGroupNil = errors.New("waitgroup nil")
)

// NodeWorkerSpawner spawns workers attached to a single workspace node.
//
// Use Workspace.Spawner to create one. The zero value is inert and returns
// ErrNoNode from Spawn and SpawnWg.
type NodeWorkerSpawner struct {
	workspace *Workspace
	node      uint64
}

// WorkerSnapshot describes one live background worker owned by a workspace.
//
// Workers are runtime state and are never persisted. Full workspace snapshots
// include live workers so frontends can show background work, and worker
// notifications carry the same shape for spawn, normal stop, and panic events.
type WorkerSnapshot struct {
	// ID is the workspace-scoped worker ID assigned by SpawnNodeWorker.
	ID uint64 `json:"id"`
	// Node is the node ID the worker was attached to when it was spawned.
	Node uint64 `json:"node"`
	// Class is the node class captured when the worker was spawned.
	Class string `json:"class"`
	// Name is the node name captured when the worker was spawned.
	Name string `json:"name"`
	// WorkerName is the optional caller-provided worker name.
	WorkerName string `json:"worker_name,omitempty"`
	// StartedAt is the UTC timestamp captured when the worker was spawned.
	StartedAt time.Time `json:"started_at"`
	// Orphan is true when the attached node no longer exists in the workspace.
	Orphan bool `json:"orphan"`
	// Error describes a worker panic for NotificationWorkerFailed.
	Error string `json:"error,omitempty"`
}

type workspaceWorkers struct {
	wg     sync.WaitGroup
	nextID uint64
	alive  map[uint64]nodeWorkerRecord
}

type nodeWorkerRecord struct {
	ID         uint64
	Node       uint64
	Class      string
	Name       string
	WorkerName string
	Start      time.Time
}

func newWorkspaceWorkers() *workspaceWorkers {
	return &workspaceWorkers{
		nextID: 1,
		alive:  make(map[uint64]nodeWorkerRecord),
	}
}

func (w *workspaceWorkers) wait() {
	w.wg.Wait()
}

func (w *workspaceWorkers) nextWorkerID() uint64 {
	if w.nextID < 1 {
		w.nextID = 1
	}
	id := w.nextID
	w.nextID += 1
	return id
}

// SpawnNodeWorker runs worker in a new goroutine attached to node.
//
// If worker panics while the workspace and attached node are still live, the
// panic is contained and the node is replaced with a placeholder, just like a
// failing node callback. The workspace logs worker spawn, normal worker stop,
// and worker panic events. Close waits for all spawned workers to return, so
// node implementations should arrange for their workers to stop from OnStop.
//
// Name is optional worker metadata. Pass an empty string for unnamed workers.
func (w *Workspace) SpawnNodeWorker(node uint64, worker func(), name string) (uint64, error) {
	tracked, err := w.prepareNodeWorker(node, worker, name)
	if err != nil {
		return 0, err
	}

	go w.runNodeWorker(tracked, worker)()

	return tracked.ID, nil
}

// SpawnNodeWorkerWg runs worker in a new goroutine attached to node and tracked
// by waitgroup.
//
// It performs the same workspace bookkeeping as SpawnNodeWorker, including
// worker notifications, panic containment, and Close wait tracking. In
// addition, the goroutine is launched with waitgroup.Go so node implementations
// with their own internal wait groups can wait for the worker independently.
//
// Name is optional worker metadata. Pass an empty string for unnamed workers.
func (w *Workspace) SpawnNodeWorkerWg(node uint64, worker func(), waitgroup *sync.WaitGroup, name string) (uint64, error) {
	if worker == nil {
		return 0, ErrWorkerNil
	}
	if waitgroup == nil {
		return 0, ErrWaitGroupNil
	}

	tracked, err := w.prepareNodeWorker(node, worker, name)
	if err != nil {
		return 0, err
	}

	waitgroup.Go(w.runNodeWorker(tracked, worker))

	return tracked.ID, nil
}

// Spawner returns a node-bound worker spawner for node.
//
// The returned value does not validate node until Spawn or SpawnWg is called,
// so it is cheap to store on a node implementation during initialization.
func (w *Workspace) Spawner(node uint64) NodeWorkerSpawner {
	return NodeWorkerSpawner{workspace: w, node: node}
}

// Spawn runs worker in a new workspace-tracked goroutine attached to the
// spawner's node.
//
// Name is optional worker metadata. Pass an empty string for unnamed workers.
func (s NodeWorkerSpawner) Spawn(worker func(), name string) (uint64, error) {
	if s.workspace == nil {
		return 0, ErrNoNode
	}
	return s.workspace.SpawnNodeWorker(s.node, worker, name)
}

// SpawnWg runs worker in a new workspace-tracked goroutine attached to the
// spawner's node and also tracks it in waitgroup.
//
// Name is optional worker metadata. Pass an empty string for unnamed workers.
func (s NodeWorkerSpawner) SpawnWg(worker func(), waitgroup *sync.WaitGroup, name string) (uint64, error) {
	if s.workspace == nil {
		return 0, ErrNoNode
	}
	return s.workspace.SpawnNodeWorkerWg(s.node, worker, waitgroup, name)
}

func (w *Workspace) prepareNodeWorker(node uint64, worker func(), workerName string) (nodeWorkerRecord, error) {
	if worker == nil {
		return nodeWorkerRecord{}, ErrWorkerNil
	}
	if node < 1 {
		return nodeWorkerRecord{}, ErrNoNode
	}

	w.Lock()
	defer w.Unlock()
	if w.closed {
		return nodeWorkerRecord{}, ErrWorkspaceClosed
	}
	record, present := w.nodes.Get(node)
	if !present || record == nil {
		return nodeWorkerRecord{}, ErrNoNode
	}
	if w.workers == nil {
		w.workers = newWorkspaceWorkers()
	}

	id := w.workers.nextWorkerID()
	tracked := nodeWorkerRecord{
		ID:         id,
		Node:       node,
		Class:      record.Class,
		Name:       record.Name,
		WorkerName: workerName,
		Start:      time.Now().UTC(),
	}
	if w.workers.alive == nil {
		w.workers.alive = make(map[uint64]nodeWorkerRecord)
	}
	w.workers.alive[id] = tracked
	w.workers.wg.Add(1)
	w.log.Debugf("node worker spawned worker=%d node=%d class=%s name=%q worker_name=%q", id, node, record.Class, record.Name, workerName)
	w.enqueueWorkerNotification(NotificationWorkerSpawned, id, w.workerSnapshotLocked(tracked, ""))

	return tracked, nil
}

func (w *Workspace) runNodeWorker(worker nodeWorkerRecord, run func()) func() {
	return func() {
		normalStop := false
		defer func() {
			if r := recover(); r != nil {
				w.failNodeWorker(worker, ErrNodePanic)
			} else {
				normalStop = true
			}
			w.finishNodeWorker(worker, normalStop)
		}()

		run()
	}
}

func (w *Workspace) finishNodeWorker(worker nodeWorkerRecord, normalStop bool) {
	w.Lock()
	if normalStop {
		w.log.Debugf("node worker stopped worker=%d node=%d class=%s name=%q worker_name=%q", worker.ID, worker.Node, worker.Class, worker.Name, worker.WorkerName)
		w.enqueueWorkerNotification(NotificationWorkerStopped, worker.ID, w.workerSnapshotLocked(worker, ""))
	}
	workers := w.workers
	if workers != nil {
		delete(workers.alive, worker.ID)
	}
	w.Unlock()
	if workers != nil {
		workers.wg.Done()
	}
}

func (w *Workspace) failNodeWorker(worker nodeWorkerRecord, cause error) {
	w.Lock()
	defer w.Unlock()

	w.enqueueWorkerNotification(NotificationWorkerFailed, worker.ID, w.workerSnapshotLocked(worker, causeString(cause)))
	record, present := w.nodes.Get(worker.Node)
	if w.closed || !present || record == nil || record.Node == nil {
		w.log.Errf(
			"node worker failed after node was unavailable worker=%d node=%d class=%s name=%q worker_name=%q cause=%v",
			worker.ID,
			worker.Node,
			worker.Class,
			worker.Name,
			worker.WorkerName,
			cause,
		)
		return
	}
	w.log.Errf(
		"node worker failed; replacing node with placeholder worker=%d node=%d class=%s name=%q worker_name=%q cause=%v",
		worker.ID,
		worker.Node,
		record.Class,
		record.Name,
		worker.WorkerName,
		cause,
	)
	w.replaceFailedNodeWithPlaceholderLocked(worker.Node, record, workerFailureText(worker.ID, cause), true, true)
}

func workerFailureText(worker uint64, cause error) string {
	if cause == nil {
		return fmt.Sprintf("worker %d failed", worker)
	}
	return fmt.Sprintf("worker %d failed: %v", worker, cause)
}

func causeString(cause error) string {
	if cause == nil {
		return ""
	}
	return cause.Error()
}

// DebugOrphanWorkers returns live workers whose attached node no longer exists.
//
// This is a diagnostic helper for finding background work that outlived its
// owner node. The returned slice is detached from workspace state and may be
// stale immediately after the method returns.
func (w *Workspace) DebugOrphanWorkers() []WorkerSnapshot {
	w.Lock()
	defer w.Unlock()
	if w.closed {
		return nil
	}
	return w.workerSnapshotsLocked(true)
}

func (w *Workspace) workerSnapshotsLocked(orphanOnly bool) []WorkerSnapshot {
	if w.workers == nil || len(w.workers.alive) == 0 {
		return nil
	}
	workers := make([]WorkerSnapshot, 0, len(w.workers.alive))
	for _, worker := range w.workers.alive {
		snapshot := w.workerSnapshotLocked(worker, "")
		if orphanOnly && !snapshot.Orphan {
			continue
		}
		workers = append(workers, snapshot)
	}
	return workers
}

func (w *Workspace) workerSnapshotMapLocked() map[uint64]WorkerSnapshot {
	if w.workers == nil || len(w.workers.alive) == 0 {
		return map[uint64]WorkerSnapshot{}
	}
	workers := make(map[uint64]WorkerSnapshot, len(w.workers.alive))
	for id, worker := range w.workers.alive {
		workers[id] = w.workerSnapshotLocked(worker, "")
	}
	return workers
}

func (w *Workspace) workerSnapshotLocked(worker nodeWorkerRecord, failure string) WorkerSnapshot {
	_, present := w.nodes.Get(worker.Node)
	return WorkerSnapshot{
		ID:         worker.ID,
		Node:       worker.Node,
		Class:      worker.Class,
		Name:       worker.Name,
		WorkerName: worker.WorkerName,
		StartedAt:  worker.Start,
		Orphan:     !present,
		Error:      failure,
	}
}
