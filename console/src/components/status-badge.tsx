import { Badge } from "@/components/ui/badge";
import { humanize } from "@/lib/format";

type Variant = "default" | "secondary" | "destructive" | "outline" | "warning" | "success" | "info";

// Statuses share a vocabulary across modules, so one map covers them all.
const variants: Record<string, Variant> = {
  // waiting on staff
  submitted: "warning",
  uploaded: "warning",
  new: "warning",
  contacted: "info",
  qualified: "info",
  converted: "success",
  triaging: "info",
  in_vetting: "info",
  awaiting_approval: "warning",
  awaiting_decision: "warning",
  awaiting_provider_account: "info",
  investigating: "info",
  open: "warning",
  pending: "warning",
  requested: "warning",
  pending_authorisation: "warning",
  draft: "secondary",
  incomplete: "warning",
  held: "warning",
  pending_verification: "warning",
  // in motion
  awaiting_client_choice: "info",
  awaiting_provider: "info",
  offered: "info",
  confirmed: "default",
  in_progress: "default",
  checked_in: "default",
  scheduled: "secondary",
  processing: "info",
  initiated: "info",
  approved: "success",
  issued: "info",
  partially_paid: "warning",
  in_payout: "info",
  awaiting_rating: "info",
  // good outcomes
  active: "success",
  accepted: "success",
  completed: "success",
  client_confirmed: "success",
  closed: "secondary",
  paid: "success",
  succeeded: "success",
  verified: "success",
  satisfied: "success",
  passed: "success",
  attended: "success",
  positive: "success",
  authentic: "success",
  payable: "success",
  in_effect: "default",
  decided: "default",
  waived: "secondary",
  // bad outcomes
  cancelled: "outline",
  inactive: "outline",
  withdrawn: "outline",
  expired: "outline",
  declined: "outline",
  replaced: "outline",
  void: "outline",
  revoked: "outline",
  unfilled: "destructive",
  rejected: "destructive",
  failed: "destructive",
  missed: "destructive",
  no_show: "destructive",
  suspended: "destructive",
  disqualified: "destructive",
  deferred: "warning",
  negative: "destructive",
  not_authentic: "destructive",
  adverse: "destructive",
  reversed: "destructive",
  refunded: "outline",
  mixed: "warning",
  unreachable: "outline",
  // severities
  safeguarding: "destructive",
  loss: "destructive",
  conduct: "warning",
  quality: "secondary",
};

/** Statuses whose stored name reads differently from what the same state is called elsewhere. */
const labels: Record<string, string> = {
  // A document waiting for a check: the checklist item it fills says "Submitted".
  uploaded: "Submitted",
  pending_upload: "Upload not finished",
};

export function StatusBadge({ value }: { value: string | null | undefined }) {
  if (!value) return <span className="text-muted-foreground">—</span>;
  return <Badge variant={variants[value] ?? "secondary"}>{labels[value] ?? humanize(value)}</Badge>;
}
