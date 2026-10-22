import React from 'react';
import ReactDOM from 'react-dom/client';
import App from './App';
import './index.css';

// Enforce light mode only
document.documentElement.classList.remove('dark');
document.documentElement.classList.add('light');
try {
  localStorage.removeItem('catalog-engine-theme');
} catch (_) {}

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <App />
  </React.StrictMode>
);
