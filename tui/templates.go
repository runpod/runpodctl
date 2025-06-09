package tui

import (
	"fmt"
	"strconv"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/runpod/runpodctl/api"
)

func CreateTemplatesScreen(app *tview.Application, pages *tview.Pages, runpodPurple, runpodBlue, runpodDarkBg, runpodLightGray tcell.Color) (*tview.Flex, func()) {
	table := CreateBaseTable(runpodBlue)

	selectedBg := tcell.NewRGBColor(20, 10, 60)

	var templates []*api.Template
	var terminalWidth int = 120
	var lastTerminalWidth int = 120

	emptyState := CreateEmptyState("📄 No templates found", "You don't have any templates yet.\n\nTemplates include both your custom templates and public system templates.", "template", "runpodctl create template", runpodDarkBg)

	contentArea := tview.NewFlex()

	loadingContent := CreateLoadingContent("Loading templates...", "Fetching data from API...", runpodDarkBg)

	var updateContent func()
	var showLoading func()
	updateContent = func() {
		contentArea.Clear()
		if len(templates) > 0 {
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
			table.SetCell(0, 2, CreateHeaderCell("Image", runpodPurple))
			table.SetCell(0, 3, CreateHeaderCell("Type", runpodPurple))
			table.SetCell(0, 4, CreateHeaderCell("Owner", runpodPurple))
			table.SetCell(0, 5, CreateHeaderCell("Disk GB", runpodPurple))
			table.SetCell(0, 6, CreateHeaderCell("Volume GB", runpodPurple))
			table.SetCell(0, 7, CreateHeaderCell("SSH", runpodPurple))

			for i, template := range templates {
				row := i + 1

				nameWidth := int(float64(terminalWidth) * 0.20)
				idWidth := int(float64(terminalWidth) * 0.12)
				imageWidth := int(float64(terminalWidth) * 0.30)

				if nameWidth < 8 {
					nameWidth = 8
				}
				if idWidth < 6 {
					idWidth = 6
				}
				if imageWidth < 12 {
					imageWidth = 12
				}

				table.SetCell(row, 0, tview.NewTableCell(" "+FormatColumnText(template.Name, nameWidth-2)+" ").
					SetSelectedStyle(tcell.StyleDefault.Foreground(runpodLightGray).Background(selectedBg)))

				table.SetCell(row, 1, tview.NewTableCell(" "+FormatColumnText(template.Id, idWidth-2)+" ").
					SetSelectedStyle(tcell.StyleDefault.Foreground(runpodLightGray).Background(selectedBg)))

				table.SetCell(row, 2, tview.NewTableCell(" "+FormatColumnText(template.ImageName, imageWidth-2)+" ").
					SetSelectedStyle(tcell.StyleDefault.Foreground(runpodLightGray).Background(selectedBg)))

				templateType := "Pod"
				if template.IsServerless {
					templateType = "Serverless"
				}
				typeColor := runpodLightGray
				if template.IsServerless {
					typeColor = tcell.ColorLimeGreen
				}
				table.SetCell(row, 3, tview.NewTableCell(" "+templateType+" ").
					SetTextColor(typeColor).
					SetSelectedStyle(tcell.StyleDefault.Foreground(typeColor).Background(selectedBg)))

				ownerText := "My"
				ownerColor := tcell.ColorLimeGreen
				if template.IsPublic {
					ownerText = "Public"
					ownerColor = tcell.ColorLightBlue
				}
				table.SetCell(row, 4, tview.NewTableCell(" "+ownerText+" ").
					SetTextColor(ownerColor).
					SetSelectedStyle(tcell.StyleDefault.Foreground(ownerColor).Background(selectedBg)))

				table.SetCell(row, 5, tview.NewTableCell(" "+strconv.Itoa(template.ContainerDiskInGb)+" ").
					SetSelectedStyle(tcell.StyleDefault.Foreground(runpodLightGray).Background(selectedBg)))

				table.SetCell(row, 6, tview.NewTableCell(" "+strconv.Itoa(template.VolumeInGb)+" ").
					SetSelectedStyle(tcell.StyleDefault.Foreground(runpodLightGray).Background(selectedBg)))

				sshText := "No"
				sshColor := tcell.ColorOrangeRed
				if template.StartSSH {
					sshText = "Yes"
					sshColor = tcell.ColorLimeGreen
				}
				table.SetCell(row, 7, tview.NewTableCell(" "+sshText+" ").
					SetTextColor(sshColor).
					SetSelectedStyle(tcell.StyleDefault.Foreground(sshColor).Background(selectedBg)))
			}

			if len(templates) > 0 {
				table.Select(1, 0)
			}
		})
	}

	updateColumnSizing := CreateColumnSizingFunc(&terminalWidth, &lastTerminalWidth, func() bool { return len(templates) > 0 }, repopulateTable)

	table.SetDrawFunc(func(screen tcell.Screen, x int, y int, width int, height int) (int, int, int, int) {
		updateColumnSizing(width)
		return x, y, width, height
	})

	updateColumnSizing(120)

	table.SetCell(0, 0, CreateHeaderCell("Name", runpodPurple))
	table.SetCell(0, 1, CreateHeaderCell("ID", runpodPurple))
	table.SetCell(0, 2, CreateHeaderCell("Image", runpodPurple))
	table.SetCell(0, 3, CreateHeaderCell("Type", runpodPurple))
	table.SetCell(0, 4, CreateHeaderCell("Owner", runpodPurple))
	table.SetCell(0, 5, CreateHeaderCell("Disk GB", runpodPurple))
	table.SetCell(0, 6, CreateHeaderCell("Volume GB", runpodPurple))
	table.SetCell(0, 7, CreateHeaderCell("SSH", runpodPurple))

	refreshTemplates := func() {
		app.QueueUpdateDraw(func() {
			showLoading()
		})

		var err error
		templates, err = api.GetTemplates()
		if err != nil {
			app.QueueUpdateDraw(func() {
				errorContent := tview.NewTextView()
				errorContent.SetDynamicColors(true)
				errorContent.SetBackgroundColor(runpodDarkBg)
				errorContent.SetTextAlign(tview.AlignCenter)
				errorContent.SetText(fmt.Sprintf(`[red]Error Loading Templates[-]

❌ Failed to fetch template data

[#CBCCD2]Error: %s

Press 'r' to retry[-]`, err.Error()))

				errorContent.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
					switch event.Rune() {
					case 'r', 'R':
						app.QueueUpdateDraw(func() {
							showLoading()
						})
						go func() {
							newTemplates, retryErr := api.GetTemplates()
							if retryErr == nil {
								templates = newTemplates
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

	emptyState.SetInputCapture(CreateBasicInputCapture(app, refreshTemplates))

	table.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Rune() {
		case 'q':
			app.Stop()
			return nil
		case 'r', 'R':
			go refreshTemplates()
			return nil
		case 'd', 'D':
			selectedRow, _ := table.GetSelection()
			if selectedRow > 0 && selectedRow <= len(templates) {
				template := templates[selectedRow-1]
				if template.IsPublic {
					errorModal := tview.NewModal().
						SetText("❌ Cannot delete public templates\n\nPublic templates are system templates and cannot be deleted.\nYou can only delete templates that you have created.").
						AddButtons([]string{"OK"}).
						SetDoneFunc(func(buttonIndex int, buttonLabel string) {
							pages.RemovePage("error-public-delete")
							pages.SwitchToPage("templates")
						})
					errorModal.SetBackgroundColor(runpodDarkBg).
						SetButtonBackgroundColor(runpodBlue).
						SetButtonTextColor(runpodLightGray).
						SetTextColor(tcell.ColorRed)
					pages.AddPage("error-public-delete", errorModal, true, true)
					pages.SwitchToPage("error-public-delete")
				} else {
					ShowTemplateDeleteConfirmation(app, pages, template, refreshTemplates, runpodPurple, runpodBlue, runpodDarkBg, runpodLightGray)
				}
			}
			return nil
		}

		switch event.Key() {
		case tcell.KeyF5:
			go refreshTemplates()
			return nil
		}

		return event
	})

	loadingContent.SetInputCapture(CreateBasicInputCapture(app, refreshTemplates))

	updateContent()

	return mainFlex, refreshTemplates
}

func ShowTemplateDeleteConfirmation(app *tview.Application, pages *tview.Pages, template *api.Template, refreshTemplates func(), runpodPurple, runpodBlue, runpodDarkBg, runpodLightGray tcell.Color) {
	templateType := "Pod"
	if template.IsServerless {
		templateType = "Serverless"
	}

	modal := tview.NewModal().
		SetText(fmt.Sprintf("Are you sure you want to delete template?\n\nName: %s\nID: %s\nType: %s\nImage: %s\n\nThis action cannot be undone!", template.Name, template.Id, templateType, template.ImageName)).
		AddButtons([]string{"Delete", "Cancel"}).
		SetDoneFunc(func(buttonIndex int, buttonLabel string) {
			if buttonLabel == "Delete" {
				deletingModal := tview.NewModal().
					SetText(fmt.Sprintf("🗑️ Deleting template '%s'...\n\nPlease wait...", template.Name)).
					SetBackgroundColor(runpodDarkBg).
					SetTextColor(runpodLightGray)

				pages.AddPage("deleting", deletingModal, true, true)
				pages.SwitchToPage("deleting")

				go func() {
					err := api.DeleteTemplate(template.Name)
					app.QueueUpdateDraw(func() {
						pages.RemovePage("deleting")
						pages.RemovePage("confirm-delete")
						if err != nil {
							errorModal := tview.NewModal().
								SetText(fmt.Sprintf("❌ Failed to delete template '%s'\n\nError: %s", template.Name, err.Error())).
								AddButtons([]string{"OK"}).
								SetDoneFunc(func(buttonIndex int, buttonLabel string) {
									pages.RemovePage("error-delete")
									pages.SwitchToPage("templates")
								})
							errorModal.SetBackgroundColor(runpodDarkBg).
								SetButtonBackgroundColor(runpodBlue).
								SetButtonTextColor(runpodLightGray).
								SetTextColor(tcell.ColorRed)
							pages.AddPage("error-delete", errorModal, true, true)
							pages.SwitchToPage("error-delete")
						} else {
							pages.SwitchToPage("templates")
							go refreshTemplates()
						}
					})
				}()
			} else {
				pages.RemovePage("confirm-delete")
				pages.SwitchToPage("templates")
			}
		})

	modal.SetBackgroundColor(runpodDarkBg).
		SetButtonBackgroundColor(runpodBlue).
		SetButtonTextColor(runpodLightGray).
		SetTextColor(runpodLightGray)

	pages.AddPage("confirm-delete", modal, true, true)
	pages.SwitchToPage("confirm-delete")
}
