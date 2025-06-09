package tui

import (
	"fmt"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// Common text formatting
func FormatColumnText(text string, maxWidth int) string {
	if len(text) <= maxWidth {
		return text
	}
	if maxWidth <= 3 {
		return text[:maxWidth]
	}
	return text[:maxWidth-3] + "..."
}

// Common math utility
func Abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// Common table configuration
func CreateBaseTable(runpodBlue tcell.Color) *tview.Table {
	return tview.NewTable().
		SetBorders(true).
		SetSelectable(true, false).
		SetFixed(1, 0).
		SetBordersColor(runpodBlue).
		SetSeparator(tview.Borders.Vertical).
		SetEvaluateAllRows(true)
}

// Common loading screen
func CreateLoadingContent(title, message string, runpodDarkBg tcell.Color) *tview.TextView {
	loading := tview.NewTextView()
	loading.SetDynamicColors(true)
	loading.SetBackgroundColor(runpodDarkBg)
	loading.SetTextAlign(tview.AlignCenter)
	loading.SetText(fmt.Sprintf(`[#824edc]%s[-]

⣾ %s

[#CBCCD2]Please wait while we retrieve your data[-]`, title, message))
	return loading
}

// Common empty state
func CreateEmptyState(title, subtitle, resourceType, createCommand string, runpodDarkBg tcell.Color) *tview.TextView {
	empty := tview.NewTextView()
	empty.SetDynamicColors(true)
	empty.SetBackgroundColor(runpodDarkBg)
	empty.SetTextAlign(tview.AlignCenter)
	empty.SetText(fmt.Sprintf(`[#824edc]%s[-]

[#CBCCD2]%s[-]

[#6134E2]Create your first %s with:[-]
[#824edc]%s[-]

[#666666]Press 'r' to refresh or 'q' to quit[-]`, title, subtitle, resourceType, createCommand))
	return empty
}

// Common error content
func CreateErrorContent(title, errorMsg string, runpodDarkBg tcell.Color) *tview.TextView {
	errorContent := tview.NewTextView()
	errorContent.SetDynamicColors(true)
	errorContent.SetBackgroundColor(runpodDarkBg)
	errorContent.SetTextAlign(tview.AlignCenter)
	errorContent.SetText(fmt.Sprintf(`[red]%s[-]

❌ Failed to fetch data

[#CBCCD2]Error: %s

Press 'r' to retry[-]`, title, errorMsg))
	return errorContent
}

// Common status bar
func CreateStatusBar(commands string, runpodDarkBg tcell.Color) *tview.TextView {
	statusBar := tview.NewTextView()
	statusBar.SetDynamicColors(true)
	statusBar.SetBackgroundColor(runpodDarkBg)
	statusBar.SetText(fmt.Sprintf("[#824edc]Commands:[-] %s | [#6134E2]1,2,3[-] - Switch Screens | [#6134E2]q[-] - Quit", commands))
	return statusBar
}

// Common terminal width update logic
func CreateColumnSizingFunc(terminalWidth, lastTerminalWidth *int, hasData func() bool, repopulateTable func()) func(int) {
	return func(newWidth int) {
		if newWidth > 0 {
			*terminalWidth = newWidth - 4
			if Abs(*terminalWidth-*lastTerminalWidth) > 10 && hasData() {
				*lastTerminalWidth = *terminalWidth
				go func() {
					time.Sleep(100 * time.Millisecond)
					repopulateTable()
				}()
			} else {
				*lastTerminalWidth = *terminalWidth
			}
		}
	}
}

// Common input capture for basic commands
func CreateBasicInputCapture(app *tview.Application, refreshFunc func()) func(*tcell.EventKey) *tcell.EventKey {
	return func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Rune() {
		case 'q':
			app.Stop()
			return nil
		case 'r', 'R':
			go refreshFunc()
			return nil
		}
		switch event.Key() {
		case tcell.KeyF5:
			go refreshFunc()
			return nil
		}
		return event
	}
}

// Common delete confirmation modal
func CreateDeleteModal(app *tview.Application, pages *tview.Pages,
	title, details string,
	deleteFunc func() error,
	onComplete func(),
	runpodPurple, runpodBlue, runpodDarkBg, runpodLightGray tcell.Color,
	pageName string) {

	modal := tview.NewModal().
		SetText(fmt.Sprintf("Are you sure you want to delete %s?\n\n%s\n\nThis action cannot be undone!", title, details)).
		AddButtons([]string{"Delete", "Cancel"}).
		SetDoneFunc(func(buttonIndex int, buttonLabel string) {
			if buttonLabel == "Delete" {
				deletingModal := tview.NewModal().
					SetText(fmt.Sprintf("🗑️ Deleting %s...\n\nPlease wait...", title)).
					SetBackgroundColor(runpodDarkBg).
					SetTextColor(runpodLightGray)

				pages.AddPage("deleting", deletingModal, true, true)
				pages.SwitchToPage("deleting")

				go func() {
					err := deleteFunc()
					app.QueueUpdateDraw(func() {
						pages.RemovePage("deleting")
						pages.RemovePage("confirm-delete")
						if err != nil {
							errorModal := tview.NewModal().
								SetText(fmt.Sprintf("❌ Failed to delete %s\n\nError: %s", title, err.Error())).
								AddButtons([]string{"OK"}).
								SetDoneFunc(func(buttonIndex int, buttonLabel string) {
									pages.RemovePage("error-delete")
									pages.SwitchToPage(pageName)
								})
							errorModal.SetBackgroundColor(runpodDarkBg).
								SetButtonBackgroundColor(runpodBlue).
								SetButtonTextColor(runpodLightGray).
								SetTextColor(tcell.ColorRed)
							pages.AddPage("error-delete", errorModal, true, true)
							pages.SwitchToPage("error-delete")
						} else {
							pages.SwitchToPage(pageName)
							onComplete()
						}
					})
				}()
			} else {
				pages.RemovePage("confirm-delete")
				pages.SwitchToPage(pageName)
			}
		})

	modal.SetBackgroundColor(runpodDarkBg).
		SetButtonBackgroundColor(runpodBlue).
		SetButtonTextColor(runpodLightGray).
		SetTextColor(runpodLightGray)

	pages.AddPage("confirm-delete", modal, true, true)
	pages.SwitchToPage("confirm-delete")
}

// Common table header cell
func CreateHeaderCell(text string, runpodPurple tcell.Color) *tview.TableCell {
	return tview.NewTableCell(" " + text + " ").
		SetTextColor(runpodPurple).
		SetAttributes(tcell.AttrBold).
		SetSelectable(false)
}

// Common data cell with selection styling
func CreateDataCell(text string, textColor tcell.Color, runpodLightGray tcell.Color, selectedBg tcell.Color) *tview.TableCell {
	return tview.NewTableCell(" " + text + " ").
		SetTextColor(textColor).
		SetSelectedStyle(tcell.StyleDefault.Foreground(textColor).Background(selectedBg))
}
