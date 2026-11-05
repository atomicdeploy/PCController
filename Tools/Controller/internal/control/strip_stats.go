package control

import "time"

// Host-observed complete frame ACKs, not a claim about physical LED refresh.
type StripStreamStats struct {
	Pixels        int    `json:"pixels"`
	RequestedFPS  int    `json:"requested_fps"`
	FramesAcked   uint64 `json:"frames_acked"`
	FramesFailed  uint64 `json:"frames_failed"`
	LastAckUnixMS int64  `json:"last_ack_unix_ms"`
	LastError     string `json:"last_error"`
}

func (s *OutputScheduler) beginStripStats(pixels, fps int) {
	s.mu.Lock()
	s.stripStats = StripStreamStats{Pixels: pixels, RequestedFPS: fps}
	s.mu.Unlock()
}
func (s *OutputScheduler) recordStripFrame(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err != nil {
		s.stripStats.FramesFailed++
		s.stripStats.LastError = err.Error()
	} else {
		s.stripStats.FramesAcked++
		s.stripStats.LastAckUnixMS = time.Now().UnixMilli()
		s.stripStats.LastError = ""
	}
}
