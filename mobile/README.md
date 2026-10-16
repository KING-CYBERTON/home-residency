# Homend mobile apps

Two Flutter apps on the JSON API (`backend/api/openapi.yaml`), mirroring the web portals:

- `apps/client` (**Homend**) for households: book a service, follow the booking, confirm visits, approve variations, rate, pay invoices by M-Pesa, manage addresses, plan, messages and complaints.
- `apps/provider` (**Homend Provider**) for service providers: apply and upload vetting documents, referees, training, sign the contract, set availability, accept offers, check in and out of visits (works offline), earnings and payouts, appraisals, messages.
- `packages/homend_core` shared by both: API client, models, session (phone + SMS code sign-in, token in the platform keychain), theme and shared screens (sign in, inbox, document upload).

It is a pub workspace, so one `flutter pub get` in this folder resolves everything.

## Run

```sh
cd mobile
flutter pub get
cd apps/client   # or apps/provider
flutter run --dart-define=API_BASE_URL=http://10.0.2.2:8080/api/v1
```

`API_BASE_URL` defaults to `http://10.0.2.2:8080/api/v1`, which is the host machine from the Android emulator. Use `http://localhost:8080/api/v1` on the iOS simulator and the real API URL for release builds.

Each app only accepts its own kind of account: a provider number signing in to the client app is signed straight back out with a pointer to the other app, as the portals do.

## Offline check-in

The provider app queues check-in, check-out and note events on the device (`lib/offline_queue.dart`) with a `client_event_id` and the time they happened, and sends them to `POST /provider/sync` when the connection comes back, the app resumes, or right after each event. The server answers each event as applied, duplicate or rejected; rejected events stay visible on the job until dismissed. Signing out warns about events that have not been sent.

## Checks

```sh
flutter analyze
(cd packages/homend_core && flutter test)
(cd apps/provider && flutter test)
(cd apps/client && flutter test)
```

`packages/homend_core/test/contract_test.dart` reads `backend/api/openapi.yaml` and fails when the app calls a path or method that is not in the contract, when a non-console operation is neither used nor listed as deliberately unused, or when a request body sends a field the schema does not define. A contract change that the apps have not caught up with fails CI (`.github/workflows/mobile-ci.yml`).

## Release to Google Play

Every merge to main that changes `mobile/` runs `.github/workflows/mobile-release.yml`. It builds signed release bundles of both apps and uploads them to the Play **internal testing** track; promote a build to production by hand in the Play Console. Each run gets a new version code (1000 + the run number). The version name comes from `version:` in each app's `pubspec.yaml`.

One-time set-up:

1. In the Play Console, create the two apps with the package names `ke.co.homendresidence.homend_client` (Homend) and `ke.co.homendresidence.homend_provider` (Homend Provider), and turn on Play App Signing.
2. Create an upload key, used by both apps:
   `keytool -genkeypair -keystore upload.jks -alias upload -keyalg RSA -keysize 2048 -validity 10000`
3. Upload the first bundle of each app by hand in the Play Console, because Play does not accept an app's first upload through the API. Build it locally with an `android/key.properties` in the app folder (`storeFile`, `storePassword`, `keyAlias`, `keyPassword`; git ignores it) and `flutter build appbundle --release --dart-define=API_BASE_URL=<production API>`.
4. In Google Cloud, create a service account with a JSON key. In the Play Console under Users and permissions, give it release rights on both apps.
5. Add these GitHub repository secrets: `ANDROID_KEYSTORE_BASE64` (`base64 -w0 upload.jks`), `ANDROID_KEYSTORE_PASSWORD`, `ANDROID_KEY_ALIAS`, `ANDROID_KEY_PASSWORD` and `PLAY_SERVICE_ACCOUNT_JSON` (the whole JSON key). Also add the repository variable `MOBILE_API_BASE_URL` (the production API, ending in `/api/v1`).
6. Set the repository variable `PLAY_RELEASE_STATUS` to `draft` until each app has been released once on the internal track, then delete the variable.

Until the secrets are set, the release workflow skips with a warning instead of failing. Mobile CI also builds signed release bundles with a throwaway key, so a broken release set-up shows up on the PR.

## Not covered yet

- Native push notifications (the in-app notification list is there; FCM/APNs registration through `/push-subscriptions` is not wired).
- App Store (iOS) release, app icons and store listings; the iOS folder is the `flutter create` default apart from the display names and permission texts.
