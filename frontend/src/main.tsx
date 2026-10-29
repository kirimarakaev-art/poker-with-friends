import React from 'react'
import ReactDOM from 'react-dom/client'
import App from './App.tsx'
import { Admin } from './components/Admin.tsx'
import './index.css'

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    {window.location.pathname === '/admin' ? <Admin /> : <App />}
  </React.StrictMode>,
)
