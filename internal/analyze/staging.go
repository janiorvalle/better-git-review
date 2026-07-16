package analyze

import (
	"github.com/janiorvalle/better-git-review/internal/document"
)

type StageDecision struct {
	Staged     bool
	InputBytes int
	Budget     int
}

func DecideStaging(files []document.File, getenv func(string) string) (StageDecision, error) {
	budget, err := StageBudget(getenv)
	if err != nil {
		return StageDecision{}, err
	}
	inputBytes := AnalysisInputBytes(files)
	return StageDecision{
		Staged:     inputBytes > budget,
		InputBytes: inputBytes,
		Budget:     budget,
	}, nil
}
