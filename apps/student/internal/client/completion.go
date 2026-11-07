package client

import "errors"

func (s *Service) completedLocked() bool {
	return s.final != nil && finalStatus(s.final.Status) || s.status != nil && finalStatus(s.status.State)
}

func (s *Service) notifyCompletion() {
	s.mu.Lock()
	if s.ctx.Err() != nil || !s.completedLocked() || s.completionNotified {
		s.mu.Unlock()
		return
	}
	s.completionNotified = true
	s.phase = "complete"
	callback := s.options.OnCompletion
	s.mu.Unlock()
	// With the exam over there is no lock left to learn a record from: what is held for a lock the server never named is let go,
	// instead of being shown as pending for as long as the window stays open.
	s.capture.releaseUnboundAtEnd()
	_ = s.sound.restore()
	if callback != nil {
		callback()
	}
}

func (s *Service) CloseCompletedWindow() error {
	s.mu.Lock()
	if !s.completedLocked() {
		s.mu.Unlock()
		return errors.New("Wait for server-confirmed finalization before using the completion close button.")
	}
	if s.closeRequested || s.ctx.Err() != nil {
		s.mu.Unlock()
		return nil
	}
	callback := s.options.OnClose
	if callback == nil {
		s.mu.Unlock()
		return errors.New("Native window closure is unavailable. Use the window manager to close this completed exam.")
	}
	s.closeRequested = true
	s.mu.Unlock()
	callback()
	return nil
}
