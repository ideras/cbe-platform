package domain

import "time"

const TimelineTimeLayout = "2006-01-02T15:04:05.000000000Z"

type ProctorRecord struct {
	Session      Session
	StudentName  string
	RosterActive bool
}

type TimelineWatermark struct {
	EventID   int64 `json:"event_log_id"`
	VersionID int64 `json:"version_id"`
}
type TimelinePosition struct {
	Time string `json:"sort_time"`
	Kind string `json:"kind"`
	ID   int64  `json:"id"`
}

type ScreenshotMetadata struct {
	ScreenshotInfo
	ID         int64 `json:"screenshot_id"`
	EventLogID int64 `json:"event_log_id"`
	// CaptureIndex numbers the captures of one focus loss: 0 at the threshold, then one per capture interval.
	CaptureIndex int       `json:"capture_index"`
	ContentType  string    `json:"content_type"`
	CapturedAt   time.Time `json:"captured_at"`
}

type HistoryRecord struct {
	Event       *Event
	Version     *Version
	Screenshots []ScreenshotMetadata
}

func (r HistoryRecord) Position() TimelinePosition {
	if r.Version != nil {
		return TimelinePosition{Time: r.Version.SavedAt.UTC().Format(TimelineTimeLayout), Kind: "answer_version", ID: r.Version.ID}
	}
	return TimelinePosition{Time: r.Event.OccurredAt.UTC().Format(TimelineTimeLayout), Kind: "event", ID: r.Event.ID}
}

// CompareTimeline orders time, kind (answers before events), then the kind's numeric ID.
func CompareTimeline(a, b TimelinePosition) int {
	if a.Time < b.Time {
		return -1
	}
	if a.Time > b.Time {
		return 1
	}
	if a.Kind < b.Kind {
		return -1
	}
	if a.Kind > b.Kind {
		return 1
	}
	if a.ID < b.ID {
		return -1
	}
	if a.ID > b.ID {
		return 1
	}
	return 0
}
