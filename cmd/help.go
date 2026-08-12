package cmd

import (
	"fmt"

	"github.com/MunchemOG/ElectroPush/internal/feature"
	"github.com/spf13/cobra"
)

const asciiArt = `
From Team #14270

 ██████╗ ██╗   ██╗ █████╗ ███╗   ██╗████████╗██╗   ██╗███╗   ███╗
██╔═══██╗██║   ██║██╔══██╗████╗  ██║╚══██╔══╝██║   ██║████╗ ████║
██║   ██║██║   ██║███████║██╔██╗ ██║   ██║   ██║   ██║██╔████╔██║
██║▄▄ ██║██║   ██║██╔══██║██║╚██╗██║   ██║   ██║   ██║██║╚██╔╝██║
╚██████╔╝╚██████╔╝██║  ██║██║ ╚████║   ██║   ╚██████╔╝██║ ╚═╝ ██║
 ╚══▀▀═╝  ╚═════╝ ╚═╝  ╚═╝╚═╝  ╚═══╝   ╚═╝    ╚═════╝ ╚═╝     ╚═╝

 ██████╗  ██████╗ ██████╗  ██████╗ ████████╗██╗ ██████╗███████╗
██╔══██╗██╔═══██╗██╔══██╗██╔═══██╗╚══██╔══╝██║██╔════╝██╔════╝
██████╔╝██║   ██║██████╔╝██║   ██║   ██║   ██║██║     ███████╗
██╔══██╗██║   ██║██╔══██╗██║   ██║   ██║   ██║██║     ╚════██║
██║  ██║╚██████╔╝██████╔╝╚██████╔╝   ██║   ██║╚██████╗███████║
╚═╝  ╚═╝ ╚═════╝ ╚═════╝  ╚═════╝    ╚═╝   ╚═╝ ╚═════╝╚══════╝ 

`

var helpCmd = &cobra.Command{
	Use:   "help",
	Short: "Show help information",
	Run:   runHelp,
}

func runHelp(cmd *cobra.Command, args []string) {
	fmt.Print(asciiArt)
	fmt.Println("Made with love by:")
	fmt.Println("	Andrei \"PzmuV1517\" Banu")
	fmt.Println("")
	fmt.Println("Commands:")
	fmt.Println("  epsh                Build and deploy to the robot")
	fmt.Println("  epsh connect        Join the robot Wi-Fi and connect adb")
	fmt.Println("  epsh exit           Disconnect adb and go back to your Wi-Fi")
	fmt.Println("  epsh dc             Disconnect adb only (alias: disconnect)")
	fmt.Println("  epsh settings       Robot profiles and preferences (alias: config)")
	fmt.Println("  epsh slim           Shrink the APK so deploys transfer less")
	fmt.Println("    epsh slim --undo       Put the gradle files back")
	fmt.Println("  epsh hwconfig       Hardware config menu and editor (alias: hw)")
	fmt.Println("    epsh hwconfig list     Print what the robot and the project have")
	fmt.Println("    epsh hwconfig pull     Copy the robot's configs into your project")
	fmt.Println("    epsh hwconfig push X   Copy X back to the robot")
	fmt.Println("  epsh dash diff      What the robot holds that your code does not")
	fmt.Println("  epsh prepare        Cache dependencies while you have internet")
	if feature.Revealed() {
		fmt.Println("  epsh visualiser     Draw the path an auto drove (alias: vis)")
	}
	fmt.Println("  epsh dev            Measure what a deploy costs (see the warning)")
	fmt.Println("  epsh update         Update epsh itself to the latest release")
	fmt.Println("    epsh update --check    Say what is available, install nothing")
	fmt.Println("  epsh --version      Show version information")
	fmt.Println("  epsh help           Show this help")
	fmt.Println("")
	fmt.Println("Epsh Extreme:")
	fmt.Println("  Reloads your OpModes onto a running robot instead of installing an")
	fmt.Println("  APK: under a second rather than around forty. Set it up in")
	fmt.Println("  'epsh settings' -> Epsh Extreme, which also undoes it.")
	fmt.Println("  While it is set up your team code is not part of the APK.")
	fmt.Println("")
	fmt.Println("epsh dev:")
	fmt.Println("  Measuring tools for working on epsh itself. It deploys to the")
	fmt.Println("  robot repeatedly and reinstalls the app several times. If you do")
	fmt.Println("  not already know why you want it, you do not want it.")
	fmt.Println("")
	fmt.Println("Deploying:")
	fmt.Println("  A hub on USB is used automatically and your Wi-Fi is left alone.")
	fmt.Println("  Otherwise epsh builds first, hops to the robot, deploys, and")
	fmt.Println("  puts you back on the network you started on.")
}
