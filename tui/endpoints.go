package tui

import (
	"fmt"
	"strconv"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/runpod/runpodctl/api"
)

func CreateEndpointsScreen(app *tview.Application, pages *tview.Pages, runpodPurple, runpodBlue, runpodDarkBg, runpodLightGray tcell.Color) (*tview.Flex, func()) {
	table := CreateBaseTable(runpodBlue)

	selectedBg := tcell.NewRGBColor(20, 10, 60)

	var endpoints []*api.Endpoint
	var terminalWidth int = 120
	var lastTerminalWidth int = 120

	emptyState := CreateEmptyState("🌐 No endpoints found", "You don't have any endpoints yet.", "endpoint", "runpodctl create endpoint", runpodDarkBg)

	contentArea := tview.NewFlex()

	loadingContent := CreateLoadingContent("Loading endpoints...", "Fetching data from API...", runpodDarkBg)

	var updateContent func()
	var showLoading func()
	updateContent = func() {
		contentArea.Clear()
		if len(endpoints) > 0 {
			contentArea.AddItem(table, 0, 1, true)
			app.SetFocus(table)
		} else {
			contentArea.AddItem(emptyState, 0, 1, true)
			app.SetFocus(emptyState)
		}
	}

	showLoading = func() {
		contentArea.Clear()
		contentArea.AddItem(loadingContent, 0, 1, true)
		app.SetFocus(loadingContent)
	}

	repopulateTable := func() {
		app.QueueUpdateDraw(func() {
			table.Clear()

			table.SetCell(0, 0, CreateHeaderCell("Name", runpodPurple))
			table.SetCell(0, 1, CreateHeaderCell("ID", runpodPurple))
			table.SetCell(0, 2, CreateHeaderCell("Type", runpodPurple))
			table.SetCell(0, 3, CreateHeaderCell("GPU Count", runpodPurple))
			table.SetCell(0, 4, CreateHeaderCell("Workers", runpodPurple))
			table.SetCell(0, 5, CreateHeaderCell("Scaler", runpodPurple))
			table.SetCell(0, 6, CreateHeaderCell("Timeout", runpodPurple))
			table.SetCell(0, 7, CreateHeaderCell("Locations", runpodPurple))

			for i, endpoint := range endpoints {
				row := i + 1

				nameWidth := int(float64(terminalWidth) * 0.25)
				idWidth := int(float64(terminalWidth) * 0.15)
				locationWidth := int(float64(terminalWidth) * 0.20)

				if nameWidth < 8 {
					nameWidth = 8
				}
				if idWidth < 6 {
					idWidth = 6
				}
				if locationWidth < 8 {
					locationWidth = 8
				}

				table.SetCell(row, 0, tview.NewTableCell(" "+FormatColumnText(endpoint.Name, nameWidth-2)+" ").
					SetSelectedStyle(tcell.StyleDefault.Foreground(runpodLightGray).Background(selectedBg)))

				table.SetCell(row, 1, tview.NewTableCell(" "+FormatColumnText(endpoint.Id, idWidth-2)+" ").
					SetSelectedStyle(tcell.StyleDefault.Foreground(runpodLightGray).Background(selectedBg)))

				typeColor := runpodLightGray
				if endpoint.Type == "SERVERLESS" {
					typeColor = tcell.ColorLimeGreen
				}
				table.SetCell(row, 2, tview.NewTableCell(" "+endpoint.Type+" ").
					SetTextColor(typeColor).
					SetSelectedStyle(tcell.StyleDefault.Foreground(typeColor).Background(selectedBg)))

				gpuCountText := "N/A"
				if endpoint.GpuCount > 0 {
					gpuCountText = strconv.Itoa(endpoint.GpuCount)
				}
				table.SetCell(row, 3, tview.NewTableCell(" "+gpuCountText+" ").
					SetSelectedStyle(tcell.StyleDefault.Foreground(runpodLightGray).Background(selectedBg)))

				workersText := fmt.Sprintf("%d-%d", endpoint.WorkersMin, endpoint.WorkersMax)
				workersColor := runpodLightGray
				if endpoint.WorkersMin == 0 && endpoint.WorkersMax == 0 {
					workersColor = tcell.ColorOrangeRed
					workersText = "0-0"
				} else if endpoint.WorkersMin > 0 {
					workersColor = tcell.ColorLimeGreen
				}
				table.SetCell(row, 4, tview.NewTableCell(" "+workersText+" ").
					SetTextColor(workersColor).
					SetSelectedStyle(tcell.StyleDefault.Foreground(workersColor).Background(selectedBg)))

				scalerText := fmt.Sprintf("%s:%d", endpoint.ScalerType, endpoint.ScalerValue)
				table.SetCell(row, 5, tview.NewTableCell(" "+scalerText+" ").
					SetSelectedStyle(tcell.StyleDefault.Foreground(runpodLightGray).Background(selectedBg)))

				timeoutText := fmt.Sprintf("%ds", endpoint.IdleTimeout)
				timeoutColor := runpodLightGray
				if endpoint.IdleTimeout < 60 {
					timeoutColor = tcell.ColorOrangeRed
				} else if endpoint.IdleTimeout >= 300 {
					timeoutColor = tcell.ColorLimeGreen
				}
				table.SetCell(row, 6, tview.NewTableCell(" "+timeoutText+" ").
					SetTextColor(timeoutColor).
					SetSelectedStyle(tcell.StyleDefault.Foreground(timeoutColor).Background(selectedBg)))

				table.SetCell(row, 7, tview.NewTableCell(" "+FormatColumnText(endpoint.Locations, locationWidth-2)+" ").
					SetSelectedStyle(tcell.StyleDefault.Foreground(runpodLightGray).Background(selectedBg)))
			}

			if len(endpoints) > 0 {
				table.Select(1, 0)
			}
		})
	}

	updateColumnSizing := CreateColumnSizingFunc(&terminalWidth, &lastTerminalWidth, func() bool { return len(endpoints) > 0 }, repopulateTable)

	table.SetDrawFunc(func(screen tcell.Screen, x int, y int, width int, height int) (int, int, int, int) {
		updateColumnSizing(width)
		return x, y, width, height
	})

	updateColumnSizing(120)

	table.SetCell(0, 0, CreateHeaderCell("Name", runpodPurple))
	table.SetCell(0, 1, CreateHeaderCell("ID", runpodPurple))
	table.SetCell(0, 2, CreateHeaderCell("Type", runpodPurple))
	table.SetCell(0, 3, CreateHeaderCell("GPU Count", runpodPurple))
	table.SetCell(0, 4, CreateHeaderCell("Workers", runpodPurple))
	table.SetCell(0, 5, CreateHeaderCell("Scaler", runpodPurple))
	table.SetCell(0, 6, CreateHeaderCell("Timeout", runpodPurple))
	table.SetCell(0, 7, CreateHeaderCell("Locations", runpodPurple))

	refreshEndpoints := func() {
		app.QueueUpdateDraw(func() {
			showLoading()
		})

		var err error
		endpoints, err = api.GetEndpoints()
		if err != nil {
			app.QueueUpdateDraw(func() {
				errorContent := CreateErrorContent("Error Loading Endpoints", err.Error(), runpodDarkBg)

				errorContent.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
					switch event.Rune() {
					case 'r', 'R':
						app.QueueUpdateDraw(func() {
							showLoading()
						})
						go func() {
							newEndpoints, retryErr := api.GetEndpoints()
							if retryErr == nil {
								endpoints = newEndpoints
								repopulateTable()
								app.QueueUpdateDraw(func() {
									updateContent()
								})
							}
						}()
						return nil
					}
					return event
				})

				contentArea.Clear()
				contentArea.AddItem(errorContent, 0, 1, true)
				app.SetFocus(errorContent)
			})
			return
		}

		repopulateTable()
		app.QueueUpdateDraw(func() {
			updateContent()
		})
	}

	statusBar := CreateStatusBar("[#6134E2]Enter[-] - Details | [#6134E2]d[-] - Delete | [#6134E2]r/F5[-] - Refresh", runpodDarkBg)

	mainFlex := tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(contentArea, 0, 1, true).
		AddItem(statusBar, 1, 0, false)

	emptyState.SetInputCapture(CreateBasicInputCapture(app, refreshEndpoints))

	table.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Rune() {
		case 'q':
			app.Stop()
			return nil
		case 'r', 'R':
			go refreshEndpoints()
			return nil
		case 'd', 'D':
			selectedRow, _ := table.GetSelection()
			if selectedRow > 0 && selectedRow <= len(endpoints) {
				endpoint := endpoints[selectedRow-1]
				ShowEndpointDeleteConfirmation(app, pages, endpoint, refreshEndpoints, runpodPurple, runpodBlue, runpodDarkBg, runpodLightGray)
			}
			return nil
		}

		switch event.Key() {
		case tcell.KeyF5:
			go refreshEndpoints()
			return nil
		}

		return event
	})

	loadingContent.SetInputCapture(CreateBasicInputCapture(app, refreshEndpoints))

	updateContent()

	return mainFlex, refreshEndpoints
}

func ShowEndpointDeleteConfirmation(app *tview.Application, pages *tview.Pages, endpoint *api.Endpoint, refreshEndpoints func(), runpodPurple, runpodBlue, runpodDarkBg, runpodLightGray tcell.Color) {
	modal := tview.NewModal().
		SetText(fmt.Sprintf("Are you sure you want to delete endpoint?\n\nName: %s\nID: %s\nType: %s\nWorkers: %d-%d\n\nThis action cannot be undone!", endpoint.Name, endpoint.Id, endpoint.Type, endpoint.WorkersMin, endpoint.WorkersMax)).
		AddButtons([]string{"Delete", "Cancel"}).
		SetDoneFunc(func(buttonIndex int, buttonLabel string) {
			if buttonLabel == "Delete" {
				deletingModal := tview.NewModal().
					SetText(fmt.Sprintf("🗑️ Deleting endpoint '%s'...\n\nPlease wait...", endpoint.Name)).
					SetBackgroundColor(runpodDarkBg).
					SetTextColor(runpodLightGray)

				pages.AddPage("deleting", deletingModal, true, true)
				pages.SwitchToPage("deleting")

				go func() {
					err := api.DeleteEndpoint(endpoint.Id)
					app.QueueUpdateDraw(func() {
						pages.RemovePage("deleting")
						pages.RemovePage("confirm-delete")
						if err != nil {
							errorModal := tview.NewModal().
								SetText(fmt.Sprintf("❌ Failed to delete endpoint '%s'\n\nError: %s", endpoint.Name, err.Error())).
								AddButtons([]string{"OK"}).
								SetDoneFunc(func(buttonIndex int, buttonLabel string) {
									pages.RemovePage("error-delete")
									pages.SwitchToPage("endpoints")
								})
							errorModal.SetBackgroundColor(runpodDarkBg).
								SetButtonBackgroundColor(runpodBlue).
								SetButtonTextColor(runpodLightGray).
								SetTextColor(tcell.ColorRed)
							pages.AddPage("error-delete", errorModal, true, true)
							pages.SwitchToPage("error-delete")
						} else {
							pages.SwitchToPage("endpoints")
							go refreshEndpoints()
						}
					})
				}()
			} else {
				pages.RemovePage("confirm-delete")
				pages.SwitchToPage("endpoints")
			}
		})

	modal.SetBackgroundColor(runpodDarkBg).
		SetButtonBackgroundColor(runpodBlue).
		SetButtonTextColor(runpodLightGray).
		SetTextColor(runpodLightGray)

	pages.AddPage("confirm-delete", modal, true, true)
	pages.SwitchToPage("confirm-delete")
}
