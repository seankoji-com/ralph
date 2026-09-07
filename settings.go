package main

import (
	"fmt"
	"strconv"
	"strings"

	"charm.land/huh/v2"
)

type RunOptions struct {
	Max      int    `json:"max"`
	Cooldown int    `json:"cooldown_seconds"`
	Timeout  int    `json:"timeout_minutes"`
	Model    string `json:"model"`
}

func (o RunOptions) validate() error {
	if o.Max < 1 || o.Max > 100 {
		return fmt.Errorf("choose 1 to 100 iterations")
	}
	if o.Cooldown < 0 || o.Cooldown > 3600 {
		return fmt.Errorf("rest must be 0 to 3600 seconds")
	}
	if o.Timeout < 1 || o.Timeout > 240 {
		return fmt.Errorf("iteration timeout must be 1 to 240 minutes")
	}
	if strings.TrimSpace(o.Model) == "" {
		return fmt.Errorf("choose a runner model")
	}
	return nil
}

func intRange(low, high int) func(string) error {
	return func(s string) error {
		n, err := strconv.Atoi(s)
		if err != nil || n < low || n > high {
			return fmt.Errorf("enter a whole number from %d to %d", low, high)
		}
		return nil
	}
}

type loopSettings struct {
	*huh.Form
	model, iterations, cooldown, timeout string
}

func settingsForm(o RunOptions, width int) *loopSettings {
	s := &loopSettings{model: o.Model, iterations: strconv.Itoa(o.Max), cooldown: strconv.Itoa(o.Cooldown), timeout: strconv.Itoa(o.Timeout)}
	s.Form = huh.NewForm(
		huh.NewGroup(
			huh.NewInput().Key("model").Title("Runner model").Description("The coding agent; the prompt partner stays on DeepSeek Flash.").Value(&s.model).Validate(huh.ValidateNotEmpty()),
			huh.NewInput().Key("max").Title("How many loops?").Description("Ralph stops early when the agent reports completion.").Value(&s.iterations).Validate(intRange(1, 100)),
		),
		huh.NewGroup(
			huh.NewInput().Key("cooldown").Title("A breather between loops").Description("Seconds. Zero is fine if you're in a hurry.").Value(&s.cooldown).Validate(intRange(0, 3600)),
			huh.NewInput().Key("timeout").Title("Time limit per iteration").Description("Minutes. A timed-out agent stops the run.").Value(&s.timeout).Validate(intRange(1, 240)),
		),
	).WithTheme(huh.ThemeFunc(huh.ThemeCharm)).WithWidth(width).WithShowHelp(true)
	return s
}

func optionsFromForm(f *loopSettings) RunOptions {
	n, _ := strconv.Atoi(f.iterations)
	rest, _ := strconv.Atoi(f.cooldown)
	timeout, _ := strconv.Atoi(f.timeout)
	return RunOptions{Max: n, Cooldown: rest, Timeout: timeout, Model: strings.TrimSpace(f.model)}
}
