if (import.meta.env.DEV && window.location.pathname === '/prototype/navigation') {
  import('./components/pages/navigation-prototype/prototype-entry');
} else {
  import('./application-entry');
}
