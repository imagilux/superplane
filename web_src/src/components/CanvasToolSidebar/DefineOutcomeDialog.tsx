import { Target } from "lucide-react";
import { useState } from "react";
import type { OutcomeState } from "@/components/AgentSidebar/widgets/OutcomeProgressWidget";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import type { useDefineAgentOutcome } from "@/hooks/useAgentChats";
import { createInitialOutcomeState, parseRubricCriteria } from "./agentConversationState";

type SetOutcomeState = (update: OutcomeState | null | ((prev: OutcomeState | null) => OutcomeState | null)) => void;

const DEFAULT_MAX_ITERATIONS = 3;
const MIN_ITERATIONS = 1;
const MAX_ITERATIONS = 10;

// DefineOutcomeDialog is the entry point to the autonomous build→grade→iterate
// loop: a trigger button that opens a small form (goal, free-form rubric, and
// an iteration cap), fires useDefineAgentOutcome, and optimistically seeds the
// OutcomeProgressWidget so the loop is visible the moment it starts. The raw
// rubric text is sent verbatim to the grader; parseRubricCriteria only shapes
// the local widget display.
export function DefineOutcomeDialog({
  chatId,
  outcomeMutation,
  setOutcomeState,
  disabled,
}: {
  chatId: string;
  outcomeMutation: ReturnType<typeof useDefineAgentOutcome>;
  setOutcomeState: SetOutcomeState;
  disabled?: boolean;
}) {
  const [open, setOpen] = useState(false);
  const [description, setDescription] = useState("");
  const [rubric, setRubric] = useState("");
  const [maxIterations, setMaxIterations] = useState(DEFAULT_MAX_ITERATIONS);
  const [error, setError] = useState<string | null>(null);

  const reset = () => {
    setDescription("");
    setRubric("");
    setMaxIterations(DEFAULT_MAX_ITERATIONS);
    setError(null);
  };

  const handleOpenChange = (next: boolean) => {
    // Don't let the dialog close mid-request — the optimistic state hasn't
    // been seeded yet, so a stray close would lose the in-flight loop.
    if (!next && outcomeMutation.isPending) return;
    setOpen(next);
    if (!next) reset();
  };

  const handleSubmit = async () => {
    const goal = description.trim();
    const rubricText = rubric.trim();
    if (!goal || !rubricText) {
      setError("Goal and rubric are both required.");
      return;
    }
    const iterations = Number.isFinite(maxIterations)
      ? Math.min(Math.max(Math.round(maxIterations), MIN_ITERATIONS), MAX_ITERATIONS)
      : DEFAULT_MAX_ITERATIONS;
    setError(null);

    try {
      await outcomeMutation.mutateAsync({ chatId, description: goal, rubric: rubricText, maxIterations: iterations });
      setOutcomeState(
        createInitialOutcomeState({
          title: goal,
          criteria: parseRubricCriteria(rubricText),
          maxIterations: iterations,
        }),
      );
      setOpen(false);
      reset();
    } catch (submitError) {
      setError(submitError instanceof Error ? submitError.message : "Failed to start the outcome loop.");
    }
  };

  return (
    <div className="border-t border-slate-200 px-3 py-2">
      <div className="mx-auto w-full max-w-[800px]">
        <Button
          type="button"
          variant="outline"
          size="sm"
          className="w-full"
          disabled={disabled}
          onClick={() => setOpen(true)}
          data-testid="define-outcome-trigger"
        >
          <Target className="size-4" /> Define outcome
        </Button>
      </div>

      <Dialog open={open} onOpenChange={handleOpenChange}>
        <DialogContent data-testid="define-outcome-dialog">
          <DialogHeader>
            <DialogTitle>Define an outcome</DialogTitle>
            <DialogDescription>
              The agent builds, grades its own work against your rubric, and iterates until the rubric is satisfied or
              the iteration cap is reached.
            </DialogDescription>
          </DialogHeader>

          <OutcomeFormFields
            description={description}
            rubric={rubric}
            maxIterations={maxIterations}
            error={error}
            onDescriptionChange={setDescription}
            onRubricChange={setRubric}
            onMaxIterationsChange={setMaxIterations}
          />

          <DialogFooter>
            <Button
              type="button"
              variant="outline"
              onClick={() => handleOpenChange(false)}
              disabled={outcomeMutation.isPending}
            >
              Cancel
            </Button>
            <Button
              type="button"
              onClick={handleSubmit}
              disabled={outcomeMutation.isPending}
              data-testid="define-outcome-submit"
            >
              {outcomeMutation.isPending ? "Starting…" : "Start"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}

function OutcomeFormFields({
  description,
  rubric,
  maxIterations,
  error,
  onDescriptionChange,
  onRubricChange,
  onMaxIterationsChange,
}: {
  description: string;
  rubric: string;
  maxIterations: number;
  error: string | null;
  onDescriptionChange: (value: string) => void;
  onRubricChange: (value: string) => void;
  onMaxIterationsChange: (value: number) => void;
}) {
  return (
    <div className="space-y-4 py-2">
      <div className="space-y-1.5">
        <Label htmlFor="define-outcome-goal">Goal</Label>
        <Input
          id="define-outcome-goal"
          value={description}
          onChange={(event) => onDescriptionChange(event.target.value)}
          placeholder="e.g. Add a daily ETL pipeline that loads orders into the warehouse"
          data-testid="define-outcome-goal"
        />
      </div>

      <div className="space-y-1.5">
        <Label htmlFor="define-outcome-rubric">Rubric</Label>
        <Textarea
          id="define-outcome-rubric"
          value={rubric}
          onChange={(event) => onRubricChange(event.target.value)}
          placeholder={
            "One criterion per line, e.g.\n- Runs on a daily schedule\n- Writes to the orders table\n- Fails loudly on bad input"
          }
          rows={6}
          data-testid="define-outcome-rubric"
        />
        <p className="text-xs text-slate-500">
          Free-form. Each line becomes a criterion the agent grades itself against.
        </p>
      </div>

      <div className="space-y-1.5">
        <Label htmlFor="define-outcome-max-iterations">Max iterations</Label>
        <Input
          id="define-outcome-max-iterations"
          type="number"
          min={MIN_ITERATIONS}
          max={MAX_ITERATIONS}
          value={maxIterations}
          onChange={(event) => onMaxIterationsChange(event.target.valueAsNumber)}
          className="w-24"
          data-testid="define-outcome-max-iterations"
        />
      </div>

      {error ? (
        <p className="text-sm text-red-600" data-testid="define-outcome-error">
          {error}
        </p>
      ) : null}
    </div>
  );
}
