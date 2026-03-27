import { Outlet } from 'react-router-dom';

import MainNavigation from './MainNavigation';
import PageContent from './PageContent';
import ToastCenter from '../UI/ToastCenter';

function RootLayout({ database_error }) {
  if (database_error) {
    return <PageContent>
      <h1>Database Error</h1>
      <p>{database_error.message}</p>
    </PageContent>
  }
  return (
    <>
      <MainNavigation />
      <main>
        <Outlet />
      </main>
      <ToastCenter />
    </>
  );
}

export default RootLayout;
