import React from 'react';
import ReactDOM from 'react-dom/client';
import 'bootstrap/dist/css/bootstrap.min.css';
import './bootstrap-overrides.css';
import './index.css';
import App from './App';
import reportWebVitals from './reportWebVitals';
import { Provider } from 'react-redux';
import { store } from './store';
import CrashBoundary from './components/UI/CrashBoundary';

function installVisualViewportInsetSync() {
  if (typeof window === 'undefined' || !window.visualViewport) {
    return;
  }

  const root = document.documentElement;

  const syncInsets = () => {
    const viewport = window.visualViewport;
    if (!viewport) {
      return;
    }

    const bottomInset = Math.max(0, window.innerHeight - (viewport.height + viewport.offsetTop));
    root.style.setProperty('--vv-bottom-inset', `${bottomInset}px`);
  };

  syncInsets();
  window.visualViewport.addEventListener('resize', syncInsets);
  window.visualViewport.addEventListener('scroll', syncInsets);
  window.addEventListener('resize', syncInsets);
}

installVisualViewportInsetSync();

const root = ReactDOM.createRoot(document.getElementById('root'));
root.render(
  <React.StrictMode>
    <Provider store={store}>
      <CrashBoundary>
        <App />
      </CrashBoundary>
    </Provider>
  </React.StrictMode>
);

reportWebVitals();