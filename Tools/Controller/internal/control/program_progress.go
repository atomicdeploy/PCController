package control

import (
	"context"
	"errors"
	"strconv"
	"sync/atomic"
	"time"

	"pccontroller.local/controller/internal/programmer"
)

var directProgrammingSequence atomic.Uint64

// Artifact operations already own the observer and durable status. Direct
// commands use the same update event family without duplicating that observer.
func directProgrammingProgress(ctx context.Context, runtime *Runtime, kind string) (context.Context, func(error)) {
	if runtime == nil || programmer.HasProgress(ctx) {
		return ctx, func(error) {}
	}
	started := time.Now().UTC()
	stageStarted := started
	stage := "preflight"
	operationID := programOperationID(ctx)
	if operationID == "" {
		operationID = "program-" + strconv.FormatInt(started.UnixMilli(), 10) + "-" + strconv.FormatUint(directProgrammingSequence.Add(1), 10)
	}
	publish := func(state string, progress programmer.Progress, failure error) {
		now := time.Now().UTC()
		if progress.Stage != stage {
			stage, stageStarted = progress.Stage, now
		}
		metadata := map[string]string{
			"operation_id": operationID, "kind": kind, "state": state,
			"stage": stage, "detail": progress.Detail,
			"progress_percent": strconv.Itoa(progress.Percent),
			"progress_known":   strconv.FormatBool(progress.Percent >= 0),
			"started_at":       started.Format(time.RFC3339Nano),
			"stage_started_at": stageStarted.Format(time.RFC3339Nano),
			"updated_at":       now.Format(time.RFC3339Nano),
		}
		if failure != nil {
			metadata["error"] = failure.Error()
			metadata["error_code"] = "programming_failed"
			if errors.Is(failure, programmer.ErrToolchainUnavailable) {
				metadata["error_code"] = "toolchain_unavailable"
				metadata["bootloader_outcome"] = "not-attempted"
			}
		}
		runtime.PublishStructuredEvent(Event{Kind: "update." + state, Stream: EventStreamActivity, Text: progress.Detail, Metadata: metadata})
	}
	ctx = programmer.WithProgress(ctx, func(progress programmer.Progress) { publish("running", progress, nil) })
	programmer.ReportProgress(ctx, programmer.Progress{Stage: stage, Percent: -1})
	return ctx, func(err error) {
		if err != nil {
			publish("failed", programmer.Progress{Stage: stage, Percent: -1, Detail: err.Error()}, err)
			return
		}
		publish("completed", programmer.Progress{Stage: "completed", Percent: 100, Detail: "Programming completed"}, nil)
	}
}
