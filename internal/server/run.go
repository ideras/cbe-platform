package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"exam-platform/internal/clock"
	"exam-platform/internal/config"
	"exam-platform/internal/httpapi"
	"exam-platform/internal/service"
	"exam-platform/internal/storage"
)

func Run(ctx context.Context, cfg config.Config, logger *slog.Logger) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	listener, err := net.Listen("tcp", cfg.Bind)
	if err != nil {
		return fmt.Errorf("listener preflight: %w", err)
	}
	defer listener.Close()
	if err := cfg.Prepare(); err != nil {
		return fmt.Errorf("configuration preflight: %w", err)
	}
	store, err := storage.Open(ctx, cfg.Database)
	if err != nil {
		return err
	}
	defer store.Close()
	if backup := store.UpgradeBackup(); backup != "" {
		logger.Info("database copied before it was upgraded; keep it until the new version has proven itself", "backup", backup)
	}
	timekeeper := clock.System{}
	classes := service.NewClasses(store, timekeeper)
	known, err := classes.List(ctx)
	if err != nil {
		return err
	}
	if len(known.Classes) == 0 {
		logger.Warn("no classes yet: add a class with its roster from the proctor dashboard before importing an exam")
	}
	version, err := store.SQLiteVersion(ctx)
	if err != nil {
		return err
	}
	sessions, err := service.NewSessionsWithLimits(store, timekeeper, service.AnswerLimits{PerAnswerBytes: cfg.MaxAnswerBytes, TotalBytes: cfg.MaxAnswersBytes})
	if err != nil {
		return err
	}
	exams := service.NewExams(store, timekeeper, sessions)
	publisher := service.NewPublisher(store, timekeeper)
	images, err := service.NewScreenshots(store, sessions, cfg.Screenshots)
	if err != nil {
		return err
	}
	defer images.Close()
	imageReport, err := images.Reconcile(ctx)
	if err != nil {
		return fmt.Errorf("screenshot startup reconciliation: %w", err)
	}
	if imageReport.RemovedRows > 0 || imageReport.RemovedOrphans > 0 {
		logger.Warn("screenshot startup reconciliation removed unavailable/orphan images", "rows", imageReport.RemovedRows, "orphans", imageReport.RemovedOrphans)
	}
	if _, err := sessions.Reconcile(ctx); err != nil {
		return fmt.Errorf("startup reconciliation: %w", err)
	}
	logger.Info("server ready", "bind", listener.Addr().String(), "classes", len(known.Classes), "sqlite", version)
	httpServer := &http.Server{
		Handler: httpapi.New(timekeeper, sessions, logger, httpapi.Options{AdminToken: cfg.AdminToken, Exams: exams, Publisher: publisher, Classes: classes, Proctor: sessions, Screenshots: images, Rules: sessions, MaxJSONBytes: cfg.MaxJSONBytes}), ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout: 10 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second,
		MaxHeaderBytes: 16 << 10,
	}
	workerCtx, cancelWorker := context.WithCancel(ctx)
	workerErrors := make(chan error, 2)
	workerDone := make(chan struct{})
	go func() {
		defer close(workerDone)
		workerErrors <- sessions.RunWorker(workerCtx, cfg.WorkerInterval, func(report service.ReconcileReport) {
			if report.GraceTransitions > 0 {
				logger.Info("sessions entered grace", "count", report.GraceTransitions)
			}
			if report.Finalizations > 0 {
				logger.Info("expired sessions finalized", "count", report.Finalizations)
			}
			if report.NetworkGaps > 0 {
				logger.Info("session heartbeat gaps detected", "count", report.NetworkGaps)
			}
			if report.Locks > 0 {
				logger.Info("sessions locked", "count", report.Locks)
			}
		})
	}()
	imageWorkerDone := make(chan struct{})
	go func() {
		defer close(imageWorkerDone)
		workerErrors <- images.RunWorker(workerCtx, time.Minute, func(report service.ScreenshotReconcileReport) {
			if report.RemovedRows > 0 || report.RemovedOrphans > 0 {
				logger.Warn("screenshot reconciliation removed unavailable/orphan images", "rows", report.RemovedRows, "orphans", report.RemovedOrphans)
			}
		}, func(err error) {
			logger.Error("screenshot cleanup incomplete; will retry, instructor action required", "error", err)
		})
	}()
	defer func() { cancelWorker(); <-workerDone; <-imageWorkerDone }()
	stopped := make(chan error, 1)
	go func() { stopped <- httpServer.Serve(listener) }()
	var runErr error
	serveStopped := false
	select {
	case err := <-stopped:
		serveStopped = true
		if !errors.Is(err, http.ErrServerClosed) {
			runErr = fmt.Errorf("serve HTTP: %w", err)
		}
	case err := <-workerErrors:
		if err != nil {
			runErr = fmt.Errorf("lifecycle worker: %w", err)
		}
		if err == nil && ctx.Err() == nil {
			runErr = fmt.Errorf("lifecycle worker stopped unexpectedly")
		}
	case <-ctx.Done():
	}
	cancelWorker()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		httpServer.Close()
		runErr = errors.Join(runErr, fmt.Errorf("shutdown HTTP: %w", err))
	}
	if !serveStopped {
		if err := <-stopped; !errors.Is(err, http.ErrServerClosed) {
			runErr = errors.Join(runErr, err)
		}
	}
	logger.Info("server stopped")
	return runErr
}
