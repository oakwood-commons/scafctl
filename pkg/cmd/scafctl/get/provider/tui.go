// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"runtime"
	"strings"

	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/terminal/kvx"
	"gopkg.in/yaml.v3"
)

// Styles for the TUI
var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("99")).
			MarginLeft(2)

	statusBarStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("241"))

	helpStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("241")).
			MarginTop(1)

	detailKeyStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("99"))

	detailValueStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("252"))

	capabilityStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("85")).
			Background(lipgloss.Color("235")).
			Padding(0, 1)

	betaStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("214")).
			Bold(true)

	deprecatedStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("196")).
			Bold(true).
			Strikethrough(true)
)

// providerItem implements list.Item for providers
type providerItem struct {
	provider provider.Provider
}

func (i providerItem) Title() string {
	desc := i.provider.Descriptor()
	name := desc.Name
	if desc.Beta {
		name += " " + betaStyle.Render("[BETA]")
	}
	if desc.Deprecated { //nolint:staticcheck // Intentionally showing deprecated status
		name = deprecatedStyle.Render(name) + " [DEPRECATED]"
	}
	return name
}

func (i providerItem) Description() string {
	desc := i.provider.Descriptor()
	return desc.Description
}

func (i providerItem) FilterValue() string {
	desc := i.provider.Descriptor()
	return desc.Name + " " + desc.Description + " " + strings.Join(capabilitiesToStrings(desc.Capabilities), " ")
}

// view represents the current view state
type view int

const (
	viewList view = iota
	viewDetail
)

// providerModel is the main TUI model
type providerModel struct {
	list        list.Model
	viewport    viewport.Model
	providers   []provider.Provider
	selected    *provider.Descriptor
	currentView view
	width       int
	height      int
	copied      bool
	quitting    bool
}

// newProviderModel creates a new TUI model
func newProviderModel(providers []provider.Provider) providerModel {
	items := make([]list.Item, len(providers))
	for i, p := range providers {
		items[i] = providerItem{provider: p}
	}

	delegate := list.NewDefaultDelegate()
	delegate.SetHeight(2)

	l := list.New(items, delegate, 0, 0)
	l.Title = "Providers"
	l.SetShowStatusBar(true)
	l.SetFilteringEnabled(true)
	l.SetShowHelp(false)

	vp := viewport.New()

	return providerModel{
		list:        l,
		viewport:    vp,
		providers:   providers,
		currentView: viewList,
	}
}

// Init initializes the model
func (m providerModel) Init() tea.Cmd {
	return nil
}

// Update handles messages
func (m providerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		keyStr := msg.String()

		// Handle quit (only when not filtering)
		if (keyStr == "q" || keyStr == "ctrl+c") && m.list.FilterState() != list.Filtering {
			m.quitting = true
			return m, tea.Quit
		}

		// Handle view switching
		switch m.currentView {
		case viewList:
			switch keyStr {
			case "enter", "right", "l":
				if m.list.FilterState() != list.Filtering {
					if item, ok := m.list.SelectedItem().(providerItem); ok {
						desc := item.provider.Descriptor()
						m.selected = desc
						m.currentView = viewDetail
						m.viewport.SetContent(m.renderDetail(desc))
						m.viewport.GotoTop()
					}
				}
			case "c":
				if m.list.FilterState() != list.Filtering {
					m.copySelectedYAML()
				}
			}
		case viewDetail:
			switch keyStr {
			case "left", "h", "esc", "backspace":
				m.currentView = viewList
				m.selected = nil
			case "c":
				m.copySelectedYAML()
			}
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.list.SetSize(msg.Width-4, msg.Height-6)
		m.viewport.SetWidth(msg.Width - 4)
		m.viewport.SetHeight(msg.Height - 6)
		if m.selected != nil {
			m.viewport.SetContent(m.renderDetail(m.selected))
		}
		return m, nil
	}

	// Update submodels based on current view
	switch m.currentView {
	case viewList:
		var cmd tea.Cmd
		m.list, cmd = m.list.Update(msg)
		cmds = append(cmds, cmd)
	case viewDetail:
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...)
}

// View renders the TUI
func (m providerModel) View() tea.View {
	if m.quitting {
		return tea.NewView("")
	}

	if m.width == 0 {
		return tea.NewView("Loading...")
	}

	var content string
	var title string

	if m.currentView == viewList {
		title = titleStyle.Render("📦 Providers")
		content = m.list.View()
	} else {
		if m.selected != nil {
			title = titleStyle.Render(fmt.Sprintf("📦 %s", m.selected.Name))
		} else {
			title = titleStyle.Render("Provider Details")
		}
		content = m.viewport.View()
	}

	// Build status bar
	var statusParts []string
	statusParts = append(statusParts, fmt.Sprintf("%d providers", len(m.providers)))
	if m.copied {
		statusParts = append(statusParts, "✓ copied to clipboard")
	}
	status := statusBarStyle.Render(strings.Join(statusParts, " | "))

	// Build help
	help := m.renderHelp()

	return tea.NewView(fmt.Sprintf("%s\n%s\n%s\n%s", title, content, status, help))
}

// renderDetail renders provider details
func (m providerModel) renderDetail(desc *provider.Descriptor) string {
	var b strings.Builder

	// Basic info
	b.WriteString(detailKeyStyle.Render("Name: "))
	b.WriteString(detailValueStyle.Render(desc.Name) + "\n")

	b.WriteString(detailKeyStyle.Render("Display Name: "))
	b.WriteString(detailValueStyle.Render(desc.DisplayName) + "\n")

	b.WriteString(detailKeyStyle.Render("Version: "))
	b.WriteString(detailValueStyle.Render(desc.Version.String()) + "\n")

	b.WriteString(detailKeyStyle.Render("API Version: "))
	b.WriteString(detailValueStyle.Render(desc.APIVersion) + "\n\n")

	// Description
	b.WriteString(detailKeyStyle.Render("Description:\n"))
	b.WriteString(detailValueStyle.Render(desc.Description) + "\n\n")

	// Capabilities
	b.WriteString(detailKeyStyle.Render("Capabilities: "))
	caps := make([]string, 0, len(desc.Capabilities))
	for _, cap := range desc.Capabilities {
		caps = append(caps, capabilityStyle.Render(string(cap)))
	}
	b.WriteString(strings.Join(caps, " ") + "\n\n")

	// Status flags
	if desc.Beta {
		b.WriteString(betaStyle.Render("⚠ This provider is in BETA") + "\n")
	}
	if desc.Deprecated { //nolint:staticcheck // Intentionally showing deprecated status
		b.WriteString(deprecatedStyle.Render("⚠ This provider is DEPRECATED") + "\n")
	}

	// Category/Tags
	if desc.Category != "" {
		b.WriteString(detailKeyStyle.Render("Category: "))
		b.WriteString(detailValueStyle.Render(desc.Category) + "\n")
	}
	if len(desc.Tags) > 0 {
		b.WriteString(detailKeyStyle.Render("Tags: "))
		b.WriteString(detailValueStyle.Render(strings.Join(desc.Tags, ", ")) + "\n")
	}

	// Mock behavior
	if desc.MockBehavior != "" {
		b.WriteString("\n" + detailKeyStyle.Render("Mock Behavior:\n"))
		b.WriteString(detailValueStyle.Render(desc.MockBehavior) + "\n")
	}

	// Schema properties
	if desc.Schema != nil && len(desc.Schema.Properties) > 0 {
		// Build required set
		requiredSet := make(map[string]bool, len(desc.Schema.Required))
		for _, name := range desc.Schema.Required {
			requiredSet[name] = true
		}
		b.WriteString("\n" + detailKeyStyle.Render("Schema Properties:\n"))
		for name, prop := range desc.Schema.Properties {
			required := ""
			if requiredSet[name] {
				required = " *"
			}
			typeStr := prop.Type
			if typeStr == "" {
				typeStr = "any"
			}
			b.WriteString(fmt.Sprintf("  %s (%s)%s\n", name, typeStr, required))
			if prop.Description != "" {
				b.WriteString(fmt.Sprintf("    %s\n", prop.Description))
			}
		}
	}

	// Examples
	if len(desc.Examples) > 0 {
		b.WriteString("\n" + detailKeyStyle.Render("Examples:\n"))
		for _, ex := range desc.Examples {
			b.WriteString(fmt.Sprintf("  %s: %s\n", ex.Name, ex.Description))
			if ex.YAML != "" {
				b.WriteString("  ---\n")
				for _, line := range strings.Split(ex.YAML, "\n") {
					b.WriteString("  " + line + "\n")
				}
			}
		}
	}

	// Links
	if len(desc.Links) > 0 {
		b.WriteString("\n" + detailKeyStyle.Render("Links:\n"))
		for _, link := range desc.Links {
			b.WriteString(fmt.Sprintf("  %s: %s\n", link.Name, link.URL))
		}
	}

	// Maintainers
	if len(desc.Maintainers) > 0 {
		b.WriteString("\n" + detailKeyStyle.Render("Maintainers:\n"))
		for _, maint := range desc.Maintainers {
			b.WriteString(fmt.Sprintf("  %s <%s>\n", maint.Name, maint.Email))
		}
	}

	return b.String()
}

// renderHelp renders the help bar
func (m providerModel) renderHelp() string {
	var keys []string
	if m.currentView == viewList {
		keys = []string{
			"↑↓ navigate",
			"→/enter expand",
			"/ filter",
			"c copy yaml",
			"q quit",
		}
	} else {
		keys = []string{
			"↑↓ scroll",
			"←/esc back",
			"c copy yaml",
			"q quit",
		}
	}
	return helpStyle.Render(strings.Join(keys, " • "))
}

// copySelectedYAML copies the selected provider's YAML to clipboard
func (m *providerModel) copySelectedYAML() {
	var desc *provider.Descriptor
	if m.selected != nil {
		desc = m.selected
	} else if item, ok := m.list.SelectedItem().(providerItem); ok {
		desc = item.provider.Descriptor()
	}

	if desc == nil {
		return
	}

	// Build example YAML
	example := buildExampleYAML(desc)

	// Try to copy to clipboard using system command
	if err := copyToClipboard(example); err == nil {
		m.copied = true
	}
}

// buildExampleYAML creates an example YAML for a provider
func buildExampleYAML(desc *provider.Descriptor) string {
	example := map[string]any{
		"provider": desc.Name,
	}

	// Add example properties from schema
	if desc.Schema != nil && len(desc.Schema.Properties) > 0 {
		for name, prop := range desc.Schema.Properties {
			switch {
			case len(prop.Examples) > 0:
				example[name] = prop.Examples[0]
			case prop.Default != nil:
				var def any
				_ = json.Unmarshal(prop.Default, &def)
				example[name] = def
			default:
				typeStr := prop.Type
				if typeStr == "" {
					typeStr = "any"
				}
				example[name] = fmt.Sprintf("<%s>", typeStr)
			}
		}
	}

	data, _ := yaml.Marshal(example)
	return string(data)
}

// copyToClipboard attempts to copy text to the system clipboard
// Uses context.Background() internally for clipboard operations
//
//nolint:noctx // Clipboard operations don't have meaningful context to pass
func copyToClipboard(text string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("pbcopy")
	case "linux":
		// Try xclip first, then xsel
		if _, err := exec.LookPath("xclip"); err == nil {
			cmd = exec.Command("xclip", "-selection", "clipboard")
		} else if _, err := exec.LookPath("xsel"); err == nil {
			cmd = exec.Command("xsel", "--clipboard", "--input")
		} else {
			return fmt.Errorf("no clipboard tool available (install xclip or xsel)")
		}
	case "windows":
		cmd = exec.Command("clip")
	default:
		return fmt.Errorf("clipboard not supported on %s", runtime.GOOS)
	}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}

	if err := cmd.Start(); err != nil {
		return err
	}

	if _, err := io.WriteString(stdin, text); err != nil {
		return err
	}

	if err := stdin.Close(); err != nil {
		return err
	}

	return cmd.Wait()
}

// RunTUI runs the interactive TUI for providers
func RunTUI(providers []provider.Provider, out io.Writer) error {
	// Check if output is a terminal using kvx helper
	if !kvx.IsTerminal(out) {
		// Not a terminal, print simple list
		return printSimpleList(providers, out)
	}

	// Run the TUI
	m := newProviderModel(providers)
	p := tea.NewProgram(m)
	_, err := p.Run()
	return err
}

// printSimpleList outputs providers as a simple list for non-interactive mode
func printSimpleList(providers []provider.Provider, out io.Writer) error {
	for _, p := range providers {
		desc := p.Descriptor()
		fmt.Fprintf(out, "%-20s %s\n", desc.Name, desc.Description)
	}
	return nil
}
