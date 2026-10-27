// Errors from the native bridge arrive as "RuntimeError: message"; the window shows only the message.
export function message(err) {
  return String(err?.message ?? err).replace(/^(RuntimeError|Error):\s*/, '');
}
