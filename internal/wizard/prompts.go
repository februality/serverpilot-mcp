package wizard

import (
	"github.com/charmbracelet/huh"
)

// Prompter is the test seam for the wizard. Production uses huhPrompter,
// tests can substitute a scripted implementation.
type Prompter interface {
	Text(title, placeholder string, validate func(string) error) (string, error)
	Password(title string, validate func(string) error) (string, error)
	Confirm(title string, defaultYes bool) (bool, error)
	MultiSelect(title string, options []SelectOption, defaults []string) ([]string, error)
}

type SelectOption struct {
	Label    string
	Value    string
	Disabled bool
}

type huhPrompter struct{}

func NewPrompter() Prompter { return huhPrompter{} }

func (huhPrompter) Text(title, placeholder string, validate func(string) error) (string, error) {
	var v string
	in := huh.NewInput().Title(title).Placeholder(placeholder).Value(&v)
	if validate != nil {
		in = in.Validate(validate)
	}
	if err := in.Run(); err != nil {
		return "", err
	}
	return v, nil
}

func (huhPrompter) Password(title string, validate func(string) error) (string, error) {
	var v string
	in := huh.NewInput().Title(title).EchoMode(huh.EchoModePassword).Value(&v)
	if validate != nil {
		in = in.Validate(validate)
	}
	if err := in.Run(); err != nil {
		return "", err
	}
	return v, nil
}

func (huhPrompter) Confirm(title string, defaultYes bool) (bool, error) {
	v := defaultYes
	if err := huh.NewConfirm().Title(title).Value(&v).Run(); err != nil {
		return false, err
	}
	return v, nil
}

func (huhPrompter) MultiSelect(title string, options []SelectOption, defaults []string) ([]string, error) {
	defaultSet := map[string]bool{}
	for _, d := range defaults {
		defaultSet[d] = true
	}
	huhOpts := make([]huh.Option[string], 0, len(options))
	selected := make([]string, 0, len(options))
	for _, o := range options {
		opt := huh.NewOption(o.Label, o.Value)
		if defaultSet[o.Value] {
			opt = opt.Selected(true)
			selected = append(selected, o.Value)
		}
		huhOpts = append(huhOpts, opt)
	}
	if err := huh.NewMultiSelect[string]().
		Title(title).
		Options(huhOpts...).
		Value(&selected).
		Run(); err != nil {
		return nil, err
	}
	return selected, nil
}
