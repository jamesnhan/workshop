import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { initTelemetry } from './telemetry'
import './index.css'
import App from './App.tsx'

// Bootstrap OTel before React renders — no-op when VITE_OTEL_ENABLED != true.
initTelemetry()

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <App />
  </StrictMode>,
)
