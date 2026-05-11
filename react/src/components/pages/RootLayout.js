import { Outlet } from 'react-router-dom';

import MainNavigation from './MainNavigation';
import PageContent from './PageContent';
import ToastCenter from '../UI/ToastCenter';
import PwaInstallBanner from '../UI/PwaInstallBanner';
import PwaUpdateBanner from '../UI/PwaUpdateBanner';
import OfflineBanner from '../UI/OfflineBanner';

function RootLayout({ database_error }) {
  if (database_error) {
    const message =
      typeof database_error === "string"
        ? database_error
        : database_error?.message || "An unknown database error occurred.";

    return <PageContent>
      <h1>Database Error</h1>
      <p>{message}</p>
    </PageContent>
  }
  return (
    <>
      <MainNavigation />
      <PwaUpdateBanner />
      <PwaInstallBanner />
      <OfflineBanner />
      <main>
        <Outlet />
      </main>
      <ToastCenter />
    </>
  );
}

export default RootLayout;
