import { FileText } from "lucide-react";
import { useState } from "react";
import { toast } from "sonner";

import { Button } from "@/components/ui/button";
import { errorMessage, fetchClient } from "@/api/client";

/**
 * Opens a vault document through a short-lived signed link. The API logs every
 * staff read with its purpose (document_access_log), so we always send one.
 *
 * The tab is opened while the click is still being handled and pointed at the
 * link once it arrives; opening it after the request would be blocked as a popup.
 */
export function DocumentButton({ id, purpose, label = "View" }: { id: string; purpose: string; label?: string }) {
  const [busy, setBusy] = useState(false);
  async function open() {
    setBusy(true);
    const tab = window.open("", "_blank");
    try {
      const { data } = await fetchClient.GET("/documents/{id}/download-url", {
        params: { path: { id }, query: { purpose } },
      });
      if (!data?.url) throw new Error("The document link could not be created.");
      if (tab) {
        tab.opener = null;
        tab.location.href = data.url;
      } else {
        // Popups are blocked: leave the console for the file rather than do nothing.
        window.location.assign(data.url);
      }
    } catch (err) {
      tab?.close();
      toast.error(`Could not open the document: ${errorMessage(err)}`);
    } finally {
      setBusy(false);
    }
  }
  return (
    <Button variant="ghost" size="sm" onClick={open} disabled={busy}>
      <FileText /> {busy ? "Opening…" : label}
    </Button>
  );
}
