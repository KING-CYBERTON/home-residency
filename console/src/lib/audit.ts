import { humanize } from "@/lib/format";

/** Plain wording for audit actions; anything not listed reads from its code ("visit.check_in" -> "Visit check in"). */
const labels: Record<string, string> = {
  "user.register": "Signed up",
  "consent.record": "Accepted a notice",
  "session.revoke": "Signed out one of their sessions",
  "staff.sign_out": "Signed a colleague out everywhere",
  "staff.reset_password": "Reset a colleague's password",
  "application.create": "Started an application",
  "application.update": "Updated the application",
  "application.submit": "Submitted the application",
  "application.withdraw": "Withdrew the application",
  "application.claim": "Vetting officer claimed the application",
  "application.return": "Application returned as incomplete",
  "application.item_review": "Checklist item reviewed",
  "application.complete_vetting": "Vetting completed, sent for approval",
  "application.reverification": "Re-verification started",
  "referee.record": "Referee call recorded",
  "clearance.request": "Background check requested",
  "clearance.record": "Background check result recorded",
  "assessment.record": "Interview or assessment recorded",
  "document.verify": "Document verified",
  "document.reject": "Document rejected",
  "training.enrol": "Enrolled in training",
  "training.result": "Training result recorded",
  "contract.sign": "Signed the contract",
  "provider.profile_update": "Updated their profile",
  "provider.update": "Profile changed by staff",
  "provider.activate": "Activated",
  "provider.suspend": "Suspended",
  "provider.reinstate": "Reinstated",
  "provider.auto_suspend": "Suspended automatically",
  "provider.auto_reinstate": "Reinstated automatically",
  "assignment.create": "Offered a job",
  "assignment.accept": "Accepted a job",
  "assignment.decline": "Declined a job",
  "assignment.withdraw": "Job offer withdrawn",
  "visit.check_in": "Checked in",
  "visit.check_out": "Checked out",
  "complaint.open": "Complaint opened",
  "complaint.decision": "Complaint decided",
  "complaint.close": "Complaint closed",
  "earning.hold": "Earning held",
  "earning.release": "Earning released",
  "lead.update": "Lead followed up",
};

export function auditLabel(action: string | null | undefined): string {
  if (!action) return "—";
  return labels[action] ?? humanize(action.replace(/\./g, "_"));
}

type Entry = { action: string; occurred_at: string; actor_id?: string | null; actor_kind?: string | null };

/**
 * Folds runs of the same action by the same person into one row with a count, so twenty saves of
 * an application read as one line. Entries arrive newest first; `earliest` is the oldest in the run.
 */
export function collapseRuns<T extends Entry>(entries: T[] | undefined): (T & { repeats: number; earliest: string })[] | undefined {
  if (!entries) return undefined;
  const out: (T & { repeats: number; earliest: string })[] = [];
  for (const e of entries) {
    const last = out[out.length - 1];
    if (last && last.action === e.action && (last.actor_id ?? last.actor_kind) === (e.actor_id ?? e.actor_kind)) {
      last.repeats++;
      last.earliest = e.occurred_at;
      continue;
    }
    out.push({ ...e, repeats: 1, earliest: e.occurred_at });
  }
  return out;
}
