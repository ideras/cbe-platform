package workspace

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"exam-platform/internal/runner"
)

// An exam's own image (workspace plan, Phase 7). A package may hold image/, a Containerfile and the files it copies, that starts from the
// exam's course image. The proctor builds it before publishing, as an action of their own (the exam server never builds when a student
// logs in): the runner builds it with network, checks it is still the course image's contract, and names it. The exam server keeps each
// build and its log, says whether the exam's image is ready, and gives containers and grading runs the build to start from.

const (
	// buildWait is how long the exam server waits for the runner to answer a build: the runner stops a build at its own limit (900 s by
	// default), this is for a runner that does not answer.
	buildWait = 40 * time.Minute
	// maxBuildLog and maxBuildMessage are the most of the runner's answer that is kept.
	maxBuildLog     = 1<<20 + 8<<10
	maxBuildMessage = 4 << 10
	maxBuildQueue   = 1000
	// buildListLimit is how many of an exam's builds the proctor's view lists.
	buildListLimit = 20
)

var buildHashFormat = regexp.MustCompile(`^[0-9a-f]{16}$`)

// ImageBuilder runs the image builds of the exam server: a queue and one worker (the runner builds one at a time, too).
type ImageBuilder struct {
	m    *Manager
	jobs chan int64
	log  *slog.Logger
	// PersistEvery is how often the log of a build that is going is written to the database (10 seconds when it is not set).
	PersistEvery time.Duration

	mu sync.Mutex
	// live are the logs of the builds that are going, by build.
	live map[int64]*liveLog
}

// NewImageBuilder makes the image builder of a manager's workspace exams.
func NewImageBuilder(m *Manager) *ImageBuilder {
	return &ImageBuilder{m: m, jobs: make(chan int64, maxBuildQueue), log: m.opts.Logger, live: map[int64]*liveLog{}}
}

func (b *ImageBuilder) liveOf(id int64) *liveLog {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.live[id]
}

// keepPersisting writes the log of a build that is going to the database every PersistEvery, until stop is closed.
func (b *ImageBuilder) keepPersisting(id int64, lv *liveLog, stop <-chan struct{}) {
	every := b.PersistEvery
	if every <= 0 {
		every = defaultPersistEvery
	}
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	written := 0
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			text := lv.snapshot()
			if len(text) == written {
				continue
			}
			bg, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			if err := b.m.opts.Repo.UpdateImageBuildLog(bg, id, clip(text, maxBuildLog)); err != nil {
				b.log.Warn("workspace: the log of a build that is going could not be written", "build", id, "error", err)
			} else {
				written = len(text)
			}
			cancel()
		}
	}
}

// Recover marks the builds that were waiting or going when the exam server last stopped as failed (interrupted): the proctor builds again.
// It is called once, before the server takes requests.
func (b *ImageBuilder) Recover(ctx context.Context) {
	count, err := b.m.opts.Repo.InterruptImageBuilds(ctx, b.m.opts.Now().UTC())
	if err != nil {
		b.log.Error("workspace: builds of the last life could not be marked interrupted", "error", err)
		return
	}
	if count > 0 {
		b.log.Warn("workspace: image builds that a stop had interrupted were marked failed", "count", count)
	}
}

// Run takes builds from the queue, one at a time, until ctx ends.
func (b *ImageBuilder) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case id := <-b.jobs:
			b.runOne(ctx, id)
		}
	}
}

// Start queues a build of the exam's image. ErrNoImage when the exam has no image folder, and ErrBuildBusy when one is waiting or going. A
// build of files that were built already is quick: the runner has the image, and says so (rebuild asks for a build that does not use it).
func (b *ImageBuilder) Start(ctx context.Context, examID string, rebuild bool) (ImageBuildRecord, error) {
	repo := b.m.opts.Repo
	exam, err := repo.Exam(ctx, examID)
	if err != nil {
		return ImageBuildRecord{}, err
	}
	if exam.ImageHash == "" || !b.m.opts.Blobs.Has(exam.ImageHash) {
		return ImageBuildRecord{}, ErrNoImage
	}
	id, err := repo.CreateImageBuild(ctx, examID, exam.ImageHash, rebuild, b.m.opts.Now().UTC())
	if err != nil {
		return ImageBuildRecord{}, err
	}
	select {
	case b.jobs <- id:
	default:
		bg, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		b.finish(bg, ImageBuildRecord{ID: id}, BuildFailRunner, "too many builds are waiting; try again in a moment", nil)
	}
	return repo.ImageBuild(ctx, id)
}

func (b *ImageBuilder) runOne(ctx context.Context, id int64) {
	repo := b.m.opts.Repo
	record, err := repo.ImageBuild(ctx, id)
	if err != nil || record.State != BuildQueued {
		return
	}
	// The live log exists from the moment the build is marked running, so a look at it never finds a running build with none.
	lv := &liveLog{}
	b.mu.Lock()
	b.live[id] = lv
	b.mu.Unlock()
	stopPersisting := make(chan struct{})
	defer func() {
		close(stopPersisting)
		b.mu.Lock()
		delete(b.live, id)
		b.mu.Unlock()
	}()
	if err := repo.StartImageBuild(ctx, id, b.m.opts.Now().UTC()); err != nil {
		b.log.Error("workspace: an image build could not be started", "build", id, "error", err)
		return
	}
	go b.keepPersisting(id, lv, stopPersisting)
	// What is recorded is recorded even when the server is stopping: the build does not end in a state it never finished.
	done := func(failure, text string, result *runner.BuildResult) {
		bg, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		b.finish(bg, record, failure, text, result)
	}
	exam, err := repo.Exam(ctx, record.ExamID)
	if err != nil {
		done(BuildFailRunner, "the exam could not be read", nil)
		return
	}
	bundle, err := b.m.opts.Blobs.Open(record.BundleHash)
	if err != nil {
		done(BuildFailRunner, "the exam's image bundle cannot be read", nil)
		return
	}
	defer bundle.Close()
	runCtx, cancel := context.WithTimeout(ctx, buildWait)
	defer cancel()
	result, err := b.m.opts.Runner.BuildImage(runCtx, runner.BuildRequest{Exam: record.ExamID, Image: exam.Document.Workspace.Image, Rebuild: record.Rebuild}, bundle,
		func(text string) { lv.add(text, b.m.opts.Now().UTC()) })
	if err != nil {
		if ctx.Err() != nil {
			done(BuildFailInterrupted, "the exam server stopped before this build ended; build the image again", nil)
			return
		}
		done(BuildFailRunner, "the runner could not build it: "+err.Error(), nil)
		return
	}
	b.settle(record, result, done)
}

// settle turns what the runner returned into the build's record.
func (b *ImageBuilder) settle(record ImageBuildRecord, result runner.BuildResult, done func(string, string, *runner.BuildResult)) {
	if !result.OK {
		code := result.Failure
		switch code {
		case BuildFailContainerfile, BuildFailBase, BuildFailBuild, BuildFailTimeout, BuildFailVerify:
		default:
			code = BuildFailRunner
		}
		text := result.Message
		if text == "" {
			text = "the build failed"
		}
		done(code, text, &result)
		return
	}
	if !buildHashFormat.MatchString(result.Hash) {
		done(BuildFailRunner, "the runner said the build was made but gave no usable name for it", &result)
		return
	}
	now := b.m.opts.Now().UTC()
	record.State, record.FinishedAt = BuildOK, &now
	b.fill(&record, &result)
	if result.Cached {
		record.Log = b.withEarlierLog(record, result)
	}
	bg, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := b.m.opts.Repo.FinishImageBuild(bg, record); err != nil {
		b.log.Error("workspace: an image build could not be recorded", "build", record.ID, "error", err)
		return
	}
	b.log.Info("workspace: image built", "build", record.ID, "exam", record.ExamID, "hash", record.Hash, "cached", record.Cached, "base", record.BaseDigest)
}

// fill copies what the runner said of a build into its record.
func (b *ImageBuilder) fill(record *ImageBuildRecord, result *runner.BuildResult) {
	record.Cached, record.Hash, record.ImageID = result.Cached, result.Hash, result.ImageID
	record.BaseName, record.BaseDigest, record.DurationMS = result.BaseName, result.BaseDigest, result.DurationMS
	record.Warnings = append([]string{}, result.Warnings...)
	record.Log = clip(strings.ToValidUTF8(result.Log, "�"), maxBuildLog)
}

// withEarlierLog gives the log of a build that found its image built the log of the build that made it, so that the proctor who asks again
// still reads what the image is made of.
func (b *ImageBuilder) withEarlierLog(record ImageBuildRecord, result runner.BuildResult) string {
	bg, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	earlier, err := b.m.opts.Repo.ImageBuilds(bg, record.ExamID, 50)
	if err != nil {
		return record.Log
	}
	for _, candidate := range earlier {
		if candidate.State == BuildOK && !candidate.Cached && candidate.Hash == result.Hash && candidate.ID != record.ID {
			if full, err := b.m.opts.Repo.ImageBuild(bg, candidate.ID); err == nil {
				return clip(record.Log+"\n--- the log of the build that made this image (build "+fmt.Sprint(full.ID)+") ---\n"+full.Log, maxBuildLog)
			}
		}
	}
	return record.Log
}

// finish records a build as failed, with what the runner said when there is that.
func (b *ImageBuilder) finish(ctx context.Context, record ImageBuildRecord, failure, text string, result *runner.BuildResult) {
	now := b.m.opts.Now().UTC()
	record.State, record.Failure, record.Message, record.FinishedAt = BuildFailed, failure, clip(strings.ToValidUTF8(text, "�"), maxBuildMessage), &now
	if result != nil {
		b.fill(&record, result)
		record.Cached, record.Hash, record.ImageID = false, "", ""
	} else if lv := b.liveOf(record.ID); lv != nil && record.Log == "" {
		// The runner gave no result (it did not answer, or the server is stopping): what the build had printed is the log.
		record.Log = clip(lv.snapshot(), maxBuildLog)
	}
	if err := b.m.opts.Repo.FinishImageBuild(ctx, record); err != nil {
		b.log.Error("workspace: a failed image build could not be recorded", "build", record.ID, "error", err)
		return
	}
	b.log.Warn("workspace: an image build failed", "build", record.ID, "exam", record.ExamID, "failure", failure, "why", text)
}

func clip(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	text = text[:limit]
	for !utf8.ValidString(text) && len(text) > 0 {
		text = text[:len(text)-1]
	}
	return text + "\n[cut]"
}

// Image states, as the proctor reads them.
const (
	ImageNone     = "none"      // the exam has no image folder
	ImageNotBuilt = "not_built" // it has one, and nothing has been built
	ImageQueued   = "queued"
	ImageBuilding = "building"
	ImageReady    = "ready"  // a verified build of its current files exists
	ImageFailed   = "failed" // the latest build failed
	ImageMissing  = "missing"
)

// ImageBuildView is a build as the proctor reads it. Log is empty unless the log was asked for.
type ImageBuildView struct {
	ID         int64      `json:"build_id"`
	State      string     `json:"state"`
	Failure    string     `json:"failure,omitempty"`
	Message    string     `json:"message,omitempty"`
	Cached     bool       `json:"cached,omitempty"`
	Rebuild    bool       `json:"rebuild,omitempty"`
	Hash       string     `json:"hash,omitempty"`
	BaseName   string     `json:"base_name,omitempty"`
	BaseDigest string     `json:"base_digest,omitempty"`
	Warnings   []string   `json:"warnings"`
	DurationMS int64      `json:"duration_ms"`
	QueuedAt   time.Time  `json:"queued_at"`
	StartedAt  *time.Time `json:"started_at,omitempty"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
	Log        string     `json:"log,omitempty"`
	// The log of a build that is waiting or going is read from an offset: Live says so, LogFrom is the offset Log starts at (0 when the one asked for is
	// not one the server has) and LogNext the offset to ask for next. A build that has ended has its whole log, from 0, and Live is false.
	Live    bool  `json:"live,omitempty"`
	LogFrom int64 `json:"log_from,omitempty"`
	LogNext int64 `json:"log_next,omitempty"`
	// Step, Steps and StepText are where a build that is going is: "STEP 4/9: RUN make". LastOutputAt is when it last printed anything.
	Step         int        `json:"step,omitempty"`
	Steps        int        `json:"steps,omitempty"`
	StepText     string     `json:"step_text,omitempty"`
	LastOutputAt *time.Time `json:"last_output_at,omitempty"`
}

// ImageView is the state of an exam's image: what the proctor needs to decide whether to build and whether to publish.
type ImageView struct {
	ExamID string `json:"exam_id"`
	// State is one of the Image… constants. Present says the runner was asked and has the image; it is only set by Image, never by the
	// states of a list (a list does not ask the runner).
	State   string `json:"state"`
	Image   string `json:"image,omitempty"`
	Present *bool  `json:"present,omitempty"`
	// Ready is whether the exam can be published as far as its image goes.
	Ready bool `json:"ready"`
	// Build is the build the state is about: the one going, or else the latest. Builds lists the exam's builds, newest first, without logs.
	Build  *ImageBuildView  `json:"build,omitempty"`
	Builds []ImageBuildView `json:"builds,omitempty"`
}

func buildView(r ImageBuildRecord, withLog bool) ImageBuildView {
	v := ImageBuildView{ID: r.ID, State: r.State, Failure: r.Failure, Message: r.Message, Cached: r.Cached, Rebuild: r.Rebuild, Hash: r.Hash, BaseName: r.BaseName,
		BaseDigest: r.BaseDigest, Warnings: append([]string{}, r.Warnings...), DurationMS: r.DurationMS, QueuedAt: r.QueuedAt, StartedAt: r.StartedAt, FinishedAt: r.FinishedAt}
	if withLog {
		v.Log = r.Log
	}
	return v
}

// stateView works out the state of an exam's image from its latest build and whether a good build of its current files exists, without
// asking the runner. A failed or running build does not take away a good build that is there; the state says what the latest build is doing,
// and Ready says whether there is a good image.
func stateView(examID, image, imageHash string, latest *ImageBuildRecord, hasGood bool) ImageView {
	view := ImageView{ExamID: examID, State: ImageNone, Image: image}
	if imageHash == "" {
		view.Ready = true // nothing to build: the course image is what it uses
		return view
	}
	view.State, view.Ready = ImageNotBuilt, hasGood
	if latest == nil {
		return view
	}
	built := buildView(*latest, false)
	view.Build = &built
	switch latest.State {
	case BuildQueued:
		view.State = ImageQueued
	case BuildRunning:
		view.State = ImageBuilding
	case BuildFailed:
		view.State = ImageFailed
	case BuildOK:
		view.State = ImageReady
	}
	return view
}

// state is stateView for one exam, asking the database for what it needs.
func (b *ImageBuilder) state(ctx context.Context, exam ExamRecord) (ImageView, error) {
	if exam.ImageHash == "" {
		return stateView(exam.ExamID, exam.Document.Workspace.Image, "", nil, false), nil
	}
	builds, err := b.m.opts.Repo.ImageBuilds(ctx, exam.ExamID, buildListLimit)
	if err != nil {
		return ImageView{}, err
	}
	var latest *ImageBuildRecord
	if len(builds) > 0 {
		latest = &builds[0]
	}
	hasGood := false
	if _, err := b.m.opts.Repo.LatestImageBuild(ctx, exam.ExamID, exam.ImageHash); err == nil {
		hasGood = true
	} else if !errors.Is(err, ErrNotFound) {
		return ImageView{}, err
	}
	view := stateView(exam.ExamID, exam.Document.Workspace.Image, exam.ImageHash, latest, hasGood)
	for _, record := range builds {
		view.Builds = append(view.Builds, buildView(record, false))
	}
	return view, nil
}

// ExamImage is what the exam list shows of a workspace exam: how many TODO items it has, and its image.
type ExamImage struct {
	Todos int       `json:"todos"`
	Image ImageView `json:"image"`
}

// States gives, for every workspace exam, its TODO count and the state of its own image, from the database alone (the exam list asks this
// at every refresh, so it is two queries and does not ask the runner).
func (b *ImageBuilder) States(ctx context.Context) (map[string]ExamImage, error) {
	summaries, err := b.m.opts.Repo.ImageSummaries(ctx)
	if err != nil {
		return nil, err
	}
	out := make(map[string]ExamImage, len(summaries))
	for _, summary := range summaries {
		out[summary.ExamID] = ExamImage{Todos: summary.Todos, Image: stateView(summary.ExamID, summary.Image, summary.ImageHash, summary.Latest, summary.HasGood)}
	}
	return out, nil
}

// View is the state of one exam's image with its builds, and the log of the latest when withLog. It asks the runner whether it still has
// the image of a build that is recorded as ready.
func (b *ImageBuilder) View(ctx context.Context, examID string, withLog bool, logFrom int64) (ImageView, error) {
	exam, err := b.m.opts.Repo.Exam(ctx, examID)
	if err != nil {
		return ImageView{}, err
	}
	view, err := b.state(ctx, exam)
	if err != nil {
		return ImageView{}, err
	}
	if view.Build != nil && (view.Build.State == BuildQueued || view.Build.State == BuildRunning) {
		// A build that is waiting or going: its log is the live one, from the offset asked for, with where it is.
		view.Build.Live = true
		if lv := b.liveOf(view.Build.ID); lv != nil {
			step, steps, text, last := lv.progress()
			view.Build.Step, view.Build.Steps, view.Build.StepText = step, steps, text
			if !last.IsZero() {
				view.Build.LastOutputAt = &last
			}
			if withLog {
				view.Build.Log, view.Build.LogFrom, view.Build.LogNext = lv.read(logFrom)
			}
		} else if withLog && view.Build.State == BuildRunning {
			// It is going and has no live log: the one the database has.
			full, err := b.m.opts.Repo.ImageBuild(ctx, view.Build.ID)
			if err != nil {
				return ImageView{}, err
			}
			view.Build.Log, view.Build.Live = full.Log, false
			view.Build.LogNext = int64(len(full.Log))
		}
	} else if withLog && view.Build != nil {
		full, err := b.m.opts.Repo.ImageBuild(ctx, view.Build.ID)
		if err != nil {
			return ImageView{}, err
		}
		*view.Build = buildView(full, true)
		view.Build.LogNext = int64(len(full.Log))
	}
	if exam.ImageHash != "" && view.Ready {
		present, err := b.present(ctx, exam)
		if err == nil {
			view.Present = &present
			if !present {
				view.State, view.Ready = ImageMissing, false
			}
		}
	}
	return view, nil
}

// present asks the runner whether it has the exam's current image.
func (b *ImageBuilder) present(ctx context.Context, exam ExamRecord) (bool, error) {
	hash, err := b.m.currentBuild(ctx, exam)
	if err != nil {
		return false, err
	}
	status, err := b.m.opts.Runner.ImageInspect(ctx, runner.BuiltName(exam.ExamID, hash))
	if err != nil {
		return false, err
	}
	return status.Approved && status.Present, nil
}

// PublishBlocked is the reason an exam cannot be published as far as its image goes (decision P7-11).
type PublishBlocked struct{ Reason string }

func (e *PublishBlocked) Error() string { return e.Reason }

// CheckPublishable says nil when the exam can be published as far as its image goes: it has none, or has a verified build the runner still
// has. It is the gate the publisher asks before it admits students, and answers *PublishBlocked otherwise; an exam that is not a workspace exam
// is none of its business.
func (b *ImageBuilder) CheckPublishable(ctx context.Context, examID string) error {
	exam, err := b.m.opts.Repo.Exam(ctx, examID)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if exam.ImageHash == "" {
		return nil
	}
	view, err := b.state(ctx, exam)
	if err != nil {
		return err
	}
	switch {
	case view.State == ImageQueued || view.State == ImageBuilding:
		return &PublishBlocked{"the exam's image is being built: wait for the build to end before publishing"}
	case !view.Ready && view.State == ImageFailed:
		return &PublishBlocked{"the last build of the exam's image failed (" + clip(view.Build.Message, 200) + "): fix the Containerfile and build it again before publishing"}
	case !view.Ready:
		return &PublishBlocked{"the exam has its own image and it has not been built: build it before publishing"}
	}
	present, err := b.present(ctx, exam)
	if err != nil {
		return fmt.Errorf("the runner could not be asked about the exam's image: %w", err)
	}
	if !present {
		return &PublishBlocked{"the runner does not have the exam's image any more (it was removed): build it again before publishing"}
	}
	return nil
}
