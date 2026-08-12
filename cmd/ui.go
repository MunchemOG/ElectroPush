package cmd

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

var (
	uiSuccessStyle = lipgloss.NewStyle().Foreground(green).Bold(true)
	uiInfoStyle    = lipgloss.NewStyle().Foreground(accent).Bold(true)
	uiWarnStyle    = lipgloss.NewStyle().Foreground(warn).Bold(true)
	uiMutedStyle   = lipgloss.NewStyle().Foreground(dim)
	uiLabelStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("255")).Bold(true)
)

// uiHeading gives command output the same visual hierarchy as the main help
// screen, while keeping the live log compact enough to follow during a deploy.
func uiHeading(title, detail string) {
	fmt.Println()
	fmt.Println(section(title))
	if detail != "" {
		fmt.Println(uiMutedStyle.Render("  " + detail))
	}
}

func uiStatus(kind, message string) {
	var badge string
	switch kind {
	case "ok":
		badge = uiSuccessStyle.Render("✓")
	case "warn":
		badge = uiWarnStyle.Render("!")
	case "run":
		badge = uiInfoStyle.Render("●")
	case "wait":
		badge = sectionBar.Render("◆")
	default:
		badge = uiMutedStyle.Render("·")
	}
	fmt.Printf("  %s %s\n", badge, message)
}

func uiNote(message string) { fmt.Println(uiMutedStyle.Render("    " + message)) }

func uiRule() { fmt.Println(ruleStyle.Render("  " + strings.Repeat("─", 58))) }

// runCommandHelp replaces Cobra's stock help with the same visual language as
// `epsh help`, including command descriptions, subcommands, and flags.
func runCommandHelp(cmd *cobra.Command, _ []string) {
	uiHeading(cmd.CommandPath(), cmd.Short)

	if cmd.Long != "" && cmd.Long != cmd.Short {
		fmt.Println(descStyle.Render("  " + strings.ReplaceAll(cmd.Long, "\n", "\n  ")))
		fmt.Println()
	}

	fmt.Println(subStyle.Render("USAGE"))
	fmt.Println("  " + cmd.UseLine())

	if cmd.HasAvailableSubCommands() {
		fmt.Println()
		fmt.Println(section("Commands"))
		for _, child := range cmd.Commands() {
			if child.IsAvailableCommand() {
				fmt.Println(row(2, cmdStyle.Render(child.Name()), child.Short))
			}
		}
	}

	flags := cmd.NonInheritedFlags()
	if flags.HasAvailableFlags() {
		fmt.Println()
		fmt.Println(section("Options"))
		flags.VisitAll(func(flag *pflag.Flag) {
			name := "--" + flag.Name
			if flag.Shorthand != "" {
				name = "-" + flag.Shorthand + ", " + name
			}
			fmt.Println(row(2, cmdStyle.Render(name), flag.Usage))
		})
	}

	if len(cmd.Aliases) > 0 {
		fmt.Println()
		uiNote("Aliases: " + strings.Join(cmd.Aliases, ", "))
	}
	fmt.Println()
}
