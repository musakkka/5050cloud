// SPDX-License-Identifier: AGPL-3.0-only

// The byte link under the terminal: the shell WebSocket in production, or the
// in-page fake shell under `npm run dev:mocks` (mocks.ts). Binary frames carry
// the stream; text frames carry {"resize":[cols,rows]} and {"exit":code}.

export interface LinkHandlers {
  onOpen(): void;
  onData(bytes: Uint8Array): void;
  onControl(frame: string): void;
  onClose(): void;
}

export interface Link {
  /** Keystrokes and pasted text, as typed. */
  send(text: string): void;
  control(msg: { resize: [number, number] }): void;
  close(): void;
}

const enc = new TextEncoder();

export function wsLink(url: string, h: LinkHandlers): Link {
  const sock = new WebSocket(url);
  sock.binaryType = "arraybuffer";
  sock.onopen = () => h.onOpen();
  sock.onmessage = (m) => {
    if (typeof m.data === "string") h.onControl(m.data);
    else h.onData(new Uint8Array(m.data as ArrayBuffer));
  };
  sock.onclose = () => h.onClose();
  const open = () => sock.readyState === WebSocket.OPEN;
  return {
    send: (text) => { if (open()) sock.send(enc.encode(text)); },
    control: (msg) => { if (open()) sock.send(JSON.stringify(msg)); },
    close: () => { sock.onclose = null; sock.close(); },
  };
}
