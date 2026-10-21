package tui

import (
	"fmt"
	"time"

	"github.com/MunchemOG/ElectroPush/internal/config"
	"github.com/MunchemOG/ElectroPush/internal/ftcproject"
	"github.com/MunchemOG/ElectroPush/internal/gradle"
	tea "github.com/charmbracelet/bubbletea"
)

var deployItems = []string{
	"Send only changed parts",
	"Skip install when unchanged",
	"Stream the install",
	"Store native libraries uncompressed",
	"Install only changed splits",
	"Reconnect attempts",
	"Connection check timeout",
	"Back",
}

var deployHelp = []string{
	"Sends only the parts of the APK that changed since the last deploy.\n" +
		"Big win over Wi-Fi, little to nothing over USB.",

	"If the robot already holds exactly this build, do nothing at all.\n" +
		"Free. Only skips when the package has not been touched since.",

	"Write the APK straight into an install session instead of pushing it\n" +
		"to a temporary file first. Halves what gets written on the robot.",

	"Stop compressing native libraries in the APK, so the install does not\n" +
		"extract them. Removes over 20 MB of writes; makes the APK bigger.\n" +
		"Applied by `epsh slim`.",

	"When the project builds a base plus a feature module, install only the\n" +
		"module that changed. Needs the project set up that way first.",

	"How many times one deploy reconnects after the robot drops offline.\n" +
		"Press enter to cycle between one, two, and three attempts.",

	"Maximum time for the quick connection check before a deploy starts.\n" +
		"Press enter to cycle between 1, 3, 5, and 10 seconds.",

	"",
}

func (m *SettingsModel) deployLabel() string {
	on := 0
	for _, enabled := range []bool{
		config.GetDeltaTransfer(),
		config.GetSkipUnchanged(),
		config.GetStreamInstall(),
		config.GetStoreLibs(),
		config.GetSplitInstall(),
	} {
		if enabled {
			on++
		}
	}

	return fmt.Sprintf("%d of 5 on", on)
}

func (m *SettingsModel) updateDeploy(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key.String() {
	case "q":
		m.quit = true
		return m, tea.Quit

	case "esc", "left", "h":
		m.goTo(screenMain, 8)
		return m, nil

	case "up", "k":
		m.moveCursor(-1, len(deployItems))
	case "down", "j":
		m.moveCursor(1, len(deployItems))

	case "enter", " ", "right", "l":
		m.err = nil
		m.status = ""

		switch m.cursor {
		case 0:
			m.setStatus(config.SetDeltaTransfer(!config.GetDeltaTransfer()), "Delta transfer updated")
		case 1:
			m.setStatus(config.SetSkipUnchanged(!config.GetSkipUnchanged()), "Skip-when-unchanged updated")
		case 2:
			m.setStatus(config.SetStreamInstall(!config.GetStreamInstall()), "Streaming install updated")
		case 3:
			m.toggleStoreLibs()
		case 4:
			m.setStatus(config.SetSplitInstall(!config.GetSplitInstall()), "Split install updated")
		case 5:
			m.cycleReconnectAttempts()
		case 6:
			m.cycleConnectionTimeout()
		case 7:
			m.goTo(screenMain, 8)
		}
	}

	return m, nil
}

func (m *SettingsModel) cycleReconnectAttempts() {
	next := config.GetReconnectAttempts() + 1
	if next > 3 {
		next = 1
	}
	m.setStatus(config.SetReconnectAttempts(next), fmt.Sprintf("Reconnect attempts set to %d", next))
}

func (m *SettingsModel) cycleConnectionTimeout() {
	current := config.GetConnectionTimeout()
	next := 1 * time.Second
	for _, option := range []time.Duration{1 * time.Second, 3 * time.Second, 5 * time.Second, 10 * time.Second} {
		if option > current {
			next = option
			break
		}
	}
	m.setStatus(config.SetConnectionTimeout(next), fmt.Sprintf("Connection check timeout set to %s", next))
}

func (m *SettingsModel) toggleStoreLibs() {
	want := !config.GetStoreLibs()

	if err := config.SetStoreLibs(want); err != nil {
		m.err = err
		return
	}

	project, err := m.ftcProject()
	if err != nil {
		m.status = "Saved. Run `epsh slim` from your FTC project to apply it."
		return
	}

	if _, err := project.StoreLibs(!want); err != nil {
		m.err = err
		return
	}

	if want {
		m.status = "Native libraries will be stored; rebuild to apply"
	} else {
		m.status = "Native libraries will be compressed again; rebuild to apply"
	}
}

func (m *SettingsModel) ftcProject() (*ftcproject.Project, error) {
	wrapper, err := gradle.DetectWrapper()
	if err != nil {
		return nil, err
	}
	return ftcproject.Detect(gradle.ProjectDir(wrapper))
}

func (m *SettingsModel) viewDeploy() string {
	values := []string{
		onOff(config.GetDeltaTransfer()),
		onOff(config.GetSkipUnchanged()),
		onOff(config.GetStreamInstall()),
		m.storeLibsLabel(),
		onOff(config.GetSplitInstall()),
		fmt.Sprintf("%d", config.GetReconnectAttempts()),
		config.GetConnectionTimeout().String(),
		"",
	}

	after := note(deployHelp, m.cursor, m.width) +
		"\n" + helpStyle.Render("  "+fit("enter change · esc back · `epsh dev` measures the difference", textWidth(m.width))) + "\n"

	return m.fill(helpStyle.Render("  Deploy speed")+"\n\n", after, len(deployItems), func(i int) string {
		return renderRow(i == m.cursor, deployItems[i], values[i], 37, m.width)
	})
}

func (m *SettingsModel) storeLibsLabel() string {
	setting := onOff(config.GetStoreLibs())

	project, err := m.ftcProject()
	if err != nil {
		return setting
	}

	stored := !project.LegacyPackaging()
	if stored == config.GetStoreLibs() {
		return setting
	}

	return setting + " (project not rebuilt)"
}
