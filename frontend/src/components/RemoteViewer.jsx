import { useCallback, useEffect, useRef, useState } from 'react'

const MSG_FRAME = 0x01
const MSG_TILE = 0x03
const MSG_MOUSE_MOVE = 0x10
const MSG_MOUSE_CLICK = 0x11
const MSG_KEY_EVENT = 0x12

function putU16(view, offset, value) {
  view.setUint16(offset, Math.max(0, Math.min(65535, Math.round(value))), false)
}

function putU32(view, offset, value) {
  view.setUint32(offset, value >>> 0, false)
}

function keyToVK(event) {
  if (event.key && event.key.length === 1) {
    return event.key.toUpperCase().charCodeAt(0)
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
  }
  return map[event.key] || event.keyCode || event.which || 0
}

export default function RemoteViewer({ serverIP = '127.0.0.1', httpPort = 8080, className = '' }) {
  const canvasRef = useRef(null)
  const socketRef = useRef(null)
  const screenRef = useRef({ width: 0, height: 0 })
  const drawingRef = useRef(false)
  const pendingRef = useRef(null)
  const lastMoveRef = useRef(0)
  const [status, setStatus] = useState('Conectando')

  const wsURL = `${window.location.protocol === 'https:' ? 'wss' : 'ws'}://${serverIP}:${httpPort}/ws/remote?role=viewer`

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
        pendingRef.current = event.data
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
      while (pendingRef.current) {
        const packet = pendingRef.current
        pendingRef.current = null
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

  const sendKey = (event, down) => {
    const vk = keyToVK(event)
    if (!vk) return
    event.preventDefault()
    const buf = new ArrayBuffer(6)
    const view = new DataView(buf)
    view.setUint8(0, MSG_KEY_EVENT)
    putU32(view, 1, vk)
    view.setUint8(5, down ? 1 : 0)
    send(buf)
  }

  return (
    <div className={`relative h-full w-full bg-black ${className}`}>
      <canvas
        ref={canvasRef}
        tabIndex={0}
        onPointerMove={sendMouseMove}
        onPointerDown={(e) => sendMouseClick(e, true)}
        onPointerUp={(e) => sendMouseClick(e, false)}
        onContextMenu={(e) => e.preventDefault()}
        onKeyDown={(e) => sendKey(e, true)}
        onKeyUp={(e) => sendKey(e, false)}
        className="h-full w-full object-contain outline-none"
      />
      <div className="pointer-events-none absolute left-3 top-3 rounded bg-black/60 px-2 py-1 text-xs text-slate-300">
        {status}
      </div>
    </div>
  )
}
