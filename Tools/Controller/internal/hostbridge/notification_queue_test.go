package hostbridge

import (
	"testing"
	"time"

	"pccontroller.local/controller/internal/hostui"
)

func queued(kind, body string, priority int) notificationJob {
	return notificationJob{
		key: kind, priority: priority,
		notification: hostui.Notification{Title: kind, Body: body},
	}
}

func TestNotificationQueueCoalescesNewestPendingKind(t *testing.T) {
	queue := newNotificationQueue(4, 3*time.Second, 500*time.Millisecond)
	queue.enqueue(queued("relay", "first", 1))
	queue.enqueue(queued("relay", "newest", 1))
	job, ok := queue.pop()
	if !ok || job.notification.Body != "newest" {
		t.Fatalf("coalesced job=%#v ok=%t", job, ok)
	}
	stats := queue.stats()
	if stats.Coalesced != 1 || stats.Pending != 0 {
		t.Fatalf("stats=%+v", stats)
	}
}

func TestNotificationQueuePrefersEmergencyAtCapacity(t *testing.T) {
	queue := newNotificationQueue(2, 3*time.Second, 500*time.Millisecond)
	queue.enqueue(queued("rf", "one", 1))
	queue.enqueue(queued("door", "two", 1))
	queue.enqueue(queued("motion.fault", "emergency", 2))
	first, _ := queue.pop()
	second, _ := queue.pop()
	if first.key != "door" || second.key != "motion.fault" {
		t.Fatalf("priority order=(%q,%q)", first.key, second.key)
	}
	if stats := queue.stats(); stats.Dropped != 1 {
		t.Fatalf("stats=%+v", stats)
	}
}

func TestNotificationQueueUsesSeparateCooldowns(t *testing.T) {
	now := time.Unix(100, 0)
	queue := newNotificationQueue(4, 3*time.Second, 500*time.Millisecond)
	queue.now = func() time.Time { return now }
	queue.complete("relay")
	queue.complete("motion.fault")
	queue.enqueue(queued("relay", "routine duplicate", 1))
	queue.enqueue(queued("motion.fault", "emergency duplicate", 2))
	if stats := queue.stats(); stats.Pending != 0 || stats.Coalesced != 2 {
		t.Fatalf("initial cooldown stats=%+v", stats)
	}
	now = now.Add(time.Second)
	queue.enqueue(queued("relay", "still cooling", 1))
	queue.enqueue(queued("motion.fault", "accepted", 2))
	job, ok := queue.pop()
	if !ok || job.key != "motion.fault" {
		t.Fatalf("post-cooldown job=%#v ok=%t", job, ok)
	}
}
