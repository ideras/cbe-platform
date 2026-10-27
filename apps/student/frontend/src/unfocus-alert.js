// The exam's required system volume decides how loud the alert is, so the element itself plays at full level.
export const ALERT_VOLUME = 1;

export function describeSoundFailure(error) {
  const name = error?.name || 'Error';
  if (name === 'NotAllowedError') return `Audio playback was blocked by the system (${name}).`;
  if (name === 'NotSupportedError') return `This computer could not open the alert sound (${name}).`;
  return `The alert sound could not be played (${name}).`;
}

export class UnfocusAlert {
  // `last` is the incident and beep heard last: Go repeats the alert while a sustained focus loss lasts, numbering each beep.
  constructor(audio, onFailure) { this.audio = audio; this.onFailure = onFailure; this.last = { incident: 0, beep: 0 }; this.failed = 0; this.audio.volume = ALERT_VOLUME; }

  // Call play() and pause() back to back inside the login click: the webview then lets later alerts, which start from
  // native focus events with no user gesture, play audibly (verified in WebKitGTK). The element is never set to 0:
  // on PipeWire the stream volume is remembered per media role, and WebKit refuses to raise it again outside a
  // gesture, which left every later alert silent. Inside this gesture a small real change (0.99, then full) is
  // allowed and also lifts a zero an earlier run left behind (a repeated set of the same value is ignored).
  prime() {
    this.audio.volume = ALERT_VOLUME - 0.01; this.audio.volume = ALERT_VOLUME; // a real change, so the player applies it
    const started = this.audio.play();
    this.audio.pause();
    return Promise.resolve(started).then(() => {}, error => { if (error?.name !== 'AbortError') throw error; });
  }
  async play(incident) {
    if (!incident) return;
    const sequence = incident.local_incident, beep = incident.beep || 1;
    if (sequence < this.last.incident || sequence === this.last.incident && beep <= this.last.beep) return;
    this.last = { incident: sequence, beep };
    this.audio.currentTime = 0;
    try { await this.audio.play(); }
    catch {
      // One report per focus loss: a blocked player fails on every beep.
      if (this.failed < sequence) { this.failed = sequence; this.onFailure(sequence); }
    }
  }
  close() { this.audio.pause(); }
}
