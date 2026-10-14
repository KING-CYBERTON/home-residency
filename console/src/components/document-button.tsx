import { FileText } from "lucide-react";
import { useState } from "react";
import { toast } from "sonner";

import { Button } from "@/components/ui/button";
import { errorMessage, fetchClient } from "@/api/client";

/**
 * Opens a vault document through a short-lived signed link. The API logs every
 * staff read with its purpose (document_access_log), so we always send one.
 */
export function DocumentButton({ id, purpose, label = "View" }: { id: string; purpose: string; label?: string }) {
  const [busy, setBusy] = useState(false);
  async function open() {
    setBusy(true);
    try {
      const { data } = await fetchClient.GET("/documents/{id}/download-url", {
        params: { path: { id }, query: { purpose } },
      });
      if (data?.url) window.open(data.url, "_blank", "noopener,noreferrer");
    } catch (err) {
      toast.error(errorMessage(err));
    } finally {
      setBusy(false);
    }
  }
  return (
    <Button variant="ghost" size="sm" onClick={open} disabled={busy}>
      <FileText /> {label}
    </Button>
  );
}
