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
	setErr              error         // writes fail with it
	block               chan struct{} // reads wait until it is closed
	sets                []string
	reads               int // volume reads
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
	f.reads++
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
	if f.setErr != nil {
		return f.setErr
	}
	f.volume = percent
	f.sets = append(f.sets, "volume")
	return nil
}
func (f *fakeSound) SetMuted(muted bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.setErr != nil {
		return f.setErr
	}
	f.muted = muted
	f.sets = append(f.sets, "muted")
	return nil
}
func (f *fakeSound) student(volume int, muted bool) {
	f.mu.Lock()
	f.volume, f.muted = volume, muted
	f.mu.Unlock()
}
func (f *fakeSound) readCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.reads
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

func noticeOf(t *testing.T, m *soundManager) SoundNotice {
	t.Helper()
	notice := m.latestNotice()
	if notice == nil {
		t.Fatal("no volume notice")
	}
	return *notice
}

func TestFirstCorrectionAfterLoginIsInitialAndLaterDropsAreNot(t *testing.T) {
	control := &fakeSound{volume: 20, muted: true}
	m := examSound(control, 80)
	first := noticeOf(t, m)
	if first.Kind != "restored" || !first.Initial || first.Minimum != 80 || first.Sequence != 1 || first.Volume == nil || *first.Volume != 20 || first.Muted == nil || !*first.Muted {
		t.Fatalf("login adjustment: %+v", first)
	}
	m.check()
	m.check()
	if got := noticeOf(t, m); got.Sequence != 1 {
		t.Fatalf("a healthy sound produced a notice: %+v", got)
	}
	control.student(30, false)
	m.check()
	later := noticeOf(t, m)
	if later.Kind != "restored" || later.Initial || later.Sequence != 2 || *later.Volume != 30 {
		t.Fatalf("a drop during the exam: %+v", later)
	}
	control.student(30, false)
	m.check()
	if got := noticeOf(t, m); got.Sequence != 3 {
		t.Fatalf("a new drop after recovery was not announced: %+v", got)
	}
}

func TestSoundAtOrAboveTheMinimumOrWithoutOneProducesNoNotice(t *testing.T) {
	for _, tc := range []struct{ volume, minimum int }{{80, 80}, {95, 80}, {5, 0}} {
		control := &fakeSound{volume: tc.volume}
		m := examSound(control, tc.minimum)
		if m.latestNotice() != nil {
			t.Fatalf("notice for %+v", tc)
		}
		if volume, _, sets := control.state(); volume != tc.volume || len(sets) != 0 {
			t.Fatalf("%+v: sound changed to %d %v", tc, volume, sets)
		}
	}
}

func TestFailedCorrectionIsAnErrorAnnouncedOncePerEpisode(t *testing.T) {
	control := &fakeSound{volume: 90}
	m := examSound(control, 80)
	control.student(10, false)
	control.mu.Lock()
	control.setErr = errors.New("device refused")
	control.mu.Unlock()
	for range 4 {
		m.check()
	}
	failed := noticeOf(t, m)
	if failed.Kind != "failed" || failed.Sequence != 1 || failed.Minimum != 80 || *failed.Volume != 10 {
		t.Fatalf("failed write: %+v", failed)
	}
	control.mu.Lock()
	control.setErr = nil
	control.mu.Unlock()
	m.check() // the episode ends with a successful correction, which is announced as one
	if got := noticeOf(t, m); got.Kind != "restored" || got.Sequence != 2 || got.Initial {
		t.Fatalf("recovery after a failure: %+v", got)
	}
}

func TestACorrectionTheSystemDoesNotConfirmIsNotReportedAsSuccess(t *testing.T) {
	// The write is accepted, but the level read back is still low: the application must not claim it restored it.
	control := &stubbornSound{fakeSound: fakeSound{volume: 90}}
	m := examSound(control, 80)
	control.student(10, false)
	control.stuck = true
	m.check()
	if got := noticeOf(t, m); got.Kind != "failed" {
		t.Fatalf("an unconfirmed correction was announced as a success: %+v", got)
	}
}

type stubbornSound struct {
	fakeSound
	stuck bool
}

func (s *stubbornSound) SetVolume(percent int) error {
	if s.stuck {
		return nil // accepted, ignored
	}
	return s.fakeSound.SetVolume(percent)
}

func TestUnreadableSoundIsNeverAnnounced(t *testing.T) {
	control := &fakeSound{volume: 20, muted: true, volumeErr: errors.New("no volume"), mutedErr: errors.New("no mute")}
	m := examSound(control, 80)
	if m.latestNotice() != nil {
		t.Fatalf("a failed read produced a notice: %+v", m.latestNotice())
	}
}

func TestEveryHeartbeatAsksForAFreshCheck(t *testing.T) {
	control := &fakeSound{volume: 90}
	options := soundOptions(control)
	options.Interval = time.Hour // only the heartbeats can cause a check
	m := newSoundManager(options)
	m.check()
	m.activate(80)
	m.check()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan struct{})
	go func() { m.run(ctx); close(done) }()
	defer func() { cancel(); <-done }()
	before := control.readCount()
	control.student(30, false)
	got := m.refresh(ctx)
	if got.Volume == nil || *got.Volume != 30 {
		t.Fatalf("the heartbeat did not see the level it asked about: %+v", got)
	}
	if control.readCount() <= before {
		t.Fatal("no new reading was taken")
	}
	if volume, _, _ := control.state(); volume != 80 {
		t.Fatalf("the low volume was not corrected: %d", volume)
	}
	if n := noticeOf(t, m); n.Kind != "restored" || n.Initial {
		t.Fatalf("notice: %+v", n)
	}
}

func TestOverlappingHeartbeatsShareChecksAndNeverPileUpRequests(t *testing.T) {
	control := &fakeSound{volume: 90}
	m := examSound(control, 80)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan struct{})
	go func() { m.run(ctx); close(done) }()
	defer func() { cancel(); <-done }()
	before := control.readCount()
	var wg sync.WaitGroup
	for range 50 {
		wg.Add(1)
		go func() { defer wg.Done(); m.refresh(ctx) }()
	}
	wg.Wait()
	if checks := control.readCount() - before; checks > 10 {
		t.Fatalf("50 heartbeats caused %d checks", checks)
	}
	m.mu.Lock()
	waiting := len(m.waiting)
	m.mu.Unlock()
	if waiting != 0 {
		t.Fatalf("%d requests leaked", waiting)
	}
}

func TestHeartbeatNeverWaitsLongerThanItsBudgetForAHungSoundTool(t *testing.T) {
	control := &fakeSound{volume: 90}
	options := soundOptions(control)
	options.HeartbeatWait = 30 * time.Millisecond
	m := newSoundManager(options)
	m.check()
	m.activate(80)
	m.check()
	control.block = make(chan struct{})
	defer close(control.block)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { m.run(ctx); close(done) }()
	defer func() { cancel(); <-done }()
	started := time.Now()
	got := m.refresh(ctx)
	if time.Since(started) > 500*time.Millisecond {
		t.Fatal("a heartbeat waited for the hung tool")
	}
	if got.Volume != nil || got.Muted != nil {
		t.Fatalf("a cached sample was passed off as a fresh reading: %+v", got)
	}
	m.mu.Lock()
	waiting := len(m.waiting)
	m.mu.Unlock()
	if waiting != 0 {
		t.Fatalf("%d requests leaked after the timeout", waiting)
	}
}

func TestNothingIsCorrectedOrAnnouncedAfterRestore(t *testing.T) {
	control := &fakeSound{volume: 90}
	m := examSound(control, 80)
	if err := m.restore(); err != nil {
		t.Fatal(err)
	}
	control.student(10, true)
	m.check()
	if got := m.refresh(t.Context()); got.Volume != nil {
		t.Fatalf("a finished exam still reads: %+v", got)
	}
	if volume, muted, _ := control.state(); volume != 10 || !muted {
		t.Fatalf("the exam minimum was reapplied after restoration: %d muted=%v", volume, muted)
	}
	if m.latestNotice() != nil {
		t.Fatalf("notice after restore: %+v", m.latestNotice())
	}
}

func TestPlatformChangeNotificationWakesTheWorkerAndStopsWithIt(t *testing.T) {
	control := &fakeSound{volume: 90}
	options := soundOptions(control)
	options.Interval = time.Hour
	changed := make(chan func(), 1)
	stopped := make(chan struct{})
	options.Watch = func(ctx context.Context, notify func()) error {
		changed <- notify
		<-ctx.Done()
		close(stopped)
		return nil
	}
	m := newSoundManager(options)
	m.check()
	m.activate(80)
	m.check()
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { m.run(ctx); close(done) }()
	notify := <-changed
	control.student(10, false)
	notify()
	eventually(t, "volume raised by the notification", func() bool { v, _, _ := control.state(); return v == 80 })
	eventually(t, "notice published", func() bool { n := m.latestNotice(); return n != nil && n.Kind == "restored" && !n.Initial })
	// The application's own adjustment wakes the watcher too; it must not announce anything again.
	sequence := noticeOf(t, m).Sequence
	notify()
	notify()
	time.Sleep(50 * time.Millisecond)
	if got := noticeOf(t, m); got.Sequence != sequence {
		t.Fatalf("the app's own change was announced: %+v", got)
	}
	cancel()
	<-done
	select {
	case <-stopped:
	default:
		t.Fatal("the watcher was left running")
	}
}

func TestHeartbeatReportsTheStudentsLevelAndTheViewCarriesTheNotice(t *testing.T) {
	f := newFixture(t)
	f.minVolume = intPtr(80)
	control := &fakeSound{volume: 90}
	s := monitorClient(t, f, func(o *Options) {
		o.Sound = soundOptions(control)
		o.Sound.Interval = time.Hour
	})
	loginClient(t, s, f)
	eventually(t, "a heartbeat", func() bool { return heartbeatWith(f, 90, false) })
	control.student(25, false)
	eventually(t, "the heartbeat checked the sound itself", func() bool { return heartbeatWith(f, 25, false) })
	eventually(t, "sound raised", func() bool { v, _, _ := control.state(); return v == 80 })
	v := waitView(t, s, func(v View) bool { return v.SoundNotice != nil && !v.SoundNotice.Initial })
	if v.SoundNotice.Kind != "restored" || v.SoundNotice.Minimum != 80 {
		t.Fatalf("view notice: %+v", v.SoundNotice)
	}
}

func TestAHeartbeatReportsWhatTheStudentHadOnceEvenAfterAnEarlierCheckCorrectedIt(t *testing.T) {
	control := &fakeSound{volume: 90}
	m := examSound(control, 80)
	control.student(15, true)
	m.check() // the periodic check restores the sound before any heartbeat asks
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan struct{})
	go func() { m.run(ctx); close(done) }()
	defer func() { cancel(); <-done }()
	first := m.refresh(ctx)
	if first.Volume == nil || *first.Volume != 15 || first.Muted == nil || !*first.Muted {
		t.Fatalf("the proctor was not told what the student had: %+v", first)
	}
	second := m.refresh(ctx)
	if second.Volume == nil || *second.Volume != 80 || *second.Muted {
		t.Fatalf("the finding was reported twice instead of the fresh reading: %+v", second)
	}
}
