// Package sysvolume reads and changes the default output device's master volume for the exam client.
package sysvolume

import (
	"errors"
	"runtime"

	volume "github.com/itchyny/volume-go"
)

var (
	errWatchUnsupported = errors.New("this platform has no volume change notification")
	errWatchEnded       = errors.New("the volume change subscription ended")
)

// System controls the operating system's default output device. Linux and macOS run pactl/amixer or osascript;
// Windows uses COM, which is bound to the calling thread, so each call pins its goroutine to one.
type System struct{}

func (System) Volume() (int, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	return volume.GetVolume()
}
func (System) Muted() (bool, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	return volume.GetMuted()
}
func (System) SetVolume(percent int) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	return volume.SetVolume(percent)
}
func (System) SetMuted(muted bool) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if muted {
		return volume.Mute()
	}
	return volume.Unmute()
}
