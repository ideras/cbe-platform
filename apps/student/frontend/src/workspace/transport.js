// How the page talks to the workspace: through the application's Go side (api.WorkspaceRequest and the rest), which holds the
// connection. A request is a JSON text and so is its answer; an answer that says ok:false is a value for the model to read, while a
// request that got no answer at all is an exception.
export function makeTransport(api) {
  return {
    async request(type, fields = {}) {
      const text = await api.WorkspaceRequest(JSON.stringify({ type, ...fields }));
      return JSON.parse(text);
    },
    input: (term, data) => api.WorkspaceInput(term, data),
    reconnect: () => api.WorkspaceReconnect(),
  };
}

// Terminal output comes from Go as base64 in an event: bytes, not text, because a character may be cut between two pieces.
export function decodeOutput(data) {
  const binary = atob(data);
  const bytes = new Uint8Array(binary.length);
  for (let i = 0; i < binary.length; i += 1) bytes[i] = binary.charCodeAt(i);
  return bytes;
}
