package traymenu

// popupLifecycleState is the platform-independent presentation policy for a
// custom popup. Its zero value is deliberately cold: callers must prepare and
// commit a complete first frame before activate can reveal the window.
type popupLifecycleState uint8

const (
	popupLifecycleCold popupLifecycleState = iota
	popupLifecyclePreparing
	popupLifecycleHiddenPrepared
	popupLifecycleActive
	popupLifecycleHiding
	popupLifecycleHiddenReusable
	popupLifecycleDestroyed
)

// popupCloseChoice tells the platform host whether a completed exit should
// retain a hidden prepared window or release it.
type popupCloseChoice uint8

const (
	popupCloseNone popupCloseChoice = iota
	popupCloseHide
	popupCloseDestroy
)

// beginPreparation starts an invisible render pass. A reusable window must
// pass through this gate again so stale menu content is never revealed.
func (s *popupLifecycleState) beginPreparation() bool {
	if s == nil {
		return false
	}
	switch *s {
	case popupLifecycleCold, popupLifecycleHiddenReusable:
		*s = popupLifecyclePreparing
		return true
	default:
		return false
	}
}

// commitPreparation records that the host surface and layered first frame
// have both been committed while the popup is still hidden.
func (s *popupLifecycleState) commitPreparation() bool {
	if s == nil || *s != popupLifecyclePreparing {
		return false
	}
	*s = popupLifecycleHiddenPrepared
	return true
}

// activate opens the visibility gate only after preparation was committed.
func (s *popupLifecycleState) activate() bool {
	if s == nil || *s != popupLifecycleHiddenPrepared {
		return false
	}
	*s = popupLifecycleActive
	return true
}

func (s *popupLifecycleState) beginHiding() bool {
	if s == nil || *s != popupLifecycleActive {
		return false
	}
	*s = popupLifecycleHiding
	return true
}

// completeHiding applies the configured cache policy after the exit frame.
func (s *popupLifecycleState) completeHiding(keepReusable bool) popupCloseChoice {
	if s == nil || *s != popupLifecycleHiding {
		return popupCloseNone
	}
	if keepReusable {
		*s = popupLifecycleHiddenReusable
		return popupCloseHide
	}
	*s = popupLifecycleDestroyed
	return popupCloseDestroy
}

// invalidate makes preparation or presentation failures terminal for this
// window. A later invocation may create a clean lifecycle and use fallback.
func (s *popupLifecycleState) invalidate() popupCloseChoice {
	if s == nil || *s == popupLifecycleDestroyed {
		return popupCloseNone
	}
	*s = popupLifecycleDestroyed
	return popupCloseDestroy
}

func (s popupLifecycleState) visible() bool {
	return s == popupLifecycleActive || s == popupLifecycleHiding
}

func (s popupLifecycleState) reusable() bool {
	return s == popupLifecycleHiddenReusable
}
