package client

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type fakeSound struct {
	mu                  sync.Mutex
	volume              int
	muted               bool
	volumeErr, mutedErr error
	block               chan struct{} // reads wait until it is closed
	sets                []string
}

func (f *fakeSound) wait() {
	if f.block != nil {
		<-f.block
	}
}
func (f *fakeSound) Volume() (int, error) {
	f.wait()
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.volume, f.volumeErr
}
func (f *fakeSound) Muted() (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.muted, f.mutedErr
}
func (f *fakeSound) SetVolume(percent int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.volume = percent
	f.sets = append(f.sets, "volume")
	return nil
}
func (f *fakeSound) SetMuted(muted bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.muted = muted
	f.sets = append(f.sets, "muted")
	return nil
}
func (f *fakeSound) student(volume int, muted bool) {
	f.mu.Lock()
	f.volume, f.muted = volume, muted
	f.mu.Unlock()
}
func (f *fakeSound) state() (int, bool, []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.volume, f.muted, append([]string(nil), f.sets...)
}

func soundOptions(control SoundControl) SoundOptions {
	return SoundOptions{Control: control, Interval: 20 * time.Millisecond, Timeout: 200 * time.Millisecond}
}
func intPtr(v int) *int    { return &v }
func boolPtr(v bool) *bool { return &v }

// examSound returns a manager that has recorded the student's own settings and been told the exam needs minimum percent.
func examSound(control SoundControl, minimum int) *soundManager {
	m := newSoundManager(soundOptions(control))
	m.check()
	m.activate(minimum)
	m.check()
	return m
}

func TestSoundIsOnlyWatchedUntilTheExamSaysWhatItNeeds(t *testing.T) {
	control := &fakeSound{volume: 20, muted: true}
	m := newSoundManager(soundOptions(control))
	m.check()
	m.check()
	if err := m.restore(); err != nil {
		t.Fatal(err)
	}
	if volume, muted, sets := control.state(); len(sets) != 0 || volume != 20 || !muted {
		t.Fatalf("sound changed before login: %d muted=%v %v", volume, muted, sets)
	}
	if got := m.report(); !reflect.DeepEqual(got, SoundReport{Volume: intPtr(20), Muted: boolPtr(true)}) {
		t.Fatalf("the login page cannot show the level: %+v", got)
	}
}

func TestSoundIsRaisedWhenTheExamStartsAndRestoredToTheStudentsOriginal(t *testing.T) {
	control := &fakeSound{volume: 20, muted: true}
	m := examSound(control, 80)
	if volume, muted, _ := control.state(); volume != 80 || muted {
		t.Fatalf("exam sound was not made audible: %d muted=%v", volume, muted)
	}
	// The report is what the student had, not what the application changed it to.
	if got := m.report(); *got.Volume != 20 || !*got.Muted {
		t.Fatalf("report hid the original level: %d %v", *got.Volume, *got.Muted)
	}
	control.student(95, false) // a later reading must not replace the remembered original
	m.check()
	if err := m.restore(); err != nil {
		t.Fatal(err)
	}
	if volume, muted, _ := control.state(); volume != 20 || !muted {
		t.Fatalf("original state not restored: %d muted=%v", volume, muted)
	}
	_, _, before := control.state()
	if err := m.restore(); err != nil {
		t.Fatal(err)
	}
	m.check()
	if _, _, after := control.state(); len(after) != len(before) {
		t.Fatal("sound changed again after restore")
	}
}

func TestSoundAlreadyAudibleIsNeverTouched(t *testing.T) {
	control := &fakeSound{volume: 85}
	m := examSound(control, 80)
	control.student(70, false) // the student's own change while the exam runs is below the minimum, so it is raised...
	m.check()
	control.student(90, false) // ...but this one is fine and stays
	m.check()
	if err := m.restore(); err != nil {
		t.Fatal(err)
	}
	if volume, _, _ := control.state(); volume != 85 {
		t.Fatalf("expected the original 85 back, got %d", volume)
	}
	control = &fakeSound{volume: 85}
	m = examSound(control, 80)
	control.student(90, false)
	m.check()
	if err := m.restore(); err != nil {
		t.Fatal(err)
	}
	if volume, _, sets := control.state(); len(sets) != 0 || volume != 90 {
		t.Fatalf("untouched sound was written: %v %d", sets, volume)
	}
}

func TestSoundRestoresOnlyWhatWasChanged(t *testing.T) {
	control := &fakeSound{volume: 90, muted: true}
	m := examSound(control, 80)
	control.student(60, false)
	if err := m.restore(); err != nil {
		t.Fatal(err)
	}
	volume, muted, sets := control.state()
	if volume != 60 || !muted || !reflect.DeepEqual(sets, []string{"muted", "muted"}) {
		t.Fatalf("volume was restored although it was never changed: %d muted=%v %v", volume, muted, sets)
	}
}

func TestSoundKeepsTheExamAudibleWhenTheStudentLowersItAgain(t *testing.T) {
	control := &fakeSound{volume: 90}
	m := examSound(control, 80)
	control.student(10, true)
	m.check()
	if got := m.report(); *got.Volume != 10 || !*got.Muted {
		t.Fatalf("the proctor could not see the student lowering the sound: %+v", got)
	}
	if volume, muted, _ := control.state(); volume != 80 || muted {
		t.Fatalf("lowered sound was left inaudible: %d muted=%v", volume, muted)
	}
	if err := m.restore(); err != nil {
		t.Fatal(err)
	}
	if volume, muted, _ := control.state(); volume != 90 || muted {
		t.Fatalf("restored to the lowered value instead of the original: %d muted=%v", volume, muted)
	}
}

func TestSoundWithAZeroMinimumOrAnInvalidOneOnlyReports(t *testing.T) {
	for _, minimum := range []int{0, -1, 101} {
		control := &fakeSound{volume: 5, muted: true}
		m := examSound(control, minimum)
		if err := m.restore(); err != nil {
			t.Fatal(err)
		}
		if _, _, sets := control.state(); len(sets) != 0 {
			t.Fatalf("minimum %d changed the sound: %v", minimum, sets)
		}
		if got := m.report(); got.Volume == nil || *got.Volume != 5 {
			t.Fatalf("report missing: %+v", got)
		}
	}
}

func TestActivationWakesTheCheckLoopImmediately(t *testing.T) {
	control := &fakeSound{volume: 20}
	options := soundOptions(control)
	options.Interval = time.Hour
	m := newSoundManager(options)
	m.check()
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { m.run(ctx); close(done) }()
	m.activate(80)
	eventually(t, "volume raised without waiting for the next interval", func() bool { v, _, _ := control.state(); return v == 80 })
	cancel()
	<-done
}

func TestSoundReadFailuresNeverChangeOrInventValues(t *testing.T) {
	failures := 0
	control := &fakeSound{volume: 20, muted: true, volumeErr: errors.New("no volume"), mutedErr: errors.New("no mute")}
	options := soundOptions(control)
	options.OnError = func(string, error) { failures++ }
	m := newSoundManager(options)
	m.check()
	m.activate(80)
	m.check()
	if err := m.restore(); err != nil {
		t.Fatal(err)
	}
	if _, _, sets := control.state(); len(sets) != 0 || failures != 4 {
		t.Fatalf("unknown sound was modified: %v failures=%d", sets, failures)
	}
	if got := m.report(); got.Volume != nil || got.Muted != nil {
		t.Fatalf("unknown values were reported: %+v", got)
	}
}

func TestSoundWithOnlyMuteReadableStillUnmutes(t *testing.T) {
	control := &fakeSound{volume: 20, muted: true, volumeErr: errors.New("fixed-volume device")}
	m := examSound(control, 80)
	got := m.report()
	if got.Volume != nil || got.Muted == nil || !*got.Muted {
		t.Fatalf("partial report wrong: %+v", got)
	}
	if _, muted, sets := control.state(); muted || !reflect.DeepEqual(sets, []string{"muted"}) {
		t.Fatalf("mute not handled: muted=%v %v", muted, sets)
	}
	if err := m.restore(); err != nil {
		t.Fatal(err)
	}
	if _, muted, _ := control.state(); !muted {
		t.Fatal("mute not restored")
	}
}

func TestReportedVolumeIsClampedAndExpiresWhenChecksStop(t *testing.T) {
	control := &fakeSound{volume: 140}
	options := soundOptions(control)
	options.Interval = 10 * time.Millisecond
	m := newSoundManager(options)
	if got := m.report(); got.Volume != nil {
		t.Fatal("report existed before the first check")
	}
	m.check()
	if got := m.report(); *got.Volume != 100 {
		t.Fatalf("over-amplified volume not clamped: %d", *got.Volume)
	}
	time.Sleep(40 * time.Millisecond)
	if got := m.report(); got.Volume != nil || got.Muted != nil {
		t.Fatalf("stale report was sent: %+v", got)
	}
}

func TestHungSoundToolNeverBlocksStartupOrRestore(t *testing.T) {
	control := &fakeSound{volume: 20, block: make(chan struct{})}
	defer close(control.block)
	started := time.Now()
	m := examSound(control, 80)
	if err := m.restore(); err != nil {
		t.Fatal(err)
	}
	if time.Since(started) > time.Second {
		t.Fatal("a hung audio tool stalled the application")
	}
	if got := m.report(); got.Volume != nil {
		t.Fatalf("hung tool produced a value: %+v", got)
	}
}

func TestSoundOptionsAreValidated(t *testing.T) {
	for _, bad := range []SoundOptions{{Control: &fakeSound{}, Timeout: time.Second}, {Control: &fakeSound{}, Interval: time.Second}} {
		o := DefaultOptions()
		o.Screenshots.Required = false
		o.Sound = bad
		if _, err := New(o); err == nil {
			t.Fatalf("accepted %+v", bad)
		}
	}
	o := DefaultOptions()
	o.Screenshots.Required = false
	o.Sound = SoundOptions{} // no control: nothing to validate or run
	s, err := New(o)
	if err != nil {
		t.Fatal(err)
	}
	_ = s.ServiceShutdown()
}

func heartbeatWith(f *fixture, volume int, muted bool) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, r := range f.telemetry {
		if r.path == "/heartbeat" && r.volume != nil && *r.volume == volume && r.muted != nil && *r.muted == muted {
			return true
		}
	}
	return false
}
func eventually(t *testing.T, what string, condition func() bool) {
	t.Helper()
	until := time.Now().Add(3 * time.Second)
	for !condition() {
		if time.Now().After(until) {
			t.Fatalf("never happened: %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestTheLoginPageShowsTheCurrentLevelWithoutChangingIt(t *testing.T) {
	f := newFixture(t)
	control := &fakeSound{volume: 35, muted: true}
	s := newClient(t, f, func(o *Options) { o.Sound = soundOptions(control) })
	view := s.Snapshot()
	if view.Sound.Volume == nil || *view.Sound.Volume != 35 || view.Sound.Muted == nil || !*view.Sound.Muted {
		t.Fatalf("view does not show the current level: %+v", view.Sound)
	}
	control.student(60, false)
	waitView(t, s, func(v View) bool {
		return v.Sound.Volume != nil && *v.Sound.Volume == 60 && v.Sound.Muted != nil && !*v.Sound.Muted
	})
	if _, _, sets := control.state(); len(sets) != 0 {
		t.Fatalf("the application changed the sound before login: %v", sets)
	}
}

func TestLoginActivatesTheExamsVolumeAndAnOlderServerDoesNot(t *testing.T) {
	f := newFixture(t)
	f.minVolume = intPtr(80)
	control := &fakeSound{volume: 20, muted: true}
	s := newClient(t, f, func(o *Options) { o.Sound = soundOptions(control) })
	if _, _, sets := control.state(); len(sets) != 0 {
		t.Fatalf("changed before login: %v", sets)
	}
	loginClient(t, s, f)
	eventually(t, "exam volume applied", func() bool { v, m, _ := control.state(); return v == 80 && !m })

	older := newFixture(t) // an older server sends no volume at all
	quiet := &fakeSound{volume: 20, muted: true}
	s = newClient(t, older, func(o *Options) { o.Sound = soundOptions(quiet) })
	loginClient(t, s, older)
	time.Sleep(100 * time.Millisecond)
	if _, _, sets := quiet.state(); len(sets) != 0 {
		t.Fatalf("an exam that advertises no volume changed the sound: %v", sets)
	}
}

func TestHeartbeatCarriesTheVolumeAndTheExamRestoresItWhenFinalized(t *testing.T) {
	f := newFixture(t)
	f.minVolume = intPtr(80)
	control := &fakeSound{volume: 20, muted: true}
	var released atomic.Int32
	s := monitorClient(t, f, func(o *Options) {
		o.Sound = soundOptions(control)
		o.Sound.Interval = 100 * time.Millisecond // several heartbeats per check
		o.OnCompletion = func() { released.Add(1) }
	})
	loginClient(t, s, f)
	eventually(t, "exam volume applied", func() bool { v, m, _ := control.state(); return v == 80 && !m })
	eventually(t, "heartbeat with the audible level", func() bool { return heartbeatWith(f, 80, false) })
	control.student(10, true) // the student silences the exam
	eventually(t, "heartbeat showing the student's own setting", func() bool { return heartbeatWith(f, 10, true) })
	eventually(t, "sound raised again", func() bool { v, m, _ := control.state(); return v == 80 && !m })
	f.mu.Lock()
	f.status.State = "submitted"
	f.status.SecondsRemaining = 0
	f.mu.Unlock()
	waitView(t, s, func(v View) bool { return v.Phase == "complete" && released.Load() == 1 })
	if volume, muted, _ := control.state(); volume != 20 || !muted {
		t.Fatalf("sound was not restored when the exam ended: %d muted=%v", volume, muted)
	}
}

func TestClosingTheApplicationRestoresTheSound(t *testing.T) {
	f := newFixture(t)
	f.minVolume = intPtr(80)
	control := &fakeSound{volume: 15}
	s := newClient(t, f, func(o *Options) { o.Sound = soundOptions(control) })
	loginClient(t, s, f)
	eventually(t, "exam volume applied", func() bool { v, _, _ := control.state(); return v == 80 })
	if err := s.ServiceShutdown(); err != nil {
		t.Fatal(err)
	}
	if volume, muted, _ := control.state(); volume != 15 || muted {
		t.Fatalf("closing did not restore the sound: %d muted=%v", volume, muted)
	}
}

func TestHeartbeatWithoutASoundControlSendsNoVolume(t *testing.T) {
	f := newFixture(t)
	s := monitorClient(t, f, nil)
	loginClient(t, s, f)
	waitMonitor(t, s, func(m MonitoringView) bool { return m.HeartbeatAt != nil })
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, r := range f.telemetry {
		if r.path == "/heartbeat" && (r.volume != nil || r.muted != nil) {
			t.Fatalf("volume invented without a control: %+v", r)
		}
	}
}
