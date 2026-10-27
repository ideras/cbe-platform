import { Events } from '/wails/runtime.js';
export function OnFocusAlert(listener) { return Events.On('exam:unfocus-alert', event => listener(event.data)); }
export { TestScreenshot, CloseCompletedWindow, CheckCapturePermission, AcknowledgeRules, ReportAlertFailure, Login, RetryLoad, Snapshot, UpdateAnswers, Submit, Advance, Retry, Rules, CalcAllowedFunctions, CalcEvaluate, CalcEvaluateInt, CalcConvertFraction, CalcInspect, CalcOpened, CalcClosed, CalcModeChanged, CalcTabChanged } from '../bindings/exam-platform/apps/student/internal/client/service.js';
