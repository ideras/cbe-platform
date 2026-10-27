package client

import (
	"os"
	"testing"

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
