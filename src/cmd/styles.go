package cmd

import (
	"github.com/SteerSpec/strspc-CLI/src/internal/ui"
	"github.com/charmbracelet/lipgloss"
)

var (
	brandStyle = lipgloss.NewStyle().Bold(true).Foreground(ui.Primary)
	labelStyle = lipgloss.NewStyle().Foreground(ui.Secondary).Width(10)
	valueStyle = lipgloss.NewStyle().Foreground(ui.Accent)
	cmdStyle   = lipgloss.NewStyle().Bold(true).Foreground(ui.Primary)
	descStyle  = lipgloss.NewStyle().Foreground(ui.Secondary)
)
