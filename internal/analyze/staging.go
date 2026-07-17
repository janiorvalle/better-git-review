package analyze

import (
	"fmt"

	"github.com/janiorvalle/better-git-review/internal/document"
)

type StageDecision struct {
	Staged     bool
	InputBytes int
	Budget     int
}

func DecideStaging(files []document.File, getenv func(string) string, defaults ...int) (StageDecision, error) {
	budget, overridden, err := stageBudget(getenv, defaults...)
	if err != nil {
		return StageDecision{}, err
	}
	inputBytes := AnalysisInputBytes(files)
	staged := inputBytes > budget || len(files) > CohortMaxFiles
	if staged {
		minimum := minimumStagedBudget(files)
		if budget < minimum {
			if !overridden {
				return StageDecision{}, fmt.Errorf(
					"analysis budget %d is too small for staged prompt framing and file headers (minimum %d)",
					budget, minimum,
				)
			}
			// BGR_STAGE_BUDGET is a test/development staging trigger. Keep its
			// tiny values useful without creating impossible provider prompts.
			budget = minimum
		}
	}
	return StageDecision{
		Staged:     staged,
		InputBytes: inputBytes,
		Budget:     budget,
	}, nil
}

func minimumStagedBudget(files []document.File) int {
	maxHeader := 0
	for index, file := range files {
		maxHeader = max(maxHeader, len(fileHeader(index, file)))
	}
	minimum := summaryBatchPromptOverheadChars() + maxHeader
	delimiters := Delimiters{
		Begin: "BEGIN_UNTRUSTED_0000000000000000",
		End:   "END_UNTRUSTED_0000000000000000",
	}
	minimum = max(minimum, synthesisPromptOverheadChars(delimiters))
	for _, cohort := range PlanCohorts(files) {
		minimum = max(minimum, len(BuildCohortNarrationPrompt(cohort, "", delimiters)))
	}
	return minimum
}
