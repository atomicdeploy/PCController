package hostbridge

import (
	"context"
	"strings"
	"sync"
	"time"

	controller "pccontroller.local/controller"
	"pccontroller.local/controller/internal/hostui"
)

type notificationJob struct {
	key          string
	notification hostui.Notification
	priority     int
	message      *controller.Event
}

type notificationQueueStats struct {
	Pending   int
	Delivered uint64
	Coalesced uint64
	Dropped   uint64
}

// notificationQueue keeps expensive native toast delivery at one worker. A
// pending kind is replaced by its newest message, emergency jobs receive a
// short independent cooldown, and capacity always prefers higher priority.
type notificationQueue struct {
	mu                sync.Mutex
	capacity          int
	normalCooldown    time.Duration
	emergencyCooldown time.Duration
	now               func() time.Time
	wake              chan struct{}
	pending           map[string]notificationJob
	order             []string
	lastDelivered     map[string]time.Time
	delivered         uint64
	coalesced         uint64
	dropped           uint64
}

func newNotificationQueue(capacity int, normalCooldown, emergencyCooldown time.Duration) *notificationQueue {
	if capacity < 1 {
		capacity = 1
	}
	return &notificationQueue{
		capacity: capacity, normalCooldown: normalCooldown,
		emergencyCooldown: emergencyCooldown, now: time.Now,
		wake: make(chan struct{}, 1), pending: make(map[string]notificationJob),
		lastDelivered: make(map[string]time.Time),
	}
}

func (queue *notificationQueue) enqueue(job notificationJob) {
	job.key = strings.ToLower(strings.TrimSpace(job.key))
	if job.key == "" {
		job.key = "notification"
	}
	queue.mu.Lock()
	if _, exists := queue.pending[job.key]; exists {
		queue.pending[job.key] = job
		queue.coalesced++
		queue.mu.Unlock()
		queue.signal()
		return
	}
	cooldown := queue.normalCooldown
	if job.priority > 1 {
		cooldown = queue.emergencyCooldown
	}
	if last := queue.lastDelivered[job.key]; !last.IsZero() && queue.now().Sub(last) < cooldown {
		queue.coalesced++
		queue.mu.Unlock()
		return
	}
	if len(queue.order) >= queue.capacity {
		replace := -1
		for index, key := range queue.order {
			candidate := queue.pending[key]
			if candidate.priority <= job.priority && (replace < 0 || candidate.priority < queue.pending[queue.order[replace]].priority) {
				replace = index
			}
		}
		if replace < 0 {
			queue.dropped++
			queue.mu.Unlock()
			return
		}
		delete(queue.pending, queue.order[replace])
		queue.order = append(queue.order[:replace], queue.order[replace+1:]...)
		queue.dropped++
	}
	queue.pending[job.key] = job
	queue.order = append(queue.order, job.key)
	queue.mu.Unlock()
	queue.signal()
}

func (queue *notificationQueue) signal() {
	select {
	case queue.wake <- struct{}{}:
	default:
	}
}

func (queue *notificationQueue) pop() (notificationJob, bool) {
	queue.mu.Lock()
	defer queue.mu.Unlock()
	if len(queue.order) == 0 {
		return notificationJob{}, false
	}
	key := queue.order[0]
	queue.order = queue.order[1:]
	job := queue.pending[key]
	delete(queue.pending, key)
	return job, true
}

func (queue *notificationQueue) next(ctx context.Context) (notificationJob, bool) {
	for {
		if job, ok := queue.pop(); ok {
			return job, true
		}
		select {
		case <-ctx.Done():
			return notificationJob{}, false
		case <-queue.wake:
		}
	}
}

func (queue *notificationQueue) complete(key string) {
	queue.mu.Lock()
	queue.lastDelivered[key] = queue.now()
	queue.delivered++
	queue.mu.Unlock()
}

func (queue *notificationQueue) stats() notificationQueueStats {
	queue.mu.Lock()
	defer queue.mu.Unlock()
	return notificationQueueStats{
		Pending: len(queue.order), Delivered: queue.delivered,
		Coalesced: queue.coalesced, Dropped: queue.dropped,
	}
}

func notificationPriority(kind string) int {
	kind = strings.ToLower(strings.TrimSpace(kind))
	if kind == "error" || strings.Contains(kind, "fault") || strings.Contains(kind, "hot") ||
		strings.Contains(kind, "motion") || kind == "warning.door-open-running" {
		return 2
	}
	return 1
}
