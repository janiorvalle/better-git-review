package app

import (
	"flag"
	"fmt"
	"os"

	"github.com/janiorvalle/better-git-review/internal/agentskill"
)

// runInstallSkill handles `bgr install-skill`: a non-interactive way to put the
// bundled agent skill into Claude Code and Codex, so setup scripts and agents
// never have to go through `bgr configure`.
func runInstallSkill(args []string, env Environment) error {
	flags := flag.NewFlagSet("install-skill", flag.ContinueOnError)
	flags.SetOutput(env.Stderr)
	flags.Usage = func() {
		fmt.Fprint(env.Stderr, installSkillUsage)
	}
	claudeOnly := flags.Bool("claude", false, "install for Claude Code only")
	codexOnly := flags.Bool("codex", false, "install for Codex only")
	home := flags.String("home", "", "home directory to install under (default: the current user's)")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *claudeOnly && *codexOnly {
		return fmt.Errorf("install-skill: pass --claude or --codex, not both (omit both to install for each)")
	}
	targets := []agentskill.Target{agentskill.Claude, agentskill.Codex}
	if *claudeOnly {
		targets = []agentskill.Target{agentskill.Claude}
	} else if *codexOnly {
		targets = []agentskill.Target{agentskill.Codex}
	}
	if *home == "" {
		resolved, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("install-skill: %w (pass --home)", err)
		}
		*home = resolved
	}
	for _, target := range targets {
		path, err := agentskill.Install(*home, target)
		if err != nil {
			return fmt.Errorf("install-skill: %w", err)
		}
		fmt.Fprintf(env.Stdout, "Installed %s\n", path)
	}
	return nil
}

const installSkillUsage = `bgr install-skill - install the bundled agent skill without prompts

USAGE
  bgr install-skill            install for Claude Code and Codex
  bgr install-skill --claude   Claude Code only
  bgr install-skill --codex    Codex only
  bgr install-skill --home DIR install under DIR instead of your home

Reinstalling is safe; the file is replaced atomically. Uninstall by deleting
the bgr folder inside the harness's skills directory.
`
