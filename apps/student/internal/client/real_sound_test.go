package client

import (
	"context"
	"os"
	"testing"
	"time"

	"exam-platform/apps/student/internal/sysvolume"
)

var _ SoundControl = sysvolume.System{}

// Changes the real system volume (briefly, then puts it back), so it only runs when asked for: EXAM_REAL_SOUND=1.
func TestRealSystemSoundIsRaisedAndRestoredByTheClient(t *testing.T) {
	if os.Getenv("EXAM_REAL_SOUND") != "1" {
		t.Skip("set EXAM_REAL_SOUND=1 to change and restore the real system volume")
	}
	system := sysvolume.System{}
	volume, err := system.Volume()
	if err != nil {
		t.Skipf("system volume unavailable: %v", err)
	}
	muted, err := system.Muted()
	if err != nil {
		t.Skipf("system mute unavailable: %v", err)
	}
	t.Logf("student's own setting: %d%% muted=%v", volume, muted)
	defer func() { // whatever happens, the student's own setting comes back
		if volume <= 100 {
			_ = system.SetVolume(volume)
		}
		_ = system.SetMuted(muted)
	}()

	if err := system.SetVolume(20); err != nil {
		t.Fatal(err)
	}
	if err := system.SetMuted(true); err != nil {
		t.Fatal(err)
	}
	options := DefaultOptions()
	options.Screenshots.Required = false
	options.Sound.Control = system
	s, err := New(options)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := system.Volume(); err != nil || got != 20 {
		t.Errorf("the application changed the sound before login: %d, %v", got, err)
	}
	s.sound.activate(80) // what a login to an exam with an 80% setting does
	eventually(t, "exam volume applied", func() bool {
		v, e1 := system.Volume()
		m, e2 := system.Muted()
		return e1 == nil && e2 == nil && v == 80 && !m
	})
	if err := s.ServiceShutdown(); err != nil {
		t.Fatal(err)
	}
	if got, err := system.Volume(); err != nil || got != 20 {
		t.Errorf("restored volume = %d, %v; want 20", got, err)
	}
	if got, err := system.Muted(); err != nil || !got {
		t.Errorf("restored mute = %v, %v; want true", got, err)
	}
}

// Measures how fast the platform tells the application that the volume changed, which is what the exam's reaction time
// to a lowered volume is made of. It moves the volume by one percent and back, so it only runs when asked for
// (EXAM_REAL_SOUND=1). Only Linux has a subscription; elsewhere it skips.
func TestRealPlatformVolumeChangeNotificationReactionTime(t *testing.T) {
	if os.Getenv("EXAM_REAL_SOUND") != "1" {
		t.Skip("set EXAM_REAL_SOUND=1 to measure the real volume change notification")
	}
	system := sysvolume.System{}
	volume, err := system.Volume()
	if err != nil {
		t.Skipf("system volume unavailable: %v", err)
	}
	defer func() { _ = system.SetVolume(volume) }()
	nudged := volume - 1
	if volume < 1 {
		nudged = volume + 1
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	changes := make(chan time.Time, 16)
	ended := make(chan error, 1)
	go func() { ended <- system.Watch(ctx, func() { changes <- time.Now() }) }()
	time.Sleep(500 * time.Millisecond) // the subscription is open before anything is written
	var total time.Duration
	const writes = 6
	for write := 0; write < writes; write++ {
		for len(changes) > 0 {
			<-changes
		}
		target := nudged
		if write%2 == 1 {
			target = volume
		}
		started := time.Now()
		if err := system.SetVolume(target); err != nil {
			t.Fatal(err)
		}
		select {
		case at := <-changes:
			total += at.Sub(started)
			t.Logf("write %d (%d%%): notified %v after the write began", write+1, target, at.Sub(started))
		case err := <-ended:
			t.Skipf("no change notification on this platform: %v", err)
		case <-time.After(3 * time.Second):
			t.Fatal("the system never reported the volume write")
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Logf("average: the notification arrived %v after the write began", total/writes)
	cancel()
	select {
	case <-ended:
	case <-time.After(3 * time.Second):
		t.Fatal("the subscription was left running")
	}
}
