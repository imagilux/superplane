import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { useArchivedAgentChats } from "@/hooks/useAgentChats";
import { ArchivedSessionsDrawer } from "./ArchivedSessionsDrawer";
import type { AgentChat } from "./types";

vi.mock("@/hooks/useAgentChats", () => ({
  useArchivedAgentChats: vi.fn(),
}));

const mockUseArchived = vi.mocked(useArchivedAgentChats);

function chat(overrides: Partial<AgentChat>): AgentChat {
  return {
    id: "s-1",
    canvasId: "c-1",
    provider: "openai",
    status: "idle",
    title: "Some topic",
    archivedAt: "2026-02-28T12:00:00",
    createdAt: "2026-02-28T12:00:00",
    updatedAt: null,
    ...overrides,
  };
}

function mockPage(chats: AgentChat[], total: number) {
  mockUseArchived.mockReturnValue({
    data: { chats, total, page: 1, pageSize: 10 },
    isLoading: false,
  } as unknown as ReturnType<typeof useArchivedAgentChats>);
}

beforeEach(() => mockUseArchived.mockReset());

describe("ArchivedSessionsDrawer", () => {
  it("lists archived sessions (date + title) and selects one", () => {
    mockPage([chat({ id: "s-1", title: "Canvas init" })], 1);
    const onSelect = vi.fn();
    render(<ArchivedSessionsDrawer organizationId="o" canvasId="c" onSelect={onSelect} onClose={vi.fn()} />);

    const item = screen.getByTestId("archived-session-item");
    expect(item).toHaveTextContent("Canvas init");
    expect(item).toHaveTextContent("28/02/26");

    fireEvent.click(item);
    expect(onSelect).toHaveBeenCalledWith(expect.objectContaining({ id: "s-1" }));
  });

  it("shows the empty state", () => {
    mockPage([], 0);
    render(<ArchivedSessionsDrawer organizationId="o" canvasId="c" onSelect={vi.fn()} onClose={vi.fn()} />);
    expect(screen.getByTestId("archived-empty")).toBeInTheDocument();
  });

  it("paginates and closes", () => {
    mockPage([chat({})], 25);
    const onClose = vi.fn();
    render(<ArchivedSessionsDrawer organizationId="o" canvasId="c" onSelect={vi.fn()} onClose={onClose} />);

    expect(screen.getByTestId("archived-page-indicator")).toHaveTextContent("1 / 3");
    expect(screen.getByTestId("archived-prev")).toBeDisabled();
    expect(screen.getByTestId("archived-next")).not.toBeDisabled();

    fireEvent.click(screen.getByTestId("archived-drawer-close"));
    expect(onClose).toHaveBeenCalledTimes(1);
  });
});
