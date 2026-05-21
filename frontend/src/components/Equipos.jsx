import { useState, useEffect, useRef } from 'react'
import {
  Laptop, Wifi, WifiOff, RefreshCw, Cpu, MemoryStick, HardDrive,
  Battery, Thermometer, Shield, ChevronRight, AlertTriangle,
  CheckCircle2, Loader2, Server, Hash, Monitor, Activity
} from 'lucide-react'
import {
  AgentListClients, AgentGetHardware, AgentGetSystemInfo,
  AgentUpdateHardware, AgentGetTasks
} from '../../wailsjs/go/main/App'
import clsx from 'clsx'

const STATUS_COLORS = {
  online: { dot: 'bg-emerald-400 shadow-[0_0_8px_rgba(52,211,153,0.5)]', text: 'text-emerald-400' },
  offline: { dot: 'bg-slate-600', text: 'text-slate-500' },
}

function timeAgo(ts) {
  if (!ts) return '—'
  const diff = Math.floor(Date.now() / 1000 - ts)
  if (diff < 5) return 'ahora'
  if (diff < 60) return `hace ${diff}s`
  if (diff < 3600) return `hace ${Math.floor(diff / 60)}m`
  return `hace ${Math.floor(diff / 3600)}h`
}

function DiskBar({ used, total }) {
  const pct = total > 0 ? Math.round(((total - used) / total) * 100) : 0
  const usedPct = 100 - pct
  const color = usedPct > 90 ? 'bg-red-500' : usedPct > 75 ? 'bg-amber-500' : 'bg-emerald-500'
  return (
    <div className="flex items-center gap-2 w-full">
      <div className="flex-1 h-2.5 bg-slate-700 rounded-full overflow-hidden">
        <div className={clsx('h-full rounded-full transition-all', color)} style={{ width: `${usedPct}%` }} />
      </div>
      <span className="text-xs text-slate-400 shrink-0 w-10 text-right">{usedPct}%</span>
    </div>
  )
}

function SpecRow({ icon: Icon, label, value, iconColor = 'text-slate-400' }) {
  return (
    <div className="flex items-start gap-3 py-2">
      <Icon size={16} className={clsx('mt-0.5 shrink-0', iconColor)} />
      <div className="min-w-0 flex-1">
        <p className="text-xs text-slate-500">{label}</p>
        <p className="text-sm text-white font-medium break-words">{value || '—'}</p>
      </div>
    </div>
  )
}

export default function Equipos() {
  const [clients, setClients] = useState([])
  const [selectedId, setSelectedId] = useState(null)
  const [hwData, setHwData] = useState(null)
  const [loading, setLoading] = useState(false)
  const [refreshing, setRefreshing] = useState(false)
  const [error, setError] = useState('')
  const pollRef = useRef(null)

  // Poll clients
  const fetchClients = async () => {
    try {
      const res = await AgentListClients()
      setClients(res || [])
      setError('')
    } catch {
      setError('Servidor de agentes no disponible')
      setClients([])
    }
  }

  useEffect(() => {
    fetchClients()
    pollRef.current = setInterval(fetchClients, 5000)
    return () => clearInterval(pollRef.current)
  }, [])

  // Load hardware when selecting a client
  const loadHardware = async (clientId) => {
    setLoading(true)
    try {
      const res = await AgentGetHardware(clientId)
      setHwData(res)
    } catch {
      setHwData(null)
    }
    setLoading(false)
  }

  useEffect(() => {
    if (!selectedId) { setHwData(null); return }
    loadHardware(selectedId)
  }, [selectedId])

  // Refresh diagnostics: queue system_info task, wait, then update
  const handleRefresh = async () => {
    if (!selectedId) return
    setRefreshing(true)
    setError('')
    try {
      await AgentGetSystemInfo(selectedId)
      // Wait for task to complete (poll for up to 40s)
      let attempts = 0
      const maxAttempts = 16
      const waitAndCheck = () => new Promise((resolve) => {
        const iv = setInterval(async () => {
          attempts++
          try {
            const tasks = await AgentGetTasks(selectedId)
            const sysTask = [...(tasks || [])].reverse().find(t => t.task_type === 'system_info')
            if (sysTask && (sysTask.status === 'completed' || sysTask.status === 'failed')) {
              clearInterval(iv)
              resolve(sysTask.status === 'completed')
            }
          } catch { /* ignore */ }
          if (attempts >= maxAttempts) { clearInterval(iv); resolve(false) }
        }, 2500)
      })
      const ok = await waitAndCheck()
      if (ok) {
        await AgentUpdateHardware(selectedId)
        await loadHardware(selectedId)
      } else {
        setError('No se pudo obtener informacion actualizada')
      }
    } catch (e) {
      setError(String(e))
    }
    setRefreshing(false)
  }

  const selected = clients.find(c => c.client_id === selectedId)
  const hw = hwData?.hardware
  const diag = hwData?.diagnostics
  const onlineCount = clients.filter(c => c.status === 'online').length

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-3">
          <Laptop size={28} className="text-cyan-400" />
          <div>
            <h2 className="text-2xl font-bold text-white">Equipos</h2>
            <p className="text-xs text-slate-500 mt-0.5">Inventario y diagnosticos de PCs instaladas</p>
          </div>
        </div>
        <button
          onClick={() => { fetchClients() }}
          className="flex items-center gap-2 px-3 py-2 rounded-lg bg-slate-800 border border-slate-700 text-slate-300 hover:bg-slate-700 transition-colors text-sm"
        >
          <RefreshCw size={14} />
          Actualizar Lista
        </button>
      </div>

      {error && (
        <div className="bg-red-500/10 border border-red-500/30 rounded-xl px-4 py-3 text-sm text-red-400">
          {error}
        </div>
      )}

      {/* Stats */}
      <div className="grid grid-cols-3 gap-3">
        <div className="bg-slate-900 rounded-xl border border-slate-700 px-4 py-3">
          <p className="text-xs text-slate-400">Total Equipos</p>
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
          <h3 className="text-sm font-semibold text-white mb-3">Equipos Registrados</h3>
          {clients.length === 0 ? (
            <div className="text-center py-8">
              <WifiOff size={32} className="text-slate-600 mx-auto mb-2" />
              <p className="text-xs text-slate-500">No hay equipos conectados</p>
              <p className="text-xs text-slate-600 mt-1">Los equipos aparecen al instalar una ISO con el agente</p>
            </div>
          ) : (
            <div className="space-y-2 max-h-[520px] overflow-auto">
              {clients.map((c) => {
                const sc = STATUS_COLORS[c.status] || STATUS_COLORS.offline
                const isSelected = selectedId === c.client_id
                const sysInfo = c.hardware?.system
                return (
                  <button
                    key={c.client_id}
                    onClick={() => setSelectedId(c.client_id)}
                    className={clsx(
                      'w-full text-left px-3 py-2.5 rounded-lg border transition-all',
                      isSelected
                        ? 'bg-cyan-500/10 border-cyan-500/40'
                        : 'bg-slate-800/50 border-slate-700 hover:bg-slate-800'
                    )}
                  >
                    <div className="flex items-center gap-2">
                      <div className={clsx('w-2 h-2 rounded-full shrink-0', sc.dot)} />
                      <span className="text-sm font-medium text-white truncate">{c.hostname}</span>
                      <ChevronRight size={14} className={clsx('ml-auto transition-transform', isSelected && 'rotate-90', 'text-slate-600')} />
                    </div>
                    <div className="flex items-center gap-3 mt-1 text-xs text-slate-500">
                      <span className="font-mono">{c.ip}</span>
                      {sysInfo?.Model && <span className="truncate">{sysInfo.Model}</span>}
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

        {/* Detail panel */}
        <div className="lg:col-span-2 space-y-4">
          {!selectedId ? (
            <div className="bg-slate-900 rounded-xl border border-slate-700 p-12 text-center">
              <Monitor size={48} className="text-slate-700 mx-auto mb-3" />
              <p className="text-slate-500 text-sm">Selecciona un equipo para ver sus especificaciones</p>
            </div>
          ) : loading ? (
            <div className="bg-slate-900 rounded-xl border border-slate-700 p-12 text-center">
              <Loader2 size={32} className="text-cyan-400 mx-auto mb-3 animate-spin" />
              <p className="text-slate-400 text-sm">Cargando informacion...</p>
            </div>
          ) : (
            <>
              {/* System header */}
              <div className="bg-slate-900 rounded-xl border border-slate-700 p-4">
                <div className="flex items-center justify-between mb-3">
                  <div className="flex items-center gap-3">
                    <Server size={20} className="text-cyan-400" />
                    <div>
                      <h3 className="text-lg font-bold text-white">{selected?.hostname || '—'}</h3>
                      <p className="text-xs text-slate-500">
                        {hw?.system?.Manufacturer} {hw?.system?.Model}
                        {hw?.system?.SystemFamily ? ` (${hw.system.SystemFamily})` : ''}
                      </p>
                    </div>
                  </div>
                  <button
                    onClick={handleRefresh}
                    disabled={refreshing}
                    className={clsx(
                      'flex items-center gap-2 px-3 py-2 rounded-lg border text-sm transition-all',
                      'bg-cyan-500/10 border-cyan-500/30 text-cyan-400 hover:bg-cyan-500/20',
                      refreshing && 'opacity-60 cursor-wait'
                    )}
                  >
                    {refreshing ? <Loader2 size={14} className="animate-spin" /> : <RefreshCw size={14} />}
                    Actualizar Diagnosticos
                  </button>
                </div>

                <div className="grid grid-cols-2 md:grid-cols-4 gap-3 text-xs">
                  <div className="bg-slate-800 rounded-lg px-3 py-2">
                    <p className="text-slate-500">IP</p>
                    <p className="text-white font-mono">{selected?.ip}</p>
                  </div>
                  <div className="bg-slate-800 rounded-lg px-3 py-2">
                    <p className="text-slate-500">MAC</p>
                    <p className="text-white font-mono">{selected?.mac || '—'}</p>
                  </div>
                  <div className="bg-slate-800 rounded-lg px-3 py-2">
                    <p className="text-slate-500">Serial Number</p>
                    <p className="text-white font-mono">{hw?.serial_number || '—'}</p>
                  </div>
                  <div className="bg-slate-800 rounded-lg px-3 py-2">
                    <p className="text-slate-500">OS</p>
                    <p className="text-white truncate">{selected?.os_version || '—'}</p>
                  </div>
                </div>
              </div>

              {/* Specs */}
              <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                {/* CPU */}
                <div className="bg-slate-900 rounded-xl border border-slate-700 p-4">
                  <h4 className="text-sm font-semibold text-white mb-2 flex items-center gap-2">
                    <Cpu size={16} className="text-blue-400" /> Procesador
                  </h4>
                  <p className="text-sm text-white font-medium">{hw?.cpu?.Name || '—'}</p>
                  <div className="flex gap-4 mt-2 text-xs text-slate-400">
                    <span>{hw?.cpu?.NumberOfCores || '?'} cores</span>
                    <span>{hw?.cpu?.NumberOfLogicalProcessors || '?'} threads</span>
                    <span>{hw?.cpu?.MaxClockSpeed ? `${hw.cpu.MaxClockSpeed} MHz` : ''}</span>
                  </div>
                </div>

                {/* RAM */}
                <div className="bg-slate-900 rounded-xl border border-slate-700 p-4">
                  <h4 className="text-sm font-semibold text-white mb-2 flex items-center gap-2">
                    <MemoryStick size={16} className="text-violet-400" /> Memoria RAM
                  </h4>
                  <p className="text-2xl font-bold text-white">{hw?.ram?.total_gb || 0} GB</p>
                  {hw?.ram?.modules?.length > 0 && (
                    <div className="mt-2 space-y-1">
                      {hw.ram.modules.map((m, i) => (
                        <p key={i} className="text-xs text-slate-400">
                          Slot {i + 1}: {m.Capacity ? `${Math.round(m.Capacity / (1024 ** 3))} GB` : '?'}
                          {m.Speed ? ` @ ${m.Speed} MHz` : ''}
                          {m.Manufacturer ? ` — ${m.Manufacturer.trim()}` : ''}
                        </p>
                      ))}
                    </div>
                  )}
                </div>
              </div>

              {/* Disks */}
              <div className="bg-slate-900 rounded-xl border border-slate-700 p-4">
                <h4 className="text-sm font-semibold text-white mb-3 flex items-center gap-2">
                  <HardDrive size={16} className="text-amber-400" /> Almacenamiento
                </h4>
                {hw?.disks?.length > 0 ? (
                  <div className="space-y-3">
                    {hw.disks.map((d, i) => (
                      <div key={i} className="bg-slate-800 rounded-lg px-4 py-3">
                        <div className="flex items-center justify-between mb-1.5">
                          <span className="text-sm text-white font-medium">{d.drive}</span>
                          <span className="text-xs text-slate-400">
                            {d.free_gb} GB libres de {d.total_gb} GB
                          </span>
                        </div>
                        <DiskBar used={d.free_gb} total={d.total_gb} />
                      </div>
                    ))}
                  </div>
                ) : (
                  <p className="text-xs text-slate-500">Sin datos de disco</p>
                )}
              </div>

              {/* Diagnostics */}
              <div className="bg-slate-900 rounded-xl border border-slate-700 p-4">
                <h4 className="text-sm font-semibold text-white mb-3 flex items-center gap-2">
                  <Activity size={16} className="text-cyan-400" /> Diagnosticos
                </h4>
                <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
                  {/* SMART */}
                  <div className="bg-slate-800 rounded-lg px-4 py-3">
                    <div className="flex items-center gap-2 mb-2">
                      <Shield size={14} className="text-emerald-400" />
                      <span className="text-xs font-semibold text-white">Salud Disco (SMART)</span>
                    </div>
                    {diag?.disk_smart?.length > 0 ? (
                      <div className="space-y-1">
                        {diag.disk_smart.map((s, i) => (
                          <div key={i} className="flex items-center gap-2 text-xs">
                            {s.predict_failure ? (
                              <AlertTriangle size={12} className="text-red-400 shrink-0" />
                            ) : (
                              <CheckCircle2 size={12} className="text-emerald-400 shrink-0" />
                            )}
                            <span className={s.predict_failure ? 'text-red-400' : 'text-emerald-400'}>
                              {s.status}
                            </span>
                            <span className="text-slate-500 truncate">{s.disk}</span>
                          </div>
                        ))}
                      </div>
                    ) : (
                      <p className="text-xs text-slate-500">Sin datos SMART</p>
                    )}
                  </div>

                  {/* Battery */}
                  <div className="bg-slate-800 rounded-lg px-4 py-3">
                    <div className="flex items-center gap-2 mb-2">
                      <Battery size={14} className="text-amber-400" />
                      <span className="text-xs font-semibold text-white">Bateria</span>
                    </div>
                    {diag?.battery ? (
                      <div className="space-y-1.5">
                        <div className="flex items-baseline gap-2">
                          <span className="text-2xl font-bold text-white">{diag.battery.charge_percent}%</span>
                          <span className="text-xs text-slate-400">{diag.battery.status}</span>
                        </div>
                        <p className="text-xs text-slate-400">
                          Salud: <span className={clsx(
                            'font-medium',
                            diag.battery.health_percent > 80 ? 'text-emerald-400' :
                            diag.battery.health_percent > 50 ? 'text-amber-400' : 'text-red-400'
                          )}>{diag.battery.health_percent}%</span>
                        </p>
                        {diag.battery.design_capacity > 0 && (
                          <p className="text-xs text-slate-500">
                            Capacidad: {diag.battery.full_charge_capacity} / {diag.battery.design_capacity} mWh
                          </p>
                        )}
                      </div>
                    ) : (
                      <p className="text-xs text-slate-500">Sin bateria (escritorio)</p>
                    )}
                  </div>

                  {/* Temperature */}
                  <div className="bg-slate-800 rounded-lg px-4 py-3">
                    <div className="flex items-center gap-2 mb-2">
                      <Thermometer size={14} className="text-orange-400" />
                      <span className="text-xs font-semibold text-white">Temperatura</span>
                    </div>
                    {diag?.temperature?.length > 0 ? (
                      <div className="space-y-1">
                        {diag.temperature.map((t, i) => (
                          <div key={i} className="flex items-center justify-between text-xs">
                            <span className="text-slate-400 truncate mr-2">{t.sensor}</span>
                            <span className={clsx(
                              'font-mono font-medium shrink-0',
                              t.value_c > 85 ? 'text-red-400' :
                              t.value_c > 70 ? 'text-amber-400' : 'text-emerald-400'
                            )}>{t.value_c}&deg;C</span>
                          </div>
                        ))}
                      </div>
                    ) : (
                      <p className="text-xs text-slate-500">
                        Sin datos de temperatura
                      </p>
                    )}
                  </div>
                </div>
              </div>
            </>
          )}
        </div>
      </div>
    </div>
  )
}
