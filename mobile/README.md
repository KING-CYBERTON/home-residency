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

Every merge to main that changes `mobile/` runs `.github/workflows/mobile-release.yml` once CI has passed. It builds signed release bundles of both apps and uploads them to the Play **internal testing** track. After testing, promote a build to production with **Actions → Mobile promote** (everyone, or a staged rollout such as `0.2`), which waits for approval on the `production` environment; or promote it by hand in the Play Console. Each build gets a new version code (minutes since 1 January 2026, plus 1000). Set the repository variable `MOBILE_STAGING_API_BASE_URL` to also get release APKs built against the staging API as run artifacts. See `docs/ci-cd.md`. The version name comes from `version:` in each app's `pubspec.yaml`.

One-time set-up:

1. In the Play Console, create the two apps with the package names `ke.co.homendresidence.homend_client` (Homend) and `ke.co.homendresidence.homend_provider` (Homend Provider), and turn on Play App Signing.
2. Create an upload key, used by both apps:
   `keytool -genkeypair -keystore upload.jks -alias upload -keyalg RSA -keysize 2048 -validity 10000`
3. Upload the first bundle of each app by hand in the Play Console, because Play does not accept an app's first upload through the API. Build it locally with an `android/key.properties` in the app folder (`storeFile`, `storePassword`, `keyAlias`, `keyPassword`; git ignores it) and `flutter build appbundle --release --dart-define=API_BASE_URL=<production API>`.
4. In Google Cloud, let GitHub Actions act as a Play service account **without a key** (Workload Identity Federation; many organisations block key creation with `iam.disableServiceAccountKeyCreation`). Only workflows of this repository running on `main` can use it. In Cloud Shell, with your project ID:

   ```sh
   PROJECT_ID=your-project-id
   PROJECT_NUMBER=$(gcloud projects describe "$PROJECT_ID" --format='value(projectNumber)')
   gcloud services enable iamcredentials.googleapis.com sts.googleapis.com androidpublisher.googleapis.com --project "$PROJECT_ID"
   gcloud iam service-accounts create homend-play --display-name "Homend Play publisher" --project "$PROJECT_ID"
   gcloud iam workload-identity-pools create github --location global --display-name "GitHub Actions" --project "$PROJECT_ID"
   gcloud iam workload-identity-pools providers create-oidc home-residency --location global --workload-identity-pool github \
     --issuer-uri https://token.actions.githubusercontent.com \
     --attribute-mapping 'google.subject=assertion.sub,attribute.repository_id=assertion.repository_id,attribute.ref=assertion.ref' \
     --attribute-condition "assertion.repository_id == '1418283878' && assertion.ref == 'refs/heads/main'" \
     --project "$PROJECT_ID"
   gcloud iam service-accounts add-iam-policy-binding "homend-play@$PROJECT_ID.iam.gserviceaccount.com" --project "$PROJECT_ID" \
     --role roles/iam.workloadIdentityUser \
     --member "principalSet://iam.googleapis.com/projects/$PROJECT_NUMBER/locations/global/workloadIdentityPools/github/attribute.repository_id/1418283878"
   echo "projects/$PROJECT_NUMBER/locations/global/workloadIdentityPools/github/providers/home-residency"
   ```

   `1418283878` is this repository's GitHub ID, which stays the same if the repository is renamed. Then in the Play Console under **Users and permissions**, invite `homend-play@<project>.iam.gserviceaccount.com` and give it release rights on both apps.
5. Add these GitHub repository secrets: `ANDROID_KEYSTORE_BASE64` (`base64 -w0 upload.jks`), `ANDROID_KEYSTORE_PASSWORD`, `ANDROID_KEY_ALIAS` and `ANDROID_KEY_PASSWORD`. Add these repository variables: `GCP_WORKLOAD_IDENTITY_PROVIDER` (the `projects/…/providers/home-residency` line the commands print), `PLAY_SERVICE_ACCOUNT` (`homend-play@<project>.iam.gserviceaccount.com`) and `MOBILE_API_BASE_URL` (the production API, ending in `/api/v1`). No Google key is stored anywhere.
6. Set the repository variable `PLAY_RELEASE_STATUS` to `draft` until each app has been released once on the internal track, then delete the variable.

Until the secrets and variables are set, the release workflow skips with a warning instead of failing. Mobile CI also builds signed release bundles with a throwaway key, so a broken release set-up shows up on the PR.

## Not covered yet

- Native push notifications (the in-app notification list is there; FCM/APNs registration through `/push-subscriptions` is not wired).
- App Store (iOS) release, app icons and store listings; the iOS folder is the `flutter create` default apart from the display names and permission texts.
