import { ArrowLeft } from "lucide-react";
import { useMemo } from "react";
import { useChatScroll } from "@/components/AgentSidebar/useChatScroll";
import { useAgentChatMessages } from "@/hooks/useAgentChats";
import { ConversationTranscript } from "./AgentConversationTranscript";
import { formatArchiveDate } from "./archiveDate";
import { useConversationMessages } from "./agentConversationState";
import { groupMessages } from "./agentMessageGroups";

// Interactive widgets are inert in the read-only archived view.
const noop = async () => {};

// ArchivedTranscript renders a frozen, read-only view of an archived
// conversation: its transcript with a banner to return to the active chat. No
// composer, no websocket — the archived session has no live model context.
export function ArchivedTranscript({
  sessionId,
  organizationId,
  canvasId,
  title,
  createdAt,
  onBack,
}: {
  sessionId: string;
  organizationId: string;
  canvasId: string;
  title: string;
  createdAt: string | null;
  onBack: () => void;
}) {
  const messagesQuery = useAgentChatMessages(sessionId, organizationId, true);
  const messages = useConversationMessages(messagesQuery.data);
  const messageGroups = useMemo(() => groupMessages(messages), [messages]);
  const scrollRef = useChatScroll(messagesQuery, sessionId, messages.length, false);
  const dateLabel = formatArchiveDate(createdAt);

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div
        className="flex items-center gap-2 border-b border-amber-200 bg-amber-50 px-3 py-1.5"
        data-testid="archived-banner"
      >
        <button
          type="button"
          onClick={onBack}
          className="flex items-center gap-1 text-xs font-medium text-amber-800 transition-colors hover:text-amber-900"
          data-testid="archived-back"
        >
          <ArrowLeft className="size-3" /> Back to current
        </button>
        <span className="ml-auto truncate text-xs text-amber-700">
          Archived{dateLabel ? ` · ${dateLabel}` : ""} · {title || "Untitled conversation"}
        </span>
      </div>
      <ConversationTranscript
        error={null}
        canvasId={canvasId}
        organizationId={organizationId}
        messageGroups={messageGroups}
        isLoading={messagesQuery.isLoading}
        isLoadingMore={messagesQuery.isFetchingNextPage}
        onAction={noop}
        onStartBuilding={noop}
        scrollRef={scrollRef}
        showThinking={false}
      />
    </div>
  );
}
