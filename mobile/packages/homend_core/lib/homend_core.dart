/// Shared code for the Homend Residence client and provider apps: the typed API client for
/// `backend/api/openapi.yaml`, models, the session (email and password, or phone and SMS code), and the UI kit that mirrors
/// the web portals.
library;

export 'src/api/api_client.dart';
export 'src/api/homend_api.dart';
export 'src/config.dart';
export 'src/models/bookings.dart';
export 'src/models/catalogue.dart';
export 'src/models/identity.dart';
export 'src/models/inbox.dart';
export 'src/models/json.dart';
export 'src/models/money.dart';
export 'src/models/provider.dart';
export 'src/session/session.dart';
export 'src/ui/document_picker.dart';
export 'src/ui/format.dart';
export 'src/ui/inbox_screens.dart';
export 'src/ui/password_screen.dart';
export 'src/ui/sign_in_screen.dart';
export 'src/ui/theme.dart';
export 'src/ui/widgets.dart';
