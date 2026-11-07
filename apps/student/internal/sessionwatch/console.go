package sessionwatch

// noSession is what Windows answers when no session is attached to the console. It does so for a moment while a session is
// being attached or detached, as in a switch of user.
const noSession = 0xFFFFFFFF

// unattachedReadings is how many readings in a row may find no session on the console before that counts as the student's
// session being off it. A moment between two sessions is not an answer; a console that stays without one is not the student's.
const unattachedReadings = 3

// consoleProbe says whether the session of this process is the one attached to the console. The console is the session on
// screen: when the student switches to another user, or a remote client takes the session over, the console moves to another
// session and this one is no longer on it.
type consoleProbe struct {
	own        uint32
	unattached int
}

// onScreen takes the number of the session attached to the console now and says whether it is this process's own. It is not
// known while the console has had no session for only a moment.
func (p *consoleProbe) onScreen(console uint32) (on, known bool) {
	if console != noSession {
		p.unattached = 0
		return console == p.own, true
	}
	p.unattached++
	return false, p.unattached > unattachedReadings
}
