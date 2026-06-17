import { useState } from "react";
import { useArchiveAgentChat } from "@/hooks/useAgentChats";

// A selected archived session being viewed read-only.
export type ArchivedView = { sessionId: string; title: string; createdAt: string | null };

// useArchiveControls bundles the agent panel's archive UI state: the archived
// drawer toggle, the read-only selection, and the archive-current action
// (which freezes the chat and lets the canvas query swap in a fresh one).
export function useArchiveControls(
  organizationId: string,
  canvasId: string,
  chatId: string,
  onError: (message: string) => void,
) {
  const archiveMutation = useArchiveAgentChat(organizationId, canvasId);
  const [drawerOpen, setDrawerOpen] = useState(false);
  const [archivedView, setArchivedView] = useState<ArchivedView | null>(null);

  const archiveCurrent = async () => {
    try {
      await archiveMutation.mutateAsync({ chatId });
      setDrawerOpen(false);
      setArchivedView(null);
    } catch {
      onError("Failed to archive conversation. Please try again.");
    }
  };

  return { archiveMutation, drawerOpen, setDrawerOpen, archivedView, setArchivedView, archiveCurrent };
}
