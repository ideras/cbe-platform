import { Events } from '/wails/runtime.js';
export function OnFocusAlert(listener) { return Events.On('exam:unfocus-alert', event => listener(event.data)); }
// A workspace exam's terminal output and the server's messages (docs/workspace/relay-protocol.md), which the Go side holds the connection for.
export function OnWorkspaceOutput(listener) { return Events.On('workspace:output', event => listener(event.data)); }
export function OnWorkspaceMessage(listener) { return Events.On('workspace:message', event => listener(event.data)); }
export { TestScreenshot, CloseCompletedWindow, CheckCapturePermission, AcknowledgeRules, ReportAlertFailure, Login, RetryLoad, Snapshot, UpdateAnswers, Submit, Advance, Retry, RequestNetworkLock, Rules, CalcAllowedFunctions, CalcEvaluate, CalcEvaluateInt, CalcConvertFraction, CalcInspect, CalcOpened, CalcClosed, CalcModeChanged, CalcTabChanged, Version, WorkspaceRequest, WorkspaceInput, WorkspaceReconnect } from '../bindings/exam-platform/apps/student/internal/client/service.js';
