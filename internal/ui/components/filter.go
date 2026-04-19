package components

import (
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// Filter provides fuzzy and exact-match filtering over table rows.
type Filter struct {
	input  textinput.Model
	active bool
}

func NewFilter() Filter {
	input := textinput.New()
	input.Prompt = "/"
	input.CharLimit = 256
	input.Placeholder = "filter"
	input.SetValue("")
	return Filter{input: input}
}

func (f *Filter) Activate() {
	f.active = true
	f.input.Focus()
}

func (f *Filter) SetPrompt(prompt string) {
	if prompt == "" {
		panic("components.Filter.SetPrompt: empty prompt")
	}
	f.input.Prompt = prompt
}

func (f *Filter) SetPlaceholder(placeholder string) {
	f.input.Placeholder = placeholder
}

func (f *Filter) SetValue(value string) {
	f.input.SetValue(value)
}

func (f *Filter) Deactivate() {
	f.active = false
	f.input.Blur()
}

func (f *Filter) Clear() {
	f.input.SetValue("")
}

func (f *Filter) Active() bool {
	return f.active
}

func (f *Filter) Value() string {
	return f.input.Value()
}

func (f *Filter) Update(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	f.input, cmd = f.input.Update(msg)
	return cmd
}

func (f *Filter) View() string {
	return f.input.View()
}
