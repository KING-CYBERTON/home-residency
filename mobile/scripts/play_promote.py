"""Promote the newest internal-testing release of an app to the Play production track.

Used by .github/workflows/mobile-promote.yml. Reads the service account key from
PLAY_SERVICE_ACCOUNT_JSON and talks to the Google Play Developer API (androidpublisher v3):
open an edit, read the internal track, put the same version codes on production (all users,
or a staged rollout to a fraction of them), and commit the edit.

    python play_promote.py <package> [--version-code N] [--rollout 0.2] [--status completed|draft]
"""

import argparse
import json
import os
import sys

from google.auth.transport.requests import AuthorizedSession
from google.oauth2 import service_account

API = "https://androidpublisher.googleapis.com/androidpublisher/v3/applications"
SCOPE = "https://www.googleapis.com/auth/androidpublisher"


def check(resp):
    if resp.status_code >= 300:
        sys.exit(f"::error::{resp.request.method} {resp.url} -> {resp.status_code}: {resp.text}")
    return resp.json() if resp.content else {}


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("package")
    ap.add_argument("--version-code", type=int, help="internal-track version code to promote (default: the newest)")
    ap.add_argument("--rollout", type=float, default=1.0, help="fraction of users, 0 < x <= 1 (default 1: everyone)")
    ap.add_argument("--status", default="completed", choices=["completed", "draft"],
                    help="draft leaves the release for you to send from the Play Console")
    args = ap.parse_args()
    if not 0 < args.rollout <= 1:
        sys.exit("::error::--rollout must be above 0 and at most 1")

    info = json.loads(os.environ["PLAY_SERVICE_ACCOUNT_JSON"])
    creds = service_account.Credentials.from_service_account_info(info, scopes=[SCOPE])
    s = AuthorizedSession(creds)
    base = f"{API}/{args.package}/edits"

    edit = check(s.post(base))["id"]
    internal = check(s.get(f"{base}/{edit}/tracks/internal"))
    releases = [r for r in internal.get("releases", []) if r.get("versionCodes")]
    if not releases:
        sys.exit(f"::error::{args.package} has no release on the internal track to promote")
    if args.version_code:
        release = next((r for r in releases if str(args.version_code) in r["versionCodes"]), None)
        if release is None:
            sys.exit(f"::error::Version code {args.version_code} is not on the internal track of {args.package}")
    else:
        release = max(releases, key=lambda r: max(int(v) for v in r["versionCodes"]))

    promoted = {"name": release.get("name"), "versionCodes": release["versionCodes"]}
    if release.get("releaseNotes"):
        promoted["releaseNotes"] = release["releaseNotes"]
    if args.status == "draft":
        promoted["status"] = "draft"
    elif args.rollout < 1:
        promoted.update(status="inProgress", userFraction=args.rollout)
    else:
        promoted["status"] = "completed"

    check(s.put(f"{base}/{edit}/tracks/production", json={"track": "production", "releases": [promoted]}))
    check(s.post(f"{base}/{edit}:commit"))
    print(f"Promoted {args.package} {promoted['name']} (version codes {', '.join(promoted['versionCodes'])}) "
          f"to production: {promoted['status']}" + (f" at {args.rollout:.0%}" if args.rollout < 1 else ""))


if __name__ == "__main__":
    main()
