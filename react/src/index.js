import React from 'react';
import ReactDOM from 'react-dom/client';
import 'bootstrap/dist/css/bootstrap.min.css'; // Phase 0: Bootstrap foundation
import './bootstrap-overrides.css'; // Phase 0: Custom theme overrides
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

// If you want to start measuring performance in your app, pass a function
// to log results (for example: reportWebVitals(console.log))
// or send to an analytics endpoint. Learn more: https://bit.ly/CRA-vitals
reportWebVitals();
