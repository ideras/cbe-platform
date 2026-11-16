package workspace

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
)

// Submit takes a snapshot of the student's files and records the submission made of it: the files the exam submits, which is what
// `submit` and `ignore` leave. A student pressing Save (the button was called Submit until 2026-11-14) makes a SubmissionSubmit; a session that ends (by the student's choice, a
// proctor's, or time) makes a SubmissionFinal, the one that counts when nothing came after it. Students may submit again and again:
// the latest submission is the one that counts.
func (m *Manager) Submit(ctx context.Context, session, kind string) (SubmissionRecord, error) {
	info, err := m.opts.Repo.Session(ctx, session)
	if err != nil {
		return SubmissionRecord{}, err
	}
	exam, err := m.opts.Repo.Exam(ctx, info.ExamID)
	if err != nil {
		return SubmissionRecord{}, err
	}
	reason := ReasonSubmit
	if kind == SubmissionFinal {
		// Time that ran out is "deadline"; a session that ended with time left (the student finished, or a proctor ended it) is "final".
		// (The server's own worker also marks a session submitted when its time is up, so the status alone does not say which.)
		reason = ReasonDeadline
		if m.opts.Now().Before(info.FreezeAt()) {
			reason = ReasonFinal
		}
	}
	snapshot, manifest, _, err := m.Snapshots.TakeManifest(ctx, session, reason)
	if err != nil {
		return SubmissionRecord{}, err
	}
	filtered, files, bytes := FilterManifest(manifest, exam.Document.Workspace.Submit, exam.Document.Workspace.Ignore)
	encoded, err := json.Marshal(filtered)
	if err != nil {
		return SubmissionRecord{}, err
	}
	hash, err := m.opts.Blobs.PutBytes(encoded)
	if err != nil {
		return SubmissionRecord{}, err
	}
	record := SubmissionRecord{SessionID: session, SnapshotID: snapshot.ID, Kind: kind, SubmittedAt: m.opts.Now().UTC(), ManifestHash: hash, Files: files, Bytes: bytes}
	if record.ID, err = m.opts.Repo.RecordSubmission(ctx, record); err != nil {
		return SubmissionRecord{}, err
	}
	m.event(session, EventSubmitted, map[string]any{"snapshot": snapshot.ID, "submission": record.ID, "final": kind == SubmissionFinal, "files": files})
	return record, nil
}

// SubmissionTar writes the files of a submission as a tar archive: what a grading container is given. It is deterministic, and links are
// links, never followed.
func (m *Manager) SubmissionTar(ctx context.Context, id int64, w io.Writer) error {
	record, manifest, err := m.SubmissionManifest(ctx, id)
	if err != nil {
		return err
	}
	return m.Snapshots.writeManifestTar(w, manifest, record.SubmittedAt, fmt.Sprintf("submission %d", id))
}

// SubmissionManifest reads the manifest of a submission: the files it holds, named by their content.
func (m *Manager) SubmissionManifest(ctx context.Context, id int64) (SubmissionRecord, Manifest, error) {
	record, err := m.opts.Repo.Submission(ctx, id)
	if err != nil {
		return SubmissionRecord{}, Manifest{}, err
	}
	data, err := m.opts.Blobs.Read(record.ManifestHash, 64<<20)
	if err != nil {
		return record, Manifest{}, err
	}
	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return record, Manifest{}, fmt.Errorf("workspace: the manifest of submission %d is unreadable: %w", id, err)
	}
	return record, manifest, nil
}
