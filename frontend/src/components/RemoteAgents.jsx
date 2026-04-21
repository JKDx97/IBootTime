import { useState, useEffect, useRef } from 'react'
import { Bot, Wifi, WifiOff, FileText, Notebook, RefreshCw, CheckCircle2, XCircle, Clock, Send, Loader2 } from 'lucide-react'
import { AgentListClients, AgentPing, AgentCreateTestFile, AgentOpenNotepad, AgentGetTasks } from '../../wailsjs/go/main/App'
import clsx from 'clsx'

const STATUS_COLORS = {
  online: { dot: 'bg-emerald-400 shadow-[0_0_8px_rgba(52,211,153,0.5)]', text: 'text-emerald-400', bg: 'bg-emerald-500/10 border-emerald-500/30' },
  offline: { dot: 'bg-slate-600', text: 'text-slate-500', bg: 'bg-slate-800/50 border-slate-700' },
}

const TASK_STATUS_ICON = {
  pending: <Clock size={14} className="text-amber-400" />,
  delivered: <Send size={14} className="text-blue-400" />,
  completed: <CheckCircle2 size={14} className="text-emerald-400" />,
  failed: <XCircle size={14} className="text-red-400" />,
}

function timeAgo(ts) {
  if (!ts) return '—'
  const diff = Math.floor(Date.now() / 1000 - ts)
  if (diff < 5) return 'ahora'
  if (diff < 60) return `hace ${diff}s`
  if (diff < 3600) return `hace ${Math.floor(diff / 60)}m`
  return `hace ${Math.floor(diff / 3600)}h`
}

export default function RemoteAgents() {
  const [clients, setClients] = useState([])
  const [selectedClient, setSelectedClient] = useState(null)
  const [tasks, setTasks] = useState([])
  const [loading, setLoading] = useState(false)
  const [actionLoading, setActionLoading] = useState('')
  const [error, setError] = useState('')
  const pollRef = useRef(null)

  // Poll clients every 5 seconds
  const fetchClients = async () => {
    try {
      const res = await AgentListClients()
      setClients(res || [])
      setError('')
    } catch (e) {
      setError('Servidor de agentes no disponible')
      setClients([])
    }
  }

  useEffect(() => {
    fetchClients()
    pollRef.current = setInterval(fetchClients, 5000)
    return () => clearInterval(pollRef.current)
  }, [])

  // Fetch tasks when a client is selected
  const fetchTasks = async (clientId) => {
    try {
      const res = await AgentGetTasks(clientId)
      setTasks(res || [])
    } catch {
      setTasks([])
    }
  }

  useEffect(() => {
    if (!selectedClient) { setTasks([]); return }
    fetchTasks(selectedClient)
    const iv = setInterval(() => fetchTasks(selectedClient), 3000)
    return () => clearInterval(iv)
  }, [selectedClient])

  const handleAction = async (clientId, action, label) => {
    setActionLoading(label)
    setError('')
    try {
      if (action === 'ping') await AgentPing(clientId)
      else if (action === 'create-test-file') await AgentCreateTestFile(clientId)
      else if (action === 'open-notepad') await AgentOpenNotepad(clientId)
      // Refresh tasks after queueing
      setTimeout(() => fetchTasks(clientId), 1000)
    } catch (e) {
      setError(String(e))
    }
    setActionLoading('')
  }

  const onlineCount = clients.filter(c => c.status === 'online').length

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-3">
          <Bot size={28} className="text-violet-400" />
          <div>
            <h2 className="text-2xl font-bold text-white">Agentes Remotos</h2>
            <p className="text-xs text-slate-500 mt-0.5">Administra PCs remotas post-instalacion</p>
          </div>
        </div>
        <button
          onClick={() => { setLoading(true); fetchClients().finally(() => setLoading(false)) }}
          className="flex items-center gap-2 px-3 py-2 rounded-lg bg-slate-800 border border-slate-700 text-slate-300 hover:bg-slate-700 transition-colors text-sm"
        >
          <RefreshCw size={14} className={loading ? 'animate-spin' : ''} />
          Actualizar
        </button>
      </div>

      {error && (
        <div className="bg-red-500/10 border border-red-500/30 rounded-xl px-4 py-3 text-sm text-red-400">
          {error}
        </div>
      )}

      {/* Stats bar */}
      <div className="grid grid-cols-3 gap-3">
        <div className="bg-slate-900 rounded-xl border border-slate-700 px-4 py-3">
          <p className="text-xs text-slate-400">Total Agentes</p>
          <p className="text-2xl font-bold text-white">{clients.length}</p>
        </div>
        <div className="bg-slate-900 rounded-xl border border-slate-700 px-4 py-3">
          <p className="text-xs text-slate-400">En Linea</p>
          <p className="text-2xl font-bold text-emerald-400">{onlineCount}</p>
        </div>
        <div className="bg-slate-900 rounded-xl border border-slate-700 px-4 py-3">
          <p className="text-xs text-slate-400">Desconectados</p>
          <p className="text-2xl font-bold text-slate-500">{clients.length - onlineCount}</p>
        </div>
      </div>

      <div className="grid grid-cols-1 lg:grid-cols-3 gap-4">
        {/* Client list */}
        <div className="lg:col-span-1 bg-slate-900 rounded-xl border border-slate-700 p-4">
          <h3 className="text-sm font-semibold text-white mb-3">Clientes Registrados</h3>
          {clients.length === 0 ? (
            <div className="text-center py-8">
              <WifiOff size={32} className="text-slate-600 mx-auto mb-2" />
              <p className="text-xs text-slate-500">No hay agentes conectados</p>
              <p className="text-xs text-slate-600 mt-1">Inicia el cliente Python en una PC remota</p>
            </div>
          ) : (
            <div className="space-y-2 max-h-96 overflow-auto">
              {clients.map((c) => {
                const sc = STATUS_COLORS[c.status] || STATUS_COLORS.offline
                const isSelected = selectedClient === c.client_id
                return (
                  <button
                    key={c.client_id}
                    onClick={() => setSelectedClient(c.client_id)}
                    className={clsx(
                      'w-full text-left px-3 py-2.5 rounded-lg border transition-all',
                      isSelected
                        ? 'bg-violet-500/10 border-violet-500/40'
                        : 'bg-slate-800/50 border-slate-700 hover:bg-slate-800'
                    )}
                  >
                    <div className="flex items-center gap-2">
                      <div className={clsx('w-2 h-2 rounded-full shrink-0', sc.dot)} />
                      <span className="text-sm font-medium text-white truncate">{c.hostname}</span>
                      <span className={clsx('ml-auto text-xs', sc.text)}>{c.status}</span>
                    </div>
                    <div className="flex items-center gap-3 mt-1 text-xs text-slate-500">
                      <span className="font-mono">{c.ip}</span>
                      <span className="truncate">{c.os_version}</span>
                    </div>
                    <div className="text-xs text-slate-600 mt-0.5">
                      Visto: {timeAgo(c.last_seen)}
                    </div>
                  </button>
                )
              })}
            </div>
          )}
        </div>

        {/* Action panel + task history */}
        <div className="lg:col-span-2 space-y-4">
          {/* Actions */}
          <div className="bg-slate-900 rounded-xl border border-slate-700 p-4">
            <h3 className="text-sm font-semibold text-white mb-3">Acciones</h3>
            {!selectedClient ? (
              <p className="text-xs text-slate-500 py-4 text-center">Selecciona un cliente para enviar acciones</p>
            ) : (
              <div className="grid grid-cols-3 gap-3">
                <ActionButton
                  icon={Wifi}
                  label="Verificar Conectividad"
                  color="blue"
                  loading={actionLoading === 'ping'}
                  onClick={() => handleAction(selectedClient, 'ping', 'ping')}
                />
                <ActionButton
                  icon={FileText}
                  label="Crear Archivo de Prueba"
                  color="emerald"
                  loading={actionLoading === 'create-test-file'}
                  onClick={() => handleAction(selectedClient, 'create-test-file', 'create-test-file')}
                />
                <ActionButton
                  icon={Notebook}
                  label="Abrir Notepad"
                  color="violet"
                  loading={actionLoading === 'open-notepad'}
                  onClick={() => handleAction(selectedClient, 'open-notepad', 'open-notepad')}
                />
              </div>
            )}
          </div>

          {/* Task history */}
          <div className="bg-slate-900 rounded-xl border border-slate-700 p-4">
            <h3 className="text-sm font-semibold text-white mb-3">Historial de Tareas</h3>
            {tasks.length === 0 ? (
              <p className="text-xs text-slate-500 py-4 text-center">
                {selectedClient ? 'No hay tareas para este cliente' : 'Selecciona un cliente'}
              </p>
            ) : (
              <div className="space-y-2 max-h-72 overflow-auto">
                {[...tasks].reverse().map((t) => (
                  <div
                    key={t.task_id}
                    className={clsx(
                      'flex items-start gap-3 px-3 py-2.5 rounded-lg border text-xs',
                      t.status === 'completed' ? 'bg-emerald-500/5 border-emerald-500/20' :
                      t.status === 'failed' ? 'bg-red-500/5 border-red-500/20' :
                      'bg-slate-800/50 border-slate-700'
                    )}
                  >
                    <div className="mt-0.5 shrink-0">
                      {TASK_STATUS_ICON[t.status] || TASK_STATUS_ICON.pending}
                    </div>
                    <div className="flex-1 min-w-0">
                      <div className="flex items-center gap-2">
                        <span className="font-semibold text-white">{t.task_type}</span>
                        <span className="text-slate-600 font-mono">{t.task_id}</span>
                        <span className="ml-auto text-slate-500">{timeAgo(t.created_at)}</span>
                      </div>
                      {t.result_output && (
                        <p className={clsx(
                          'mt-1 font-mono break-all',
                          t.status === 'completed' ? 'text-emerald-400/80' : 'text-red-400/80'
                        )}>
                          {t.result_output}
                        </p>
                      )}
                    </div>
                  </div>
                ))}
              </div>
            )}
          </div>
        </div>
      </div>
    </div>
  )
}

function ActionButton({ icon: Icon, label, color, loading, onClick }) {
  const colors = {
    blue: 'bg-blue-500/10 border-blue-500/30 text-blue-400 hover:bg-blue-500/20',
    emerald: 'bg-emerald-500/10 border-emerald-500/30 text-emerald-400 hover:bg-emerald-500/20',
    violet: 'bg-violet-500/10 border-violet-500/30 text-violet-400 hover:bg-violet-500/20',
  }
  return (
    <button
      onClick={onClick}
      disabled={loading}
      className={clsx(
        'flex flex-col items-center gap-2 p-4 rounded-xl border transition-all text-center',
        colors[color],
        loading && 'opacity-60 cursor-wait'
      )}
    >
      {loading ? <Loader2 size={24} className="animate-spin" /> : <Icon size={24} />}
      <span className="text-xs font-medium">{label}</span>
    </button>
  )
}
