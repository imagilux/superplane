import { ChevronLeft, ChevronRight, Loader2, X } from "lucide-react";
import { useState } from "react";
import { Button } from "@/components/ui/button";
import { useArchivedAgentChats } from "@/hooks/useAgentChats";
import { formatArchiveDate } from "./archiveDate";
import type { AgentChat } from "./types";

const PAGE_SIZE = 10;

// ArchivedSessionsDrawer overlays the agent panel with the canvas's archived
// conversations (newest first), paginated. Selecting one opens it read-only.
export function ArchivedSessionsDrawer({
  organizationId,
  canvasId,
  onSelect,
  onClose,
}: {
  organizationId: string;
  canvasId: string;
  onSelect: (chat: AgentChat) => void;
  onClose: () => void;
}) {
  const [page, setPage] = useState(1);
  const query = useArchivedAgentChats(organizationId, canvasId, page, PAGE_SIZE, true);
  const chats = query.data?.chats ?? [];
  const total = query.data?.total ?? 0;
  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE));

  return (
    <div className="absolute inset-0 z-20 flex flex-col bg-white" data-testid="archived-sessions-drawer">
      <div className="flex items-center justify-between border-b border-slate-200 px-3 py-2">
        <span className="text-sm font-medium">Archived conversations</span>
        <button
          type="button"
          onClick={onClose}
          className="text-slate-400 transition-colors hover:text-slate-600"
          aria-label="Close archived conversations"
          data-testid="archived-drawer-close"
        >
          <X size={16} />
        </button>
      </div>

      <div className="min-h-0 flex-1 overflow-y-auto p-2">
        {query.isLoading ? (
          <div className="flex items-center gap-2 px-2 py-3 text-xs text-slate-500">
            <Loader2 className="size-3 animate-spin" /> Loading…
          </div>
        ) : chats.length === 0 ? (
          <div className="px-2 py-6 text-center text-xs text-slate-500" data-testid="archived-empty">
            No archived conversations yet.
          </div>
        ) : (
          <ul className="space-y-1">
            {chats.map((chat) => (
              <li key={chat.id}>
                <button
                  type="button"
                  onClick={() => onSelect(chat)}
                  className="flex w-full items-center gap-2 rounded-md border border-slate-200 px-3 py-2 text-left text-sm transition-colors hover:bg-slate-50"
                  data-testid="archived-session-item"
                >
                  <span className="shrink-0 text-xs tabular-nums text-slate-500">
                    {formatArchiveDate(chat.createdAt)}
                  </span>
                  <span className="truncate text-slate-800">{chat.title || "Untitled conversation"}</span>
                </button>
              </li>
            ))}
          </ul>
        )}
      </div>

      {totalPages > 1 ? (
        <div className="flex items-center justify-center gap-3 border-t border-slate-200 px-3 py-2">
          <Button
            type="button"
            variant="outline"
            size="icon-xs"
            disabled={page <= 1}
            onClick={() => setPage((p) => Math.max(1, p - 1))}
            aria-label="Previous page"
            data-testid="archived-prev"
          >
            <ChevronLeft className="size-3" />
          </Button>
          <span className="text-xs tabular-nums text-slate-600" data-testid="archived-page-indicator">
            {page} / {totalPages}
          </span>
          <Button
            type="button"
            variant="outline"
            size="icon-xs"
            disabled={page >= totalPages}
            onClick={() => setPage((p) => Math.min(totalPages, p + 1))}
            aria-label="Next page"
            data-testid="archived-next"
          >
            <ChevronRight className="size-3" />
          </Button>
        </div>
      ) : null}
    </div>
  );
}
