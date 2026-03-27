# Pubnight picker

## Setting Up Firebase

To develop this app using your own Firebase database, follow these steps:

1. **Create a Firebase Project:**
   - Go to the [Firebase Console](https://console.firebase.google.com/).
   - Click on "Add project" and follow the instructions to create a new project.

2. **Add a Web App to the Firebase Project:**
   - In the Firebase Console, go to the project you just created.
   - Click on the gear icon next to "Project Overview" and select "Project settings".
   - Scroll down to "Your apps" and click on the `</>` (Web) icon to add a new web app.
   - Register the app and Firebase will provide you with a configuration object containing your API key and other necessary values.

3. **Update the `.env` File:**
   - Copy the configuration values provided by Firebase.
   - Open the `.env` file located in the `react` directory.
   - Replace the existing values with your new Firebase configuration values. The `.env` file should look like this:

```properties
VITE_FIREBASE_API_KEY="your-new-api-key"
VITE_FIREBASE_AUTH_DOMAIN="your-new-auth-domain"
VITE_FIREBASE_PROJECT_ID="your-new-project-id"
VITE_FIREBASE_STORAGE_BUCKET="your-new-storage-bucket"
VITE_FIREBASE_MESSAGING_SENDER_ID="your-new-messaging-sender-id"
VITE_FIREBASE_APP_ID="your-new-app-id"
```


## Development Servers

Do all of this under the react directory.

In one terminal, start the Vite dev server:
```
npm run dev
```

(`npm start` is still an alias, but `npm run dev` is the standard Vite command.)

In another terminal, start the Firebase emulators:
```
npx firebase-tools emulators:start --import=./db_dir
```

You should now be able to develop without affecting the production database.

If you want the data to persist, while the emulators are running, run this in another terminal:
```
npx firebase-tools emulators:export ./db_dir
```
This will save the current state.

## Auth Troubleshooting (Unauthorized Domain)

If you see an unauthorized-domain error during sign-in:

1. Make sure you are running the app on `localhost` or `127.0.0.1`.
2. This app now uses Firebase emulators by default in local dev.
3. Google popup sign-in is disabled while the Auth emulator is active.

For local emulator login, use email/password accounts from the imported emulator data.

If you need to test Google popup sign-in against your real Firebase project instead:

1. Add this to your `.env`:
```properties
VITE_USE_FIREBASE_EMULATORS="false"
```
2. In Firebase Console -> Authentication -> Settings -> Authorized domains, add:
   - `localhost`
   - `127.0.0.1`

## Deployment
Deployment is now handled by the github runners.
PRs get a temp URL generated that allows you to test it
<!-- # After Changes to deploy it
npm run build  
firebase deploy -->
