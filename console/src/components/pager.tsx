import { ChevronLeft, ChevronRight } from "lucide-react";

import { Button } from "@/components/ui/button";
import type { useCursor } from "@/hooks/use-cursor";

export function Pager({
  pager,
  nextCursor,
}: {
  pager: ReturnType<typeof useCursor>;
  nextCursor: string | null | undefined;
}) {
  if (pager.page === 1 && !nextCursor) return null;
  return (
    <div className="flex items-center justify-end gap-2 pt-3">
      <span className="text-muted-foreground text-xs">Page {pager.page}</span>
      <Button variant="outline" size="sm" disabled={pager.page === 1} onClick={pager.prev}>
        <ChevronLeft /> Previous
      </Button>
      <Button variant="outline" size="sm" disabled={!nextCursor} onClick={() => nextCursor && pager.next(nextCursor)}>
        Next <ChevronRight />
      </Button>
    </div>
  );
}
