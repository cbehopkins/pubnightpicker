import { isRouteErrorResponse, useRouteError } from 'react-router-dom';
import MainNavigation from './MainNavigation';

import PageContent from './PageContent';

function ErrorPage() {
  const error = useRouteError();
  console.log("Error Object", error)

  let title = 'An error occurred!';
  let message = 'Something went wrong!';

  if (isRouteErrorResponse(error)) {
    if (error.status === 404) {
      title = 'Not found!';
      message = 'Could not find resource or page.';
    } else if (error.status === 500) {
      message =
        (typeof error.data === 'object' && error.data?.message) ||
        'Internal server error.';
    } else {
      title = `Something else ${error.status}`;
      message =
        (typeof error.data === 'object' && error.data?.message) ||
        `Request failed with status ${error.status}.`;
    }
  } else if (error instanceof Error) {
    // Handles runtime errors such as offline failed lazy imports.
    message = error.message || message;
  } else if (typeof error === 'string') {
    message = error;
  }

  return (
    <>
      <MainNavigation />
      <PageContent title={title}>
        <p>{message}</p>
      </PageContent>
    </>
  );
}

export default ErrorPage;