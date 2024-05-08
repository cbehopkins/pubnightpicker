import { useRouteError } from 'react-router-dom';
import MainNavigation from './MainNavigation';

import PageContent from './PageContent';

function ErrorPage() {
  const error = useRouteError();
  console.log("Error Object", error)

  let title = 'An error occurred!';
  let message = 'Something went wrong!';

  if (error.status === 500) {
    message = error.data.message;
  } else if (error.status === 404) {
    title = 'Not found!';
    message = 'Could not find resource or page.';
  } else {
    title = `Something else ${error.status}`
    message = error.data.message;
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