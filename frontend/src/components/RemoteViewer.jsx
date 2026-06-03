import { useCallback, useEffect, useRef, useState } from 'react'

const MSG_FRAME = 0x01
const MSG_TILE = 0x03
const MSG_MOUSE_MOVE = 0x10
const MSG_MOUSE_CLICK = 0x11
const MSG_KEY_EVENT = 0x12
const MSG_TEXT_INPUT = 0x13
const MSG_MOUSE_WHEEL = 0x14
const MAX_PENDING_PACKETS = 240
const textEncoder = new TextEncoder()

const VK = {
  CTRL: 0x11,
  SHIFT: 0x10,
  ALT: 0x12,
  ESC: 0x1b,
  DELETE: 0x2e,
  WIN: 0x5b,
  R: 0x52,
  E: 0x45,
  I: 0x49,
  D: 0x44,
  L: 0x4c,
  TAB: 0x09,
  F4: 0x73,
  PRINT_SCREEN: 0x2c,
}

const SHORTCUTS = [
  { label: 'TaskMgr', keys: [VK.CTRL, VK.SHIFT, VK.ESC] },
  { label: 'Ejecutar', keys: [VK.WIN, VK.R] },
  { label: 'Explorador', keys: [VK.WIN, VK.E] },
  { label: 'Config', keys: [VK.WIN, VK.I] },
  { label: 'Alt+Tab', keys: [VK.ALT, VK.TAB] },
  { label: 'Alt+F4', keys: [VK.ALT, VK.F4] },
  { label: 'Win+D', keys: [VK.WIN, VK.D] },
  { label: 'Bloquear', keys: [VK.WIN, VK.L] },
  { label: 'ImprPant', keys: [VK.PRINT_SCREEN] },
  { label: 'Ctrl+Alt+Del', keys: [VK.CTRL, VK.ALT, VK.DELETE] },
]

function putU16(view, offset, value) {
  view.setUint16(offset, Math.max(0, Math.min(65535, Math.round(value))), false)
}

function putU32(view, offset, value) {
  view.setUint32(offset, value >>> 0, false)
}

function putI16(view, offset, value) {
  view.setInt16(offset, Math.max(-32768, Math.min(32767, Math.round(value))), false)
}

function keyToVK(event) {
  const codeMap = {
    Digit0: 0x30,
    Digit1: 0x31,
    Digit2: 0x32,
    Digit3: 0x33,
    Digit4: 0x34,
    Digit5: 0x35,
    Digit6: 0x36,
    Digit7: 0x37,
    Digit8: 0x38,
    Digit9: 0x39,
    KeyA: 0x41,
    KeyB: 0x42,
    KeyC: 0x43,
    KeyD: 0x44,
    KeyE: 0x45,
    KeyF: 0x46,
    KeyG: 0x47,
    KeyH: 0x48,
    KeyI: 0x49,
    KeyJ: 0x4a,
    KeyK: 0x4b,
    KeyL: 0x4c,
    KeyM: 0x4d,
    KeyN: 0x4e,
    KeyO: 0x4f,
    KeyP: 0x50,
    KeyQ: 0x51,
    KeyR: 0x52,
    KeyS: 0x53,
    KeyT: 0x54,
    KeyU: 0x55,
    KeyV: 0x56,
    KeyW: 0x57,
    KeyX: 0x58,
    KeyY: 0x59,
    KeyZ: 0x5a,
  }
  if (codeMap[event.code]) {
    return codeMap[event.code]
  }
  const map = {
    Backspace: 0x08,
    Tab: 0x09,
    Enter: 0x0d,
    Shift: 0x10,
    Control: 0x11,
    Alt: 0x12,
    Pause: 0x13,
    CapsLock: 0x14,
    Escape: 0x1b,
    ' ': 0x20,
    PageUp: 0x21,
    PageDown: 0x22,
    End: 0x23,
    Home: 0x24,
    ArrowLeft: 0x25,
    ArrowUp: 0x26,
    ArrowRight: 0x27,
    ArrowDown: 0x28,
    Insert: 0x2d,
    Delete: 0x2e,
    Meta: 0x5b,
    F1: 0x70,
    F2: 0x71,
    F3: 0x72,
    F4: 0x73,
    F5: 0x74,
    F6: 0x75,
    F7: 0x76,
    F8: 0x77,
    F9: 0x78,
    F10: 0x79,
    F11: 0x7a,
    F12: 0x7b,
    F13: 0x7c,
    F14: 0x7d,
    F15: 0x7e,
    F16: 0x7f,
    F17: 0x80,
    F18: 0x81,
    F19: 0x82,
    F20: 0x83,
    F21: 0x84,
    F22: 0x85,
    F23: 0x86,
    F24: 0x87,
    NumLock: 0x90,
    ScrollLock: 0x91,
  }
  return map[event.key] || event.keyCode || event.which || 0
}

function isTextKey(event) {
  if (!event.key || event.key.length === 0 || event.key === 'Dead') return false
  if (event.metaKey) return false
  if (event.ctrlKey && !event.altKey && !event.getModifierState?.('AltGraph')) return false
  return event.key.length > 0 && [...event.key].length === 1
}

export default function RemoteViewer({ serverIP = '127.0.0.1', httpPort = 8080, clientId = '', className = '' }) {
  const canvasRef = useRef(null)
  const socketRef = useRef(null)
  const screenRef = useRef({ width: 0, height: 0 })
  const drawingRef = useRef(false)
  const pendingRef = useRef([])
  const lastMoveRef = useRef(0)
  const [status, setStatus] = useState('Conectando')

  const clientPath = clientId ? `/${encodeURIComponent(clientId)}` : ''
  const wsURL = `${window.location.protocol === 'https:' ? 'wss' : 'ws'}://${serverIP}:${httpPort}/ws/remote${clientPath}?role=viewer`

  useEffect(() => {
    let closed = false
    let retry = 500
    let timer

    const connect = () => {
      if (closed) return
      const ws = new WebSocket(wsURL)
      ws.binaryType = 'arraybuffer'
      socketRef.current = ws
      setStatus('Conectando')

      ws.onopen = () => {
        retry = 500
        setStatus('Esperando agente')
      }
      ws.onclose = () => {
        if (socketRef.current === ws) socketRef.current = null
        if (closed) return
        setStatus('Reconectando')
        timer = window.setTimeout(connect, retry)
        retry = Math.min(retry * 1.7, 5000)
      }
      ws.onerror = () => ws.close()
      ws.onmessage = (event) => {
        if (!(event.data instanceof ArrayBuffer)) return
        const bytes = new Uint8Array(event.data)
        const queue = pendingRef.current
        // Full frames are keyframes: prefer the newest one and discard stale work.
        if (bytes[0] === MSG_FRAME) {
          queue.length = 0
          queue.push(event.data)
        } else {
          // Tile packets are incremental; keep a bounded FIFO so 30/60 FPS bursts
          // do not overwrite each other before the canvas can draw them.
          if (queue.length >= MAX_PENDING_PACKETS) {
            queue.splice(0, queue.length - MAX_PENDING_PACKETS + 1)
          }
          queue.push(event.data)
        }
        pumpDrawQueue()
      }
    }

    connect()
    return () => {
      closed = true
      window.clearTimeout(timer)
      socketRef.current?.close()
      socketRef.current = null
    }
  }, [wsURL])

  const pumpDrawQueue = async () => {
    if (drawingRef.current) return
    drawingRef.current = true
    try {
      while (pendingRef.current.length > 0) {
        const packet = pendingRef.current.shift()
        await drawPacket(packet)
      }
    } finally {
      drawingRef.current = false
    }
  }

  const drawPacket = async (buffer) => {
    const bytes = new Uint8Array(buffer)
    if (bytes.length < 1) return
    const view = new DataView(buffer)
    const canvas = canvasRef.current
    if (!canvas) return
    const ctx = canvas.getContext('2d', { alpha: false })

    if (bytes[0] === MSG_FRAME && bytes.length >= 6) {
      setStatus('Recibiendo pantalla')
      const width = view.getUint16(1, false)
      const height = view.getUint16(3, false)
      const blob = new Blob([bytes.slice(6)], { type: 'image/jpeg' })
      let bitmap
      try {
        bitmap = await createImageBitmap(blob)
      } catch (e) {
        setStatus('Frame inválido')
        return
      }
      if (canvas.width !== width || canvas.height !== height) {
        canvas.width = width
        canvas.height = height
      }
      screenRef.current = { width, height }
      ctx.drawImage(bitmap, 0, 0, width, height)
      bitmap.close?.()
      return
    }

    if (bytes[0] === MSG_TILE && bytes.length >= 10) {
      setStatus('Recibiendo pantalla')
      const x = view.getUint16(1, false)
      const y = view.getUint16(3, false)
      const width = view.getUint16(5, false)
      const height = view.getUint16(7, false)
      const blob = new Blob([bytes.slice(10)], { type: 'image/jpeg' })
      let bitmap
      try {
        bitmap = await createImageBitmap(blob)
      } catch (e) {
        setStatus('Tile inválido')
        return
      }
      ctx.drawImage(bitmap, x, y, width, height)
      bitmap.close?.()
    }
  }

  const canvasPoint = useCallback((event) => {
    const canvas = canvasRef.current
    const { width, height } = screenRef.current
    if (!canvas || !width || !height) return null
    const rect = canvas.getBoundingClientRect()
    const scale = Math.min(rect.width / width, rect.height / height)
    const drawnWidth = width * scale
    const drawnHeight = height * scale
    const offsetX = (rect.width - drawnWidth) / 2
    const offsetY = (rect.height - drawnHeight) / 2
    const x = (event.clientX - rect.left - offsetX) / scale
    const y = (event.clientY - rect.top - offsetY) / scale
    return {
      x: Math.max(0, Math.min(width - 1, x)),
      y: Math.max(0, Math.min(height - 1, y)),
    }
  }, [])

  const send = (packet) => {
    const ws = socketRef.current
    if (ws?.readyState === WebSocket.OPEN) {
      ws.send(packet)
    }
  }

  const sendMouseMove = (event) => {
    event.currentTarget.focus()
    const now = performance.now()
    if (now - lastMoveRef.current < 16) return
    lastMoveRef.current = now
    const p = canvasPoint(event)
    if (!p) return
    const buf = new ArrayBuffer(5)
    const view = new DataView(buf)
    view.setUint8(0, MSG_MOUSE_MOVE)
    putU16(view, 1, p.x)
    putU16(view, 3, p.y)
    send(buf)
  }

  const sendMouseClick = (event, down) => {
    event.preventDefault()
    event.currentTarget.focus()
    const p = canvasPoint(event)
    if (!p) return
    const buf = new ArrayBuffer(7)
    const view = new DataView(buf)
    view.setUint8(0, MSG_MOUSE_CLICK)
    putU16(view, 1, p.x)
    putU16(view, 3, p.y)
    view.setUint8(5, event.button === 1 ? 2 : event.button === 2 ? 3 : 1)
    view.setUint8(6, down ? 1 : 0)
    send(buf)
  }

  const sendMouseWheel = (event) => {
    event.preventDefault()
    event.currentTarget.focus()
    const p = canvasPoint(event)
    if (!p) return
    const normalized = event.deltaMode === 1 ? event.deltaY * 120 : event.deltaY
    const direction = normalized < 0 ? 1 : -1
    const magnitude = Math.max(60, Math.min(1200, Math.round(Math.abs(normalized))))
    const delta = direction * magnitude
    const buf = new ArrayBuffer(7)
    const view = new DataView(buf)
    view.setUint8(0, MSG_MOUSE_WHEEL)
    putU16(view, 1, p.x)
    putU16(view, 3, p.y)
    putI16(view, 5, delta)
    send(buf)
  }

  const sendKeyPacket = (vk, down) => {
    if (!vk) return
    const buf = new ArrayBuffer(6)
    const view = new DataView(buf)
    view.setUint8(0, MSG_KEY_EVENT)
    putU32(view, 1, vk)
    view.setUint8(5, down ? 1 : 0)
    send(buf)
  }

  const sendShortcut = (keys) => {
    if (!keys?.length) return
    const modifiers = keys.slice(0, -1)
    const key = keys[keys.length - 1]
    modifiers.forEach((vk) => sendKeyPacket(vk, true))
    sendKeyPacket(key, true)
    window.setTimeout(() => {
      sendKeyPacket(key, false)
      modifiers.slice().reverse().forEach((vk) => sendKeyPacket(vk, false))
      canvasRef.current?.focus()
    }, 40)
  }

  const sendKey = (event, down) => {
    if (down && isTextKey(event)) {
      event.preventDefault()
      const data = textEncoder.encode(event.key)
      const buf = new ArrayBuffer(3 + data.length)
      const view = new DataView(buf)
      const bytes = new Uint8Array(buf)
      view.setUint8(0, MSG_TEXT_INPUT)
      putU16(view, 1, data.length)
      bytes.set(data, 3)
      send(buf)
      return
    }
    if (!down && isTextKey(event)) {
      event.preventDefault()
      return
    }
    const vk = keyToVK(event)
    if (!vk) return
    event.preventDefault()
    sendKeyPacket(vk, down)
  }

  return (
    <div className={`relative h-full w-full bg-black ${className}`}>
      <canvas
        ref={canvasRef}
        tabIndex={0}
        onPointerMove={sendMouseMove}
        onPointerDown={(e) => sendMouseClick(e, true)}
        onPointerUp={(e) => sendMouseClick(e, false)}
        onWheel={sendMouseWheel}
        onContextMenu={(e) => e.preventDefault()}
        onKeyDown={(e) => sendKey(e, true)}
        onKeyUp={(e) => sendKey(e, false)}
        className="h-full w-full object-contain outline-none"
      />
      <div className="absolute right-3 top-3 flex max-w-[min(720px,calc(100%-1.5rem))] flex-wrap justify-end gap-1.5">
        {SHORTCUTS.map((shortcut) => (
          <button
            key={shortcut.label}
            type="button"
            onClick={() => sendShortcut(shortcut.keys)}
            className="rounded border border-slate-500/50 bg-slate-950/80 px-2 py-1 text-[11px] font-medium text-slate-100 shadow-sm backdrop-blur transition hover:border-emerald-300/70 hover:bg-emerald-500/20"
          >
            {shortcut.label}
          </button>
        ))}
      </div>
      <div className="pointer-events-none absolute left-3 top-3 rounded bg-black/60 px-2 py-1 text-xs text-slate-300">
        {status}
      </div>
    </div>
  )
}
