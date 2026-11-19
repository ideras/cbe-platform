package client

import "time"

// waitLimit is how long a test waits for something it expects to happen. A test that passes stops waiting the moment it happens, so the
// limit costs nothing then: it only has to be long enough for the slowest runner of CI under the race detector. Three seconds was not, on
// macOS x64 (TestALaterCaptureTheServerRefusesIsNotAFailure and TestScreenshotsWithoutARecordAreLetGoWhenTheExamEnds failed there on
// 2026-11-19, and passed when run again).
const waitLimit = 10 * time.Second
