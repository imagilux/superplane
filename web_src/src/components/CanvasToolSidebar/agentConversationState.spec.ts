import { renderHook } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { createInitialOutcomeState, parseRubricCriteria, useThinkingIndicator } from "./agentConversationState";
import type { AgentMessage } from "./types";

function message(overrides: Partial<AgentMessage>): AgentMessage {
  return {
    id: "message-1",
    role: "assistant",
    content: "",
    toolName: "",
    toolCallId: "",
    toolStatus: "",
    createdAt: null,
    ...overrides,
  };
}

function thinking(messages: AgentMessage[], status = "streaming") {
  return renderHook(() => useThinkingIndicator(messages, status)).result.current;
}

describe("useThinkingIndicator", () => {
  it("shows while the agent is streaming without an active tool", () => {
    expect(thinking([message({ id: "user-1", role: "user", content: "Run this" })])).toBe(true);
  });

  it("keeps showing after a tool finishes while the agent is still streaming", () => {
    expect(
      thinking([
        message({ id: "user-1", role: "user", content: "Run this" }),
        message({ id: "tool-start-1", role: "tool", toolCallId: "call-1", toolStatus: "started" }),
        message({ id: "tool-1", role: "tool", toolCallId: "call-1", toolStatus: "finished" }),
      ]),
    ).toBe(true);
  });

  it("hides while a tool is active", () => {
    expect(
      thinking([
        message({ id: "user-1", role: "user", content: "Run this" }),
        message({ id: "tool-1", role: "tool", toolCallId: "call-1", toolStatus: "started" }),
      ]),
    ).toBe(false);
  });

  it("hides when the session is not streaming", () => {
    expect(thinking([message({ id: "user-1", role: "user", content: "Run this" })], "idle")).toBe(false);
  });
});

describe("parseRubricCriteria", () => {
  it("strips list markers, numbering, and heading hashes", () => {
    const rubric = [
      "- Runs daily",
      "* Writes to orders",
      "+ Logs failures",
      "1. Idempotent",
      "2) Retries",
      "# Goal",
    ].join("\n");

    expect(parseRubricCriteria(rubric)).toEqual([
      "Runs daily",
      "Writes to orders",
      "Logs failures",
      "Idempotent",
      "Retries",
      "Goal",
    ]);
  });

  it("drops blank lines and trims surrounding whitespace", () => {
    expect(parseRubricCriteria("  - First  \n\n   \n- Second\n")).toEqual(["First", "Second"]);
  });

  it("passes plain lines through unchanged", () => {
    expect(parseRubricCriteria("Just a sentence with no marker")).toEqual(["Just a sentence with no marker"]);
  });
});

describe("createInitialOutcomeState", () => {
  it("defaults max iterations to 3 when omitted", () => {
    const state = createInitialOutcomeState({ title: "Goal", criteria: ["A", "B"] });

    expect(state.maxIterations).toBe(3);
    expect(state.iteration).toBe(1);
    expect(state.phase).toBe("building");
    expect(state.criteria).toEqual([{ text: "A" }, { text: "B" }]);
    expect(state.log).toEqual([{ phase: "building" }]);
  });

  it("honors an explicit max iterations value", () => {
    expect(createInitialOutcomeState({ title: "Goal", criteria: ["A"], maxIterations: 7 }).maxIterations).toBe(7);
  });
});
