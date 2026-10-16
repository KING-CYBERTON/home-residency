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

## Not covered yet

- Native push notifications (the in-app notification list is there; FCM/APNs registration through `/push-subscriptions` is not wired).
- Store signing, app icons and release flavours; the Android and iOS folders are the `flutter create` defaults.
