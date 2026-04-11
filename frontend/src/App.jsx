import { useState } from 'react'
import Sidebar from './components/Sidebar'
import Dashboard from './components/Dashboard'
import IsoManager from './components/IsoManager'
import ClientMonitor from './components/ClientMonitor'
import NetworkConfig from './components/NetworkConfig'

function App() {
  const [activeView, setActiveView] = useState('dashboard')

  const renderView = () => {
    switch (activeView) {
      case 'dashboard':
        return <Dashboard />
      case 'isos':
        return <IsoManager />
      case 'clients':
        return <ClientMonitor />
      case 'config':
        return <NetworkConfig />
      default:
        return <Dashboard />
    }
  }

  return (
    <div className="flex h-screen bg-slate-950 text-slate-300">
      <Sidebar activeView={activeView} onNavigate={setActiveView} />
      <main className="flex-1 overflow-auto p-6">
        {renderView()}
      </main>
    </div>
  )
}

export default App
