package client

import "time"

// TimeNotice is a sequenced, memory-only change report. Repeated snapshots do not
// replay it; an initial login report is not a proctor change.
type TimeNotice struct {
	Sequence     uint64     `json:"sequence"`
	Kind         string     `json:"kind"`
	AddedSeconds int64      `json:"added_seconds,omitempty"`
	EndsAt       *time.Time `json:"ends_at"`
}

func ownTimes(status Status) (time.Time, time.Time) {
	if !status.OwnDeadlineAt.IsZero() && !status.OwnGraceUntil.IsZero() {
		return status.OwnDeadlineAt, status.OwnGraceUntil
	}
	return status.DeadlineAt, status.GraceUntil
}

func sameEnd(a, b *time.Time) bool {
	return a == nil && b == nil || a != nil && b != nil && a.Equal(*b)
}

// Called under s.mu. The request start includes network time in the report age,
// conservatively avoiding a countdown that overstates the server's observation.
func (s *Service) observeStatus(status Status, observedAt time.Time) {
	previous := s.status
	if previous != nil && !finalStatus(status.State) {
		notices := []TimeNotice{}
		post := func(kind string, seconds int64) {
			s.timeNoticeSequence++
			notices = append(notices, TimeNotice{s.timeNoticeSequence, kind, seconds, status.EndsAt})
		}
		if !sameEnd(previous.EndsAt, status.EndsAt) {
			post("end_time", 0)
		}
		// Unlock moves both bounds; changing an end cap may also move both. Neither
		// is a grace extension and must not produce a duplicate toast.
		oldDeadline, oldGrace := ownTimes(*previous)
		deadline, grace := ownTimes(status)
		hasOwnTimes := !previous.OwnGraceUntil.IsZero() && !status.OwnGraceUntil.IsZero()
		if (hasOwnTimes || sameEnd(previous.EndsAt, status.EndsAt)) && oldDeadline.Equal(deadline) && grace.After(oldGrace) && s.locked == nil && previous.Locked == nil {
			post("grace", int64(grace.Sub(oldGrace)/time.Second))
		}
		if len(notices) > 0 {
			s.timeNotices = notices
		}
	}
	s.status = &status
	s.statusObservedAt = observedAt
	s.statusObservation++
}
