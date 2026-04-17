import { useState, useEffect } from 'react'
import { Disc3, FolderOpen, RefreshCw, ToggleLeft, ToggleRight, FileText, X } from 'lucide-react'
import { GetISOList, ScanISOs, ToggleISO, GetISODirectory, BrowseISODirectory, BrowseISOUnattend, ClearISOUnattend } from '../../wailsjs/go/main/App'
import { EventsOn } from '../../wailsjs/runtime/runtime'
import clsx from 'clsx'

const osColors = {
  Windows: 'text-blue-400 bg-blue-500/10 border-blue-500/30',
  Linux: 'text-amber-400 bg-amber-500/10 border-amber-500/30',
  WinPE: 'text-purple-400 bg-purple-500/10 border-purple-500/30',
  Utility: 'text-emerald-400 bg-emerald-500/10 border-emerald-500/30',
  Unknown: 'text-slate-400 bg-slate-500/10 border-slate-500/30',
}

export default function IsoManager() {
  const [isos, setIsos] = useState([])
  const [isoDir, setIsoDir] = useState('')
  const [scanning, setScanning] = useState(false)

  useEffect(() => {
    GetISODirectory().then(setIsoDir)
    GetISOList().then((list) => setIsos(list || []))

    const unsub = EventsOn('iso:list-changed', (list) => {
      setIsos(list || [])
    })
    return () => unsub()
  }, [])

  const handleScan = async () => {
    setScanning(true)
    try {
      const list = await ScanISOs()
      setIsos(list || [])
    } catch (e) {
      console.error('Scan error:', e)
    }
    setScanning(false)
  }

  const handleBrowse = async () => {
    try {
      const dir = await BrowseISODirectory()
      if (dir) {
        setIsoDir(dir)
        handleScan()
      }
    } catch (e) {
      console.error('Browse error:', e)
    }
  }

  const handleToggle = async (name, currentEnabled) => {
    try {
      await ToggleISO(name, !currentEnabled)
    } catch (e) {
      console.error('Toggle error:', e)
    }
  }

  const handleBrowseUnattend = async (name) => {
    try {
      await BrowseISOUnattend(name)
    } catch (e) {
      console.error('Browse unattend error:', e)
    }
  }

  const handleClearUnattend = async (name) => {
    try {
      await ClearISOUnattend(name)
    } catch (e) {
      console.error('Clear unattend error:', e)
    }
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h2 className="text-2xl font-bold text-white">ISO Manager</h2>
        <div className="flex items-center gap-2">
          <button
            onClick={handleScan}
            disabled={scanning}
            className="flex items-center gap-2 px-3 py-2 bg-slate-800 hover:bg-slate-700 border border-slate-600 rounded-lg text-sm text-slate-300 transition-colors"
          >
            <RefreshCw size={14} className={scanning ? 'animate-spin' : ''} />
            {scanning ? 'Scanning...' : 'Rescan'}
          </button>
        </div>
      </div>

      <div className="bg-slate-900 rounded-xl border border-slate-700 p-4">
        <div className="flex items-center gap-3">
          <FolderOpen size={18} className="text-slate-400" />
          <div className="flex-1">
            <p className="text-xs text-slate-500">ISO Directory</p>
            <p className="text-sm font-mono text-slate-300">{isoDir || 'Not configured'}</p>
          </div>
          <button
            onClick={handleBrowse}
            className="px-3 py-1.5 bg-blue-600 hover:bg-blue-500 rounded-lg text-sm text-white transition-colors"
          >
            Change
          </button>
        </div>
      </div>

      <div className="bg-slate-900 rounded-xl border border-slate-700 overflow-hidden">
        <table className="w-full">
          <thead>
            <tr className="border-b border-slate-700 text-xs text-slate-400 uppercase tracking-wider">
              <th className="text-left px-4 py-3">Name</th>
              <th className="text-left px-4 py-3">Size</th>
              <th className="text-left px-4 py-3">Type</th>
              <th className="text-left px-4 py-3">Arch</th>
              <th className="text-left px-4 py-3">Unattend</th>
              <th className="text-center px-4 py-3">Enabled</th>
            </tr>
          </thead>
          <tbody>
            {isos.length === 0 ? (
              <tr>
                <td colSpan={6} className="text-center py-12 text-slate-500">
                  <Disc3 size={32} className="mx-auto mb-2 opacity-50" />
                  <p>No ISO files found</p>
                  <p className="text-xs mt-1">Configure a directory and scan for ISOs</p>
                </td>
              </tr>
            ) : (
              isos.map((iso) => (
                <tr key={iso.name} className="border-b border-slate-800 hover:bg-slate-800/50 transition-colors">
                  <td className="px-4 py-3">
                    <div className="flex items-center gap-2">
                      <Disc3 size={16} className="text-slate-500" />
                      <span className="text-sm text-white font-medium">{iso.name}</span>
                    </div>
                  </td>
                  <td className="px-4 py-3 text-sm text-slate-400 font-mono">{iso.sizeHR}</td>
                  <td className="px-4 py-3">
                    <span className={clsx(
                      'text-xs px-2 py-0.5 rounded-full border',
                      osColors[iso.osType] || osColors.Unknown
                    )}>
                      {iso.osType}
                    </span>
                  </td>
                  <td className="px-4 py-3 text-sm text-slate-400">{iso.arch}</td>
                  <td className="px-4 py-3">
                    {(iso.osType === 'Windows' || iso.osType === 'WinPE') ? (
                      iso.unattendPath ? (
                        <div className="flex items-center gap-1">
                          <FileText size={14} className="text-emerald-400 flex-shrink-0" />
                          <span className="text-xs text-emerald-400 truncate max-w-[120px]" title={iso.unattendPath}>
                            {iso.unattendPath.split('\\').pop().split('/').pop()}
                          </span>
                          <button
                            onClick={() => handleClearUnattend(iso.name)}
                            className="ml-1 p-0.5 hover:bg-red-500/20 rounded text-red-400"
                            title="Quitar autounattend.xml"
                          >
                            <X size={12} />
                          </button>
                        </div>
                      ) : (
                        <button
                          onClick={() => handleBrowseUnattend(iso.name)}
                          className="flex items-center gap-1 px-2 py-1 text-xs bg-slate-800 hover:bg-slate-700 border border-slate-600 rounded text-slate-400 hover:text-white transition-colors"
                        >
                          <FileText size={12} />
                          Agregar
                        </button>
                      )
                    ) : (
                      <span className="text-xs text-slate-600">N/A</span>
                    )}
                  </td>
                  <td className="px-4 py-3 text-center">
                    <button
                      onClick={() => handleToggle(iso.name, iso.enabled)}
                      className="inline-flex items-center"
                    >
                      {iso.enabled ? (
                        <ToggleRight size={28} className="text-emerald-400" />
                      ) : (
                        <ToggleLeft size={28} className="text-slate-600" />
                      )}
                    </button>
                  </td>
                </tr>
              ))
            )}
          </tbody>
        </table>
      </div>
    </div>
  )
}
