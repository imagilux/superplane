import { Archive, History, Loader2 } from "lucide-react";
import { Button } from "@/components/ui/button";

// AgentChatHeader is the agent panel's top bar: open the archived-conversations
// drawer (left) and archive the current conversation to start fresh (right).
export function AgentChatHeader({
  onOpenArchives,
  onArchiveCurrent,
  archiving,
  canArchive,
}: {
  onOpenArchives: () => void;
  onArchiveCurrent: () => void;
  archiving: boolean;
  canArchive: boolean;
}) {
  return (
    <div className="flex items-center justify-between border-b border-slate-200 px-2 py-1.5">
      <Button
        type="button"
        variant="ghost"
        size="icon-sm"
        onClick={onOpenArchives}
        title="Archived conversations"
        aria-label="Archived conversations"
        data-testid="agent-archives-open"
      >
        <History className="size-4" />
      </Button>
      <Button
        type="button"
        variant="ghost"
        size="icon-sm"
        onClick={onArchiveCurrent}
        disabled={archiving || !canArchive}
        title="Archive conversation & start fresh"
        aria-label="Archive conversation"
        data-testid="agent-archive-current"
      >
        {archiving ? <Loader2 className="size-4 animate-spin" /> : <Archive className="size-4" />}
      </Button>
    </div>
  );
}
