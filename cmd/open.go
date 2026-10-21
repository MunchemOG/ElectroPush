package cmd

import (
	"fmt"
	"os/exec"
	"sort"
	"strconv"
	"strings"

	"github.com/MunchemOG/ElectroPush/internal/adb"
	"github.com/MunchemOG/ElectroPush/internal/bridge"
	"github.com/MunchemOG/ElectroPush/internal/power"
	"github.com/spf13/cobra"
)

// robotPage is a page the robot serves, and every port the page itself talks
// to once it is open. The pages find their sockets from the address they were
// loaded from, so over USB each one has to be forwarded on its own number.
type robotPage struct {
	Port  int
	Path  string
	Ports []int
	What  string
}

var robotPages = map[string]robotPage{
	"rc":        {Port: 8080, Ports: []int{8080}, What: "the hub's own manage page"},
	"dash":      {Port: 8080, Path: "/dash", Ports: []int{8080, 8000}, What: "FtcDashboard"},
	"panels":    {Port: 8001, Ports: []int{8001, 8002}, What: "Panels"},
	"autotune":  {Port: 10158, Ports: []int{10158, 12649}, What: "Pedro's AutoTune, with ElectroDromos's tuners"},
	"limelight": {Port: 5801, Ports: []int{5801, 5800, 5805}, What: "the Limelight, through the Panels proxy"},
}

var openPrint bool

var openCmd = &cobra.Command{
	Use:   "open <" + strings.Join(pageNames(), "|") + ">",
	Short: "Open one of the robot's pages in your browser, however it is attached",
	Long: `Opens a page the robot serves, at the address that works from here: the
robot's own over Wi-Fi, the tunnel's end through a bridge, and over USB a port
forward set up for it, including the sockets the page opens after it loads.

  rc         the hub's own manage page
  dash       FtcDashboard
  panels     Panels
  autotune   Pedro's AutoTune, with ElectroDromos's tuners
  limelight  the Limelight, through the Panels proxy`,
	Args:      cobra.ExactArgs(1),
	ValidArgs: pageNames(),
	RunE:      runOpen,
}

func init() {
	openCmd.Flags().BoolVar(&openPrint, "print", false, "Print the address instead of opening it")
}

func pageNames() []string {
	names := make([]string, 0, len(robotPages))
	for name := range robotPages {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func runOpen(cmd *cobra.Command, args []string) error {
	page, ok := robotPages[strings.ToLower(args[0])]
	if !ok {
		return fmt.Errorf("no page called %q: pick one of %s", args[0], strings.Join(pageNames(), ", "))
	}

	serial, err := requireRobot()
	if err != nil {
		return err
	}

	_, bridged := bridge.Joined()
	url, forward := pageAddress(page, serial, bridged && serial == adb.RobotAddr())

	for _, port := range forward {
		p := strconv.Itoa(port)
		if out, err := exec.Command("adb", "-s", serial, "forward", "tcp:"+p, "tcp:"+p).CombinedOutput(); err != nil {
			return fmt.Errorf("cannot forward port %d over USB: %s", port, strings.TrimSpace(string(out)))
		}
	}

	if openPrint {
		fmt.Println(url)
		return nil
	}

	uiStatus("ok", fmt.Sprintf("%s · %s", page.What, url))
	if len(forward) > 0 {
		uiNote("Forwarded over USB. `adb forward --remove-all` takes the forwards down.")
	}
	power.Open(url)
	return nil
}

// pageAddress is where a page opens from here, and which ports have to be
// forwarded over USB first for it to work.
func pageAddress(page robotPage, serial string, bridged bool) (string, []int) {
	switch {
	case bridged:
		port := page.Port
		if local, ok := bridge.Local(page.Port); ok {
			port = local
		}
		return fmt.Sprintf("http://localhost:%d%s", port, page.Path), nil

	case strings.Contains(serial, ":"):
		host, _, _ := strings.Cut(serial, ":")
		return fmt.Sprintf("http://%s:%d%s", host, page.Port, page.Path), nil

	default:
		return fmt.Sprintf("http://localhost:%d%s", page.Port, page.Path), page.Ports
	}
}
