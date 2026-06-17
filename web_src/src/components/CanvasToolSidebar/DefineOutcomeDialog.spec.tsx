import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import type { useDefineAgentOutcome } from "@/hooks/useAgentChats";
import { DefineOutcomeDialog } from "./DefineOutcomeDialog";

type OutcomeMutation = ReturnType<typeof useDefineAgentOutcome>;

function makeMutation(overrides: Partial<{ isPending: boolean; mutateAsync: ReturnType<typeof vi.fn> }> = {}) {
  return {
    isPending: false,
    mutateAsync: vi.fn().mockResolvedValue(undefined),
    ...overrides,
  } as unknown as OutcomeMutation;
}

function renderDialog(overrides: Partial<Parameters<typeof DefineOutcomeDialog>[0]> = {}) {
  const outcomeMutation = overrides.outcomeMutation ?? makeMutation();
  const setOutcomeState = overrides.setOutcomeState ?? vi.fn();
  render(
    <DefineOutcomeDialog
      chatId="chat-1"
      outcomeMutation={outcomeMutation}
      setOutcomeState={setOutcomeState}
      {...overrides}
    />,
  );
  return { outcomeMutation, setOutcomeState };
}

describe("DefineOutcomeDialog", () => {
  it("opens the dialog from the trigger", () => {
    renderDialog();
    expect(screen.queryByTestId("define-outcome-dialog")).not.toBeInTheDocument();

    fireEvent.click(screen.getByTestId("define-outcome-trigger"));

    expect(screen.getByTestId("define-outcome-dialog")).toBeInTheDocument();
    expect(screen.getByTestId("define-outcome-goal")).toBeInTheDocument();
    expect(screen.getByTestId("define-outcome-rubric")).toBeInTheDocument();
  });

  it("disables the trigger when disabled", () => {
    renderDialog({ disabled: true });
    expect(screen.getByTestId("define-outcome-trigger")).toBeDisabled();
  });

  it("requires both a goal and a rubric before submitting", async () => {
    const { outcomeMutation } = renderDialog();
    fireEvent.click(screen.getByTestId("define-outcome-trigger"));

    fireEvent.click(screen.getByTestId("define-outcome-submit"));

    expect(await screen.findByTestId("define-outcome-error")).toHaveTextContent(/required/i);
    expect(outcomeMutation.mutateAsync).not.toHaveBeenCalled();
  });

  it("submits the outcome and seeds the progress widget, clamping max iterations", async () => {
    const { outcomeMutation, setOutcomeState } = renderDialog();
    fireEvent.click(screen.getByTestId("define-outcome-trigger"));

    fireEvent.change(screen.getByTestId("define-outcome-goal"), { target: { value: "Ship the ETL" } });
    fireEvent.change(screen.getByTestId("define-outcome-rubric"), { target: { value: "- Runs daily\n- Idempotent" } });
    fireEvent.change(screen.getByTestId("define-outcome-max-iterations"), { target: { value: "99" } });

    fireEvent.click(screen.getByTestId("define-outcome-submit"));

    await waitFor(() => {
      expect(outcomeMutation.mutateAsync).toHaveBeenCalledWith({
        chatId: "chat-1",
        description: "Ship the ETL",
        rubric: "- Runs daily\n- Idempotent",
        maxIterations: 10,
      });
    });

    expect(setOutcomeState).toHaveBeenCalledWith(
      expect.objectContaining({
        title: "Ship the ETL",
        criteria: [{ text: "Runs daily" }, { text: "Idempotent" }],
        maxIterations: 10,
        phase: "building",
      }),
    );
  });

  it("surfaces a submit failure without seeding state", async () => {
    const outcomeMutation = makeMutation({ mutateAsync: vi.fn().mockRejectedValue(new Error("boom")) });
    const { setOutcomeState } = renderDialog({ outcomeMutation });
    fireEvent.click(screen.getByTestId("define-outcome-trigger"));

    fireEvent.change(screen.getByTestId("define-outcome-goal"), { target: { value: "Goal" } });
    fireEvent.change(screen.getByTestId("define-outcome-rubric"), { target: { value: "Criterion" } });
    fireEvent.click(screen.getByTestId("define-outcome-submit"));

    expect(await screen.findByTestId("define-outcome-error")).toHaveTextContent("boom");
    expect(setOutcomeState).not.toHaveBeenCalled();
  });
});
