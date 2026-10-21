"""Upload a bundle to Google Play, or promote a tested build to production.

Used by .github/workflows/mobile-release.yml (upload) and mobile-promote.yml (promote). There is
no service account key: google-github-actions/auth exchanges the job's GitHub OIDC token through
Workload Identity Federation for short-lived credentials of the Play service account and points
GOOGLE_APPLICATION_CREDENTIALS at them, which google.auth.default() picks up.

    python play.py upload  <package> <bundle.aab> [--track internal] [--status completed|draft]
    python play.py promote <package> [--version-code N] [--rollout 0.2] [--status completed|draft]

Both talk to the Google Play Developer API (androidpublisher v3) inside one edit: open it, change
it, commit it.
"""

import argparse
import sys

import google.auth
from google.auth.transport.requests import AuthorizedSession

API = "https://androidpublisher.googleapis.com/androidpublisher/v3/applications"
UPLOAD_API = "https://androidpublisher.googleapis.com/upload/androidpublisher/v3/applications"
SCOPE = "https://www.googleapis.com/auth/androidpublisher"


def check(resp):
    if resp.status_code >= 300:
        sys.exit(f"::error::{resp.request.method} {resp.url} -> {resp.status_code}: {resp.text}")
    return resp.json() if resp.content else {}


def session():
    creds, _ = google.auth.default(scopes=[SCOPE])
    return AuthorizedSession(creds)


def upload(s, args):
    base = f"{API}/{args.package}/edits"
    edit = check(s.post(base))["id"]
    with open(args.bundle, "rb") as f:
        bundle = check(s.post(f"{UPLOAD_API}/{args.package}/edits/{edit}/bundles", params={"uploadType": "media"},
                              data=f, headers={"Content-Type": "application/octet-stream"}, timeout=600))
    code = str(bundle["versionCode"])
    check(s.put(f"{base}/{edit}/tracks/{args.track}",
                json={"track": args.track, "releases": [{"versionCodes": [code], "status": args.status}]}))
    check(s.post(f"{base}/{edit}:commit"))
    print(f"Uploaded {args.package} version code {code} to the {args.track} track ({args.status})")


def promote(s, args):
    if not 0 < args.rollout <= 1:
        sys.exit("::error::--rollout must be above 0 and at most 1")
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


def main():
    ap = argparse.ArgumentParser()
    sub = ap.add_subparsers(dest="command", required=True)
    up = sub.add_parser("upload")
    up.add_argument("package")
    up.add_argument("bundle")
    up.add_argument("--track", default="internal")
    up.add_argument("--status", default="completed", choices=["completed", "draft"],
                    help="draft until the app has been released once in the Play Console")
    pr = sub.add_parser("promote")
    pr.add_argument("package")
    pr.add_argument("--version-code", type=int, help="internal-track version code to promote (default: the newest)")
    pr.add_argument("--rollout", type=float, default=1.0, help="fraction of users, 0 < x <= 1 (default 1: everyone)")
    pr.add_argument("--status", default="completed", choices=["completed", "draft"],
                    help="draft leaves the release for you to send from the Play Console")
    args = ap.parse_args()
    (upload if args.command == "upload" else promote)(session(), args)


if __name__ == "__main__":
    main()
