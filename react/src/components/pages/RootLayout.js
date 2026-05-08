import { Outlet } from 'react-router-dom';

import MainNavigation from './MainNavigation';
import PageContent from './PageContent';
import ToastCenter from '../UI/ToastCenter';
import PwaInstallBanner from '../UI/PwaInstallBanner';
import PwaUpdateBanner from '../UI/PwaUpdateBanner';

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
      <PwaUpdateBanner />
      <PwaInstallBanner />
      <main>
        <Outlet />
      </main>
      <ToastCenter />
    </>
  );
}

export default RootLayout;
