import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { AgentChatHeader } from "./AgentChatHeader";

describe("AgentChatHeader", () => {
  it("triggers open-archives and archive-current from the buttons", () => {
    const onOpenArchives = vi.fn();
    const onArchiveCurrent = vi.fn();
    render(
      <AgentChatHeader
        onOpenArchives={onOpenArchives}
        onArchiveCurrent={onArchiveCurrent}
        archiving={false}
        canArchive
      />,
    );

    fireEvent.click(screen.getByTestId("agent-archives-open"));
    expect(onOpenArchives).toHaveBeenCalledTimes(1);

    fireEvent.click(screen.getByTestId("agent-archive-current"));
    expect(onArchiveCurrent).toHaveBeenCalledTimes(1);
  });

  it("disables archive-current when it cannot archive or is archiving", () => {
    const { rerender } = render(
      <AgentChatHeader onOpenArchives={vi.fn()} onArchiveCurrent={vi.fn()} archiving={false} canArchive={false} />,
    );
    expect(screen.getByTestId("agent-archive-current")).toBeDisabled();

    rerender(<AgentChatHeader onOpenArchives={vi.fn()} onArchiveCurrent={vi.fn()} archiving canArchive />);
    expect(screen.getByTestId("agent-archive-current")).toBeDisabled();
  });
});
