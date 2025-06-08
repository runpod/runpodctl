package cmd

import (
	"fmt"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/spf13/cobra"
)

var tuiCmd = &cobra.Command{
	Use:   "tui",
	Short: "Launch TUI interface to manage pods, templates, and endpoints",
	Long:  "Launch a text-based user interface to manage RunPod pods, templates, and endpoints",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runTUI()
	},
}

func init() {
	rootCmd.AddCommand(tuiCmd)
}

func runTUI() error {
	app := tview.NewApplication()

	runpodPurple := tcell.NewRGBColor(130, 78, 220)
	runpodBlue := tcell.NewRGBColor(97, 52, 226)
	runpodDarkBg := tcell.NewRGBColor(13, 0, 51)
	runpodLightGray := tcell.NewRGBColor(203, 204, 210)

	tview.Styles.PrimitiveBackgroundColor = runpodDarkBg
	tview.Styles.ContrastBackgroundColor = runpodDarkBg
	tview.Styles.MoreContrastBackgroundColor = runpodDarkBg
	tview.Styles.PrimaryTextColor = runpodLightGray
	tview.Styles.SecondaryTextColor = runpodLightGray

	pages := tview.NewPages()

	currentScreen := "pods"

	navHeader := tview.NewTextView()
	navHeader.SetDynamicColors(true)
	navHeader.SetBackgroundColor(runpodDarkBg)
	navHeader.SetTextAlign(tview.AlignCenter)
	updateNavHeader := func() {
		var podsStyle, templatesStyle, endpointsStyle string
		switch currentScreen {
		case "pods":
			podsStyle = "[#CBCCD2:#824edc:b] PODS [-:-:-]"
			templatesStyle = "[#666666]Templates[-]"
			endpointsStyle = "[#666666]Endpoints[-]"
		case "templates":
			podsStyle = "[#666666]Pods[-]"
			templatesStyle = "[#CBCCD2:#824edc:b] TEMPLATES [-:-:-]"
			endpointsStyle = "[#666666]Endpoints[-]"
		case "endpoints":
			podsStyle = "[#666666]Pods[-]"
			templatesStyle = "[#666666]Templates[-]"
			endpointsStyle = "[#CBCCD2:#824edc:b] ENDPOINTS [-:-:-]"
		}
		navHeader.SetText(fmt.Sprintf("%s  |  %s  |  %s\n[#6134E2]Use 1,2,3 to switch screens | Press q to quit[-]",
			podsStyle, templatesStyle, endpointsStyle))
	}
	updateNavHeader()

	podsScreen, refreshPods := createPodsScreen(app, pages, runpodPurple, runpodBlue, runpodDarkBg, runpodLightGray)
	templatesScreen, refreshTemplates := createTemplatesScreen(app, pages, runpodPurple, runpodBlue, runpodDarkBg, runpodLightGray)
	endpointsScreen, refreshEndpoints := createEndpointsScreen(app, pages, runpodPurple, runpodBlue, runpodDarkBg, runpodLightGray)

	switchScreen := func(newScreen string) {
		if newScreen == currentScreen {
			return
		}
		oldScreen := currentScreen
		currentScreen = newScreen
		updateNavHeader()

		switch currentScreen {
		case "pods":
			pages.SwitchToPage("pods")
			app.SetFocus(podsScreen)
		case "templates":
			pages.SwitchToPage("templates")
			app.SetFocus(templatesScreen)
			if oldScreen != "templates" {
				go refreshTemplates()
			}
		case "endpoints":
			pages.SwitchToPage("endpoints")
			app.SetFocus(endpointsScreen)
			if oldScreen != "endpoints" {
				go refreshEndpoints()
			}
		}
	}

	header := tview.NewTextView()
	header.SetDynamicColors(true)
	header.SetBackgroundColor(runpodDarkBg)
	header.SetTextAlign(tview.AlignCenter)
	header.SetText(fmt.Sprintf("[#824edc]██████╗ ██╗   ██╗███╗   ██╗██████╗  ██████╗ ██████╗ [-]\n[#824edc]██╔══██╗██║   ██║████╗  ██║██╔══██╗██╔═══██╗██╔══██╗[-]\n[#824edc]██████╔╝██║   ██║██╔██╗ ██║██████╔╝██║   ██║██║  ██║[-]\n[#824edc]██╔══██╗██║   ██║██║╚██╗██║██╔═══╝ ██║   ██║██║  ██║[-]\n[#824edc]██║  ██║╚██████╔╝██║ ╚████║██║     ╚██████╔╝██████╔╝[-]\n[#824edc]╚═╝  ╚═╝ ╚═════╝ ╚═╝  ╚═══╝╚═╝      ╚═════╝ ╚═════╝[-]\n\n[#CBCCD2]Multi-Service Management Interface[-]"))

	loadingView := tview.NewTextView()
	loadingView.SetDynamicColors(true)
	loadingView.SetBackgroundColor(runpodDarkBg)
	loadingView.SetTextColor(runpodLightGray)
	loadingView.SetTextAlign(tview.AlignCenter)
	loadingView.SetText(fmt.Sprintf(`[#824edc]Loading RunPod Data...[-]

⣾ Fetching data from API...

[#CBCCD2]Please wait while we retrieve your information[-]`))

	mainLayout := tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(header, 8, 0, false).
		AddItem(navHeader, 3, 0, false)

	paddedLayout := tview.NewFlex().
		SetDirection(tview.FlexColumn).
		AddItem(tview.NewBox().SetBackgroundColor(runpodDarkBg), 1, 0, false).
		AddItem(mainLayout, 0, 1, true).
		AddItem(tview.NewBox().SetBackgroundColor(runpodDarkBg), 1, 0, false)

	pages.AddPage("pods", tview.NewFlex().SetDirection(tview.FlexRow).AddItem(paddedLayout, 11, 0, false).AddItem(podsScreen, 0, 1, true), true, true)
	pages.AddPage("templates", tview.NewFlex().SetDirection(tview.FlexRow).AddItem(paddedLayout, 11, 0, false).AddItem(templatesScreen, 0, 1, true), true, false)
	pages.AddPage("endpoints", tview.NewFlex().SetDirection(tview.FlexRow).AddItem(paddedLayout, 11, 0, false).AddItem(endpointsScreen, 0, 1, true), true, false)
	pages.AddPage("loading", loadingView, true, false)

	app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Rune() {
		case '1':
			switchScreen("pods")
			return nil
		case '2':
			switchScreen("templates")
			return nil
		case '3':
			switchScreen("endpoints")
			return nil
		case 'q':
			app.Stop()
			return nil
		}
		return event
	})

	loadingView.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Rune() {
		case 'q':
			app.Stop()
			return nil
		case 'r', 'R':
			switch currentScreen {
			case "pods":
				go refreshPods()
			case "templates":
				go refreshTemplates()
			case "endpoints":
				go refreshEndpoints()
			}
			return nil
		}

		switch event.Key() {
		case tcell.KeyEscape:
			pages.SwitchToPage(currentScreen)
			return nil
		}

		return event
	})

	switchScreen("pods")

	if err := app.SetRoot(pages, true).Run(); err != nil {
		return fmt.Errorf("error running TUI: %v", err)
	}

	return nil
}