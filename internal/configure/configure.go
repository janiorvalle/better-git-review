package configure

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/janiorvalle/better-git-review/internal/agentskill"
	"github.com/janiorvalle/better-git-review/internal/config"
	"github.com/janiorvalle/better-git-review/internal/provider"
)

var ErrCancelled = errors.New("configuration cancelled")

type Options struct {
	Current    config.Config
	ConfigPath string
	Registry   provider.Registry
	Input      io.Reader
	Output     io.Writer
	Home       string
	FirstRun   bool
}

type Result struct {
	Config    config.Config
	ReviewNow bool
}

func Run(ctx context.Context, opts Options) (Result, error) {
	if opts.Input == nil || opts.Output == nil {
		return Result{}, fmt.Errorf("configure requires input and output")
	}
	reader, ok := opts.Input.(*bufio.Reader)
	if !ok {
		reader = bufio.NewReader(opts.Input)
	}
	if opts.Home == "" {
		opts.Home = homeDir()
	}
	if opts.FirstRun {
		fmt.Fprintln(opts.Output, "Welcome to bgr. Let's set up your review provider.")
	} else {
		fmt.Fprintln(opts.Output, "bgr configure")
	}

	probes := opts.Registry.ProbeAll()
	for _, probe := range probes {
		mark := "\u2717"
		if probe.Available {
			mark = "\u2713"
		}
		fmt.Fprintf(opts.Output, "  %s %s - %s\n", mark, probe.Name, probe.Detail)
	}
	providerName, err := chooseProvider(reader, opts.Output, probes, opts.Current.Provider)
	if err != nil {
		return Result{}, err
	}
	providerCfg := opts.Current.Providers[providerName]
	selection, err := opts.Registry.Create(providerName, providerCfg)
	if err != nil {
		return Result{}, err
	}
	model := selection.Model
	reasoning := selection.Reasoning
	if catalog, ok := selection.Provider.(provider.Cataloger); ok {
		models, modelErr := catalog.Models(ctx)
		if modelErr != nil {
			return Result{}, fmt.Errorf("load %s model catalog: %w", providerName, modelErr)
		}
		model, err = chooseModel(reader, opts.Output, models, firstNonEmpty(providerCfg.Model, selection.Model))
		if err != nil {
			return Result{}, err
		}
		if selector, ok := selection.Provider.(provider.CatalogModelSelector); ok {
			selector.SetCatalogModel(model)
		}
		reasoning, err = chooseReasoning(reader, opts.Output, catalog.ReasoningLevels(), providerCfg.Reasoning, selection.Reasoning)
		if err != nil {
			return Result{}, err
		}
	} else {
		model, err = prompt(reader, opts.Output, "Model", firstNonEmpty(providerCfg.Model, selection.Model))
		if err != nil {
			return Result{}, err
		}
	}
	autoOpenDefault := true
	if opts.Current.AutoOpen != nil {
		autoOpenDefault = *opts.Current.AutoOpen
	}
	autoOpen, err := promptYesNo(reader, opts.Output, "Open HTML walkthroughs automatically?", autoOpenDefault)
	if err != nil {
		return Result{}, err
	}

	if err := installSkill(reader, opts.Output, opts.Home); err != nil {
		return Result{}, err
	}
	if opts.Current.Providers == nil {
		opts.Current.Providers = map[string]config.ProviderConfig{}
	}
	providerCfg.Model = model
	providerCfg.Reasoning = reasoning
	if providerName == "openrouter" && providerCfg.APIKeyEnv == "" {
		providerCfg.APIKeyEnv = "OPENROUTER_API_KEY"
	}
	opts.Current.Provider = providerName
	opts.Current.AutoOpen = &autoOpen
	opts.Current.Providers[providerName] = providerCfg
	if err := config.WriteUser(opts.ConfigPath, opts.Current); err != nil {
		return Result{}, fmt.Errorf("write user config: %w", err)
	}
	fmt.Fprintf(opts.Output, "\nSaved %s\n", opts.ConfigPath)
	if providerName == "openrouter" {
		fmt.Fprintf(opts.Output, "Set %s in your environment; the key is never stored in config.\n", providerCfg.APIKeyEnv)
	}
	fmt.Fprintln(opts.Output, "Quick reference: bgr --dirty | bgr --commit <sha> | bgr <PR_NUMBER> | bgr -i")
	if !opts.FirstRun {
		return Result{Config: opts.Current}, nil
	}
	reviewNow, err := promptYesNo(reader, opts.Output, "Review the current branch now?", true)
	if err != nil {
		return Result{}, err
	}
	return Result{Config: opts.Current, ReviewNow: reviewNow}, nil
}

func chooseProvider(reader *bufio.Reader, output io.Writer, probes []provider.Probe, current string) (string, error) {
	if current == "" {
		for _, probe := range probes {
			if probe.Available {
				current = probe.Name
				break
			}
		}
	}
	if current == "" && len(probes) > 0 {
		current = probes[0].Name
	}
	fmt.Fprintln(output, "\nProvider")
	defaultIndex := 1
	for index, probe := range probes {
		status := "unavailable"
		if probe.Available {
			status = "available"
		}
		fmt.Fprintf(output, "  %d  %s (%s)\n", index+1, probe.Name, status)
		if probe.Name == current {
			defaultIndex = index + 1
		}
	}
	answer, err := prompt(reader, output, "Provider number", fmt.Sprintf("%d", defaultIndex))
	if err != nil {
		return "", err
	}
	index, parseErr := parseChoice(answer, len(probes))
	if parseErr != nil {
		for _, probe := range probes {
			if probe.Name == answer {
				return answer, nil
			}
		}
		return "", fmt.Errorf("unknown provider %q", answer)
	}
	return probes[index].Name, nil
}

func chooseModel(reader *bufio.Reader, output io.Writer, models []provider.ModelOption, current string) (string, error) {
	if current == "" {
		for _, model := range models {
			if model.Default {
				current = model.ID
				break
			}
		}
	}
	query := ""
	for {
		filtered := filterModels(models, query)
		visible := filtered
		if len(visible) > 8 {
			visible = visible[:8]
		}
		fmt.Fprintln(output, "\nModel")
		for index, model := range visible {
			note := ""
			if model.Note != "" {
				note = " - " + model.Note
			}
			fmt.Fprintf(output, "  %d  %s (%s)%s\n", index+1, model.Label, model.ID, note)
		}
		otherIndex := len(visible) + 1
		fmt.Fprintf(output, "  %d  other... (type any model id)\n", otherIndex)
		if hidden := len(filtered) - len(visible); hidden > 0 {
			fmt.Fprintf(output, "     ... %d more - type to filter\n", hidden)
		}
		answer, err := prompt(reader, output, "Model number or filter", defaultModelChoice(visible, current))
		if err != nil {
			return "", err
		}
		if index, parseErr := parseChoice(answer, otherIndex); parseErr == nil {
			if index < len(visible) {
				return visible[index].ID, nil
			}
			return prompt(reader, output, "Model id", current)
		}
		query = answer
	}
}

func chooseReasoning(reader *bufio.Reader, output io.Writer, levels []string, current, fallback string) (string, error) {
	if len(levels) == 0 {
		return "", nil
	}
	current = firstNonEmpty(current, fallback)
	fmt.Fprintln(output, "\nReasoning")
	fmt.Fprintln(output, "  1  provider default")
	defaultChoice := "1"
	for index, level := range levels {
		fmt.Fprintf(output, "  %d  %s\n", index+2, level)
		if level == current {
			defaultChoice = fmt.Sprintf("%d", index+2)
		}
	}
	answer, err := prompt(reader, output, "Reasoning number", defaultChoice)
	if err != nil {
		return "", err
	}
	index, err := parseChoice(answer, len(levels)+1)
	if err != nil {
		return "", err
	}
	if index == 0 {
		return "", nil
	}
	return levels[index-1], nil
}

func installSkill(reader *bufio.Reader, output io.Writer, home string) error {
	fmt.Fprintln(output, "\nInstall the embedded bgr agent skill?")
	fmt.Fprintln(output, "  1  Skip (default)")
	fmt.Fprintln(output, "  2  Claude Code")
	fmt.Fprintln(output, "  3  Codex")
	fmt.Fprintln(output, "  4  Both")
	answer, err := prompt(reader, output, "Skill install", "1")
	if err != nil {
		return err
	}
	index, err := parseChoice(answer, 4)
	if err != nil {
		return err
	}
	var targets []agentskill.Target
	switch index {
	case 1:
		targets = []agentskill.Target{agentskill.Claude}
	case 2:
		targets = []agentskill.Target{agentskill.Codex}
	case 3:
		targets = []agentskill.Target{agentskill.Claude, agentskill.Codex}
	}
	for _, target := range targets {
		path, installErr := agentskill.Install(home, target)
		if installErr != nil {
			return installErr
		}
		fmt.Fprintf(output, "Installed %s (uninstall by deleting its bgr skill folder).\n", path)
	}
	return nil
}

func prompt(reader *bufio.Reader, output io.Writer, label, defaultValue string) (string, error) {
	fmt.Fprintf(output, "%s [%s]: ", label, defaultValue)
	answer, err := reader.ReadString('\n')
	if errors.Is(err, io.EOF) && answer == "" {
		return "", ErrCancelled
	}
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	answer = strings.TrimSpace(answer)
	if answer == "" {
		return defaultValue, nil
	}
	return answer, nil
}

func promptYesNo(reader *bufio.Reader, output io.Writer, label string, defaultValue bool) (bool, error) {
	suffix := "y/N"
	if defaultValue {
		suffix = "Y/n"
	}
	fmt.Fprintf(output, "%s [%s]: ", label, suffix)
	answer, err := reader.ReadString('\n')
	if errors.Is(err, io.EOF) && answer == "" {
		return false, ErrCancelled
	}
	if err != nil && !errors.Is(err, io.EOF) {
		return false, err
	}
	answer = strings.ToLower(strings.TrimSpace(answer))
	if answer == "" {
		return defaultValue, nil
	}
	if answer == "y" || answer == "yes" {
		return true, nil
	}
	if answer == "n" || answer == "no" {
		return false, nil
	}
	return false, fmt.Errorf("expected yes or no, got %q", answer)
}

func filterModels(models []provider.ModelOption, query string) []provider.ModelOption {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return append([]provider.ModelOption(nil), models...)
	}
	var result []provider.ModelOption
	for _, model := range models {
		if strings.Contains(strings.ToLower(model.ID+" "+model.Label+" "+model.Note), query) {
			result = append(result, model)
		}
	}
	return result
}

func defaultModelChoice(models []provider.ModelOption, current string) string {
	for index, model := range models {
		if model.ID == current {
			return fmt.Sprintf("%d", index+1)
		}
	}
	return fmt.Sprintf("%d", len(models)+1)
}

func parseChoice(value string, count int) (int, error) {
	var number int
	if _, err := fmt.Sscan(value, &number); err != nil || number < 1 || number > count {
		return 0, fmt.Errorf("choose a number from 1 to %d", count)
	}
	return number - 1, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func homeDir() string {
	if value := os.Getenv("HOME"); value != "" {
		return value
	}
	value, _ := os.UserHomeDir()
	return value
}
