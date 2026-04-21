import { useState } from 'react'
import Sidebar from './components/Sidebar'
import Dashboard from './components/Dashboard'
import IsoManager from './components/IsoManager'
import ClientMonitor from './components/ClientMonitor'
import NetworkConfig from './components/NetworkConfig'
import RemoteControl from './components/RemoteControl'
import RemoteAgents from './components/RemoteAgents'

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
      case 'remote':
        return <RemoteControl />
      case 'agents':
        return <RemoteAgents />
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
