package cmd

import (
	"fmt"
	"strings"

	"github.com/MunchemOG/ElectroPush/internal/feature"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
)

var (
	accent   = lipgloss.Color("39")
	accent2  = lipgloss.Color("212")
	dim      = lipgloss.Color("244")
	faintDim = lipgloss.Color("238")
	green    = lipgloss.Color("42")
	warn     = lipgloss.Color("214")

	brandStyle   = lipgloss.NewStyle().Bold(true).Foreground(accent)
	taglineStyle = lipgloss.NewStyle().Foreground(dim).Italic(true)
	boxStyle     = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(accent).
			Padding(1, 3)

	sectionBar   = lipgloss.NewStyle().Foreground(accent2).Bold(true)
	sectionTitle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("255"))
	ruleStyle    = lipgloss.NewStyle().Foreground(faintDim)

	cmdStyle  = lipgloss.NewStyle().Foreground(green).Bold(true)
	subStyle  = lipgloss.NewStyle().Foreground(dim)
	descStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("250"))
	pipeStyle = lipgloss.NewStyle().Foreground(faintDim)
	warnStyle = lipgloss.NewStyle().Foreground(warn)
	noteStyle = lipgloss.NewStyle().Foreground(dim)
)

const asciiArt = `
███████╗██╗     ███████╗ ██████╗████████╗██████╗  ██████╗ ██████╗ ██╗   ██╗███████╗██╗  ██╗
██╔════╝██║     ██╔════╝██╔════╝╚══██╔══╝██╔══██╗██╔═══██╗██╔══██╗██║   ██║██╔════╝██║  ██║
█████╗  ██║     █████╗  ██║        ██║   ██████╔╝██║   ██║██████╔╝██║   ██║███████╗███████║
██╔══╝  ██║     ██╔══╝  ██║        ██║   ██╔══██╗██║   ██║██╔═══╝ ██║   ██║╚════██║██╔══██║
███████╗███████╗███████╗╚██████╗   ██║   ██║  ██║╚██████╔╝██║     ╚██████╔╝███████║██║  ██║
╚══════╝╚══════╝╚══════╝ ╚═════╝   ╚═╝   ╚═╝  ╚═╝ ╚═════╝ ╚═╝      ╚═════╝ ╚══════╝╚═╝  ╚═╝`

var helpCmd = &cobra.Command{
	Use:   "help",
	Short: "Show help information",
	Run:   runHelp,
}

// row pads a rendered (possibly colored) string to a visual width,
// measuring with lipgloss.Width so ANSI codes don't throw off alignment.
func row(indent int, name, desc string) string {
	pad := strings.Repeat(" ", indent)
	col := pad + name
	gap := 26 - lipgloss.Width(col)
	if gap < 1 {
		gap = 1
	}
	return col + strings.Repeat(" ", gap) + descStyle.Render(desc)
}

func section(title string) string {
	bar := sectionBar.Render("▍")
	heading := sectionTitle.Render(strings.ToUpper(title))
	rule := ruleStyle.Render(strings.Repeat("─", 60-len(title)))
	return fmt.Sprintf("%s %s %s", bar, heading, rule)
}

func runHelp(cmd *cobra.Command, args []string) {
	header := brandStyle.Render(asciiArt) + "\n" +
		taglineStyle.Render("forked from Team #14270  ·  made by Munchem #30686")
	fmt.Println(boxStyle.Render(header))
	fmt.Println()

	fmt.Println(subStyle.Render("USAGE"))
	fmt.Println("  epsh <command> [options]")
	fmt.Println()

	fmt.Println(section("Deploy"))
	fmt.Println(row(2, cmdStyle.Render("epsh"), "Build and deploy to the robot"))
	fmt.Println(row(2, cmdStyle.Render("epsh slim"), "Shrink the APK so deploys transfer less"))
	fmt.Println(row(4, subStyle.Render("↳ --undo"), "Revert slim changes"))
	fmt.Println(row(2, cmdStyle.Render("epsh prepare"), "Cache dependencies while you have internet"))
	fmt.Println()

	fmt.Println(section("Connection"))
	fmt.Println(row(2, cmdStyle.Render("epsh connect"), "Join the robot Wi-Fi and connect adb"))
	fmt.Println(row(2, cmdStyle.Render("epsh exit"), "Disconnect adb and go back to your Wi-Fi"))
	fmt.Println(row(2, cmdStyle.Render("epsh dc")+pipeStyle.Render(" | ")+cmdStyle.Render("disconnect"), "Disconnect adb only"))
	fmt.Println()

	fmt.Println(section("Config"))
	fmt.Println(row(2, cmdStyle.Render("epsh settings")+pipeStyle.Render(" | ")+cmdStyle.Render("config"), "Robot profiles and preferences"))
	fmt.Println(row(2, cmdStyle.Render("epsh hwconfig")+pipeStyle.Render(" | ")+cmdStyle.Render("hw"), "Hardware config menu and editor"))
	fmt.Println(row(4, subStyle.Render("↳ list"), "Print what the robot and the project have"))
	fmt.Println(row(4, subStyle.Render("↳ pull"), "Copy the robot's configs into your project"))
	fmt.Println(row(4, subStyle.Render("↳ push <name>"), "Copy a config back to the robot"))
	fmt.Println(row(2, cmdStyle.Render("epsh dash diff"), "What the robot holds that your code does not"))
	if feature.Revealed() {
		fmt.Println(row(2, cmdStyle.Render("epsh visualiser")+pipeStyle.Render(" | ")+cmdStyle.Render("vis"), "Draw the path an auto drove"))
	}
	fmt.Println()

	fmt.Println(section("Maintenance"))
	fmt.Println(row(2, cmdStyle.Render("epsh update"), "Update epsh itself to the latest release"))
	fmt.Println(row(4, subStyle.Render("↳ --check"), "Say what is available, install nothing"))
	fmt.Println(row(2, cmdStyle.Render("epsh dev"), warnStyle.Render("Measure deploy cost (advanced)")))
	fmt.Println(row(2, cmdStyle.Render("epsh --version"), "Show version information"))
	fmt.Println(row(2, cmdStyle.Render("epsh help"), "Show this help"))
	fmt.Println()

	fmt.Println(ruleStyle.Render(strings.Repeat("─", 78)))
	fmt.Println(noteStyle.Render("Epsh Extreme   reloads OpModes onto a running robot in <1s instead of ~40s."))
	fmt.Println(noteStyle.Render("               Set it up in 'epsh settings' → Epsh Extreme, which also"))
	fmt.Println(noteStyle.Render("               undoes it. While active, your team code isn't in the APK."))
	fmt.Println()
	fmt.Println(noteStyle.Render("Deploying      a hub on USB is used automatically and your Wi-Fi is left"))
	fmt.Println(noteStyle.Render("               alone. Otherwise epsh builds first, hops to the robot,"))
	fmt.Println(noteStyle.Render("               deploys, and puts you back on your original network."))
}
