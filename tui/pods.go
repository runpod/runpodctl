package tui

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/runpod/runpodctl/api"
)

func CreatePodsScreen(app *tview.Application, pages *tview.Pages, runpodPurple, runpodBlue, runpodDarkBg, runpodLightGray tcell.Color) (*tview.Flex, func()) {
	table := CreateBaseTable(runpodBlue)

	selectedBg := tcell.NewRGBColor(20, 10, 60)

	terminalWidth := 120
	lastTerminalWidth := 120

	var pods []*api.Pod

	emptyState := CreateEmptyState("🚀 No pods found", "You don't have any pods yet.", "pod", "runpodctl create pod", runpodDarkBg)

	contentArea := tview.NewFlex()

	loadingContent := CreateLoadingContent("Loading pods...", "Fetching data from API...", runpodDarkBg)

	var updateContent func()
	var showLoading func()
	updateContent = func() {
		contentArea.Clear()
		if len(pods) > 0 {
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
			table.SetCell(0, 2, CreateHeaderCell("Status", runpodPurple))
			table.SetCell(0, 3, CreateHeaderCell("CPU%", runpodPurple))
			table.SetCell(0, 4, CreateHeaderCell("Memory%", runpodPurple))
			table.SetCell(0, 5, CreateHeaderCell("GPU%", runpodPurple))
			table.SetCell(0, 6, CreateHeaderCell("Uptime", runpodPurple))
			table.SetCell(0, 7, CreateHeaderCell("Cost/Hr", runpodPurple))
			table.SetCell(0, 8, CreateHeaderCell("Location", runpodPurple))

			for i, pod := range pods {
				row := i + 1

				nameWidth := int(float64(terminalWidth) * 0.22)
				idWidth := int(float64(terminalWidth) * 0.12)
				locationWidth := int(float64(terminalWidth) * 0.15)

				if nameWidth < 8 {
					nameWidth = 8
				}
				if idWidth < 6 {
					idWidth = 6
				}
				if locationWidth < 8 {
					locationWidth = 8
				}

				table.SetCell(row, 0, tview.NewTableCell(" "+FormatColumnText(pod.Name, nameWidth-2)+" ").
					SetSelectedStyle(tcell.StyleDefault.Foreground(runpodLightGray).Background(selectedBg)))

				table.SetCell(row, 1, tview.NewTableCell(" "+FormatColumnText(pod.Id, idWidth-2)+" ").
					SetSelectedStyle(tcell.StyleDefault.Foreground(runpodLightGray).Background(selectedBg)))

				statusColor := runpodLightGray
				switch pod.DesiredStatus {
				case "RUNNING":
					statusColor = tcell.ColorLimeGreen
				case "STOPPED":
					statusColor = tcell.ColorOrangeRed
				}
				table.SetCell(row, 2, tview.NewTableCell(" "+pod.DesiredStatus+" ").
					SetTextColor(statusColor).
					SetSelectedStyle(tcell.StyleDefault.Foreground(statusColor).Background(selectedBg)))

				cpuUsage := "N/A"
				cpuColor := runpodLightGray
				if pod.Runtime != nil && pod.Runtime.Container != nil {
					cpuPercent := pod.Runtime.Container.CpuPercent
					cpuUsage = fmt.Sprintf("%.1f%%", cpuPercent)
					if cpuPercent > 80 {
						cpuColor = tcell.ColorOrangeRed
					} else if cpuPercent > 50 {
						cpuColor = tcell.ColorYellow
					} else {
						cpuColor = tcell.ColorLimeGreen
					}
				}
				table.SetCell(row, 3, tview.NewTableCell(" "+cpuUsage+" ").
					SetTextColor(cpuColor).
					SetSelectedStyle(tcell.StyleDefault.Foreground(cpuColor).Background(selectedBg)))

				memUsage := "N/A"
				memColor := runpodLightGray
				if pod.Runtime != nil && pod.Runtime.Container != nil {
					memPercent := pod.Runtime.Container.MemoryPercent
					memUsage = fmt.Sprintf("%.1f%%", memPercent)
					if memPercent > 80 {
						memColor = tcell.ColorOrangeRed
					} else if memPercent > 50 {
						memColor = tcell.ColorYellow
					} else {
						memColor = tcell.ColorLimeGreen
					}
				}
				table.SetCell(row, 4, tview.NewTableCell(" "+memUsage+" ").
					SetTextColor(memColor).
					SetSelectedStyle(tcell.StyleDefault.Foreground(memColor).Background(selectedBg)))

				gpuUsage := "N/A"
				gpuColor := runpodLightGray
				if pod.Runtime != nil && pod.Runtime.Gpus != nil && len(pod.Runtime.Gpus) > 0 {
					var totalGpuUtil float32
					for _, gpu := range pod.Runtime.Gpus {
						totalGpuUtil += gpu.GpuUtilPercent
					}
					avgGpuUtil := totalGpuUtil / float32(len(pod.Runtime.Gpus))
					gpuUsage = fmt.Sprintf("%.1f%%", avgGpuUtil)
					if avgGpuUtil > 80 {
						gpuColor = tcell.ColorOrangeRed
					} else if avgGpuUtil > 50 {
						gpuColor = tcell.ColorYellow
					} else {
						gpuColor = tcell.ColorLimeGreen
					}
				}
				table.SetCell(row, 5, tview.NewTableCell(" "+gpuUsage+" ").
					SetTextColor(gpuColor).
					SetSelectedStyle(tcell.StyleDefault.Foreground(gpuColor).Background(selectedBg)))

				uptime := "N/A"
				uptimeSeconds := pod.UptimeSeconds
				if pod.Runtime != nil && pod.Runtime.UptimeInSeconds > 0 {
					uptimeSeconds = pod.Runtime.UptimeInSeconds
				}
				if uptimeSeconds > 0 {
					hours := uptimeSeconds / 3600
					minutes := (uptimeSeconds % 3600) / 60
					if hours > 0 {
						uptime = fmt.Sprintf("%dh %dm", hours, minutes)
					} else if minutes > 0 {
						uptime = fmt.Sprintf("%dm", minutes)
					} else {
						uptime = fmt.Sprintf("%ds", uptimeSeconds)
					}
				}
				table.SetCell(row, 6, tview.NewTableCell(" "+uptime+" ").
					SetSelectedStyle(tcell.StyleDefault.Foreground(runpodLightGray).Background(selectedBg)))

				table.SetCell(row, 7, tview.NewTableCell(" $"+strconv.FormatFloat(float64(pod.CostPerHr), 'f', 2, 32)+" ").
					SetSelectedStyle(tcell.StyleDefault.Foreground(runpodLightGray).Background(selectedBg)))

				location := "Unknown"
				if pod.Machine != nil && pod.Machine.Location != "" {
					location = pod.Machine.Location
				}
				table.SetCell(row, 8, tview.NewTableCell(" "+FormatColumnText(location, locationWidth-2)+" ").
					SetSelectedStyle(tcell.StyleDefault.Foreground(runpodLightGray).Background(selectedBg)))
			}

			if len(pods) > 0 {
				table.Select(1, 0)
			}
		})
	}

	updateColumnSizing := CreateColumnSizingFunc(&terminalWidth, &lastTerminalWidth, func() bool { return len(pods) > 0 }, repopulateTable)

	table.SetCell(0, 0, CreateHeaderCell("Name", runpodPurple))
	table.SetCell(0, 1, CreateHeaderCell("ID", runpodPurple))
	table.SetCell(0, 2, CreateHeaderCell("Status", runpodPurple))
	table.SetCell(0, 3, CreateHeaderCell("CPU%", runpodPurple))
	table.SetCell(0, 4, CreateHeaderCell("Memory%", runpodPurple))
	table.SetCell(0, 5, CreateHeaderCell("GPU%", runpodPurple))
	table.SetCell(0, 6, CreateHeaderCell("Uptime", runpodPurple))
	table.SetCell(0, 7, CreateHeaderCell("Cost/Hr", runpodPurple))
	table.SetCell(0, 8, CreateHeaderCell("Location", runpodPurple))

	refreshPods := func() {
		app.QueueUpdateDraw(func() {
			showLoading()
		})

		var err error
		pods, err = api.GetPods()
		if err != nil {
			app.QueueUpdateDraw(func() {
				errorContent := tview.NewTextView()
				errorContent.SetDynamicColors(true)
				errorContent.SetBackgroundColor(runpodDarkBg)
				errorContent.SetTextAlign(tview.AlignCenter)
				errorContent.SetText(fmt.Sprintf(`[red]Error Loading Pods[-]

❌ Failed to fetch pod data

[#CBCCD2]Error: %s

Press 'r' to retry[-]`, err.Error()))

				errorContent.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
					switch event.Rune() {
					case 'r', 'R':
						app.QueueUpdateDraw(func() {
							showLoading()
						})
						go func() {
							newPods, retryErr := api.GetPods()
							if retryErr == nil {
								pods = newPods
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

	table.SetDrawFunc(func(screen tcell.Screen, x int, y int, width int, height int) (int, int, int, int) {
		updateColumnSizing(width)
		return x, y, width, height
	})

	updateColumnSizing(120)

	statusBar := CreateStatusBar("[#6134E2]Enter[-] - Details | [#6134E2]h[-] - SSH | [#6134E2]s[-] - Stop | [#6134E2]t[-] - Start | [#6134E2]d[-] - Delete | [#6134E2]r/F5[-] - Refresh", runpodDarkBg)

	mainFlex := tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(contentArea, 0, 1, true).
		AddItem(statusBar, 1, 0, false)

	emptyState.SetInputCapture(CreateBasicInputCapture(app, refreshPods))

	loadingContent.SetInputCapture(CreateBasicInputCapture(app, refreshPods))

	table.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Rune() {
		case 'q':
			app.Stop()
			return nil
		case 'r', 'R':
			go refreshPods()
			return nil
		case 's', 'S':
			selectedRow, _ := table.GetSelection()
			if selectedRow > 0 && selectedRow <= len(pods) {
				pod := pods[selectedRow-1]
				go func() {
					_, err := api.StopPod(pod.Id)
					if err == nil {
						time.Sleep(1 * time.Second)
						go refreshPods()
					}
				}()
			}
			return nil
		case 'd', 'D':
			selectedRow, _ := table.GetSelection()
			if selectedRow > 0 && selectedRow <= len(pods) {
				pod := pods[selectedRow-1]
				ShowDeleteConfirmation(app, pages, pod, refreshPods, runpodPurple, runpodBlue, runpodDarkBg, runpodLightGray)
			}
			return nil
		case 't', 'T':
			selectedRow, _ := table.GetSelection()
			if selectedRow > 0 && selectedRow <= len(pods) {
				pod := pods[selectedRow-1]
				go func() {
					_, err := api.StartOnDemandPod(pod.Id)
					if err == nil {
						time.Sleep(1 * time.Second)
						go refreshPods()
					}
				}()
			}
			return nil
		case 'h', 'H':
			selectedRow, _ := table.GetSelection()
			if selectedRow > 0 && selectedRow <= len(pods) {
				pod := pods[selectedRow-1]
				sshCommand := getSSHConnectionInfo(pod)
				if sshCommand != "" {
					app.Suspend(func() {
						connectToSSH(sshCommand)
					})
				}
			}
			return nil
		}

		switch event.Key() {
		case tcell.KeyEnter:
			selectedRow, _ := table.GetSelection()
			if selectedRow > 0 && selectedRow <= len(pods) {
				pod := pods[selectedRow-1]
				ShowPodDetails(app, pages, pod, runpodPurple, runpodBlue, runpodDarkBg, runpodLightGray)
			}
			return nil
		case tcell.KeyF5:
			go refreshPods()
			return nil
		}

		return event
	})

	updateContent()

	go refreshPods()

	return mainFlex, refreshPods
}

func ShowDeleteConfirmation(app *tview.Application, pages *tview.Pages, pod *api.Pod, refreshPods func(), runpodPurple, runpodBlue, runpodDarkBg, runpodLightGray tcell.Color) {
	modal := tview.NewModal().
		SetText(fmt.Sprintf("Are you sure you want to delete pod?\n\nName: %s\nID: %s\nStatus: %s\n\nThis action cannot be undone!", pod.Name, pod.Id, pod.DesiredStatus)).
		AddButtons([]string{"Delete", "Cancel"}).
		SetDoneFunc(func(buttonIndex int, buttonLabel string) {
			if buttonLabel == "Delete" {
				deletingModal := tview.NewModal().
					SetText(fmt.Sprintf("🗑️ Deleting pod '%s'...\n\nPlease wait...", pod.Name)).
					SetBackgroundColor(runpodDarkBg).
					SetTextColor(runpodLightGray)

				pages.AddPage("deleting", deletingModal, true, true)
				pages.SwitchToPage("deleting")

				go func() {
					_, err := api.RemovePod(pod.Id)
					app.QueueUpdateDraw(func() {
						pages.RemovePage("deleting")
						pages.RemovePage("confirm-delete")
						if err != nil {
							errorModal := tview.NewModal().
								SetText(fmt.Sprintf("❌ Failed to delete pod '%s'\n\nError: %s", pod.Name, err.Error())).
								AddButtons([]string{"OK"}).
								SetDoneFunc(func(buttonIndex int, buttonLabel string) {
									pages.RemovePage("error-delete")
									pages.SwitchToPage("pods")
								})
							errorModal.SetBackgroundColor(runpodDarkBg).
								SetButtonBackgroundColor(runpodBlue).
								SetButtonTextColor(runpodLightGray).
								SetTextColor(tcell.ColorRed)
							pages.AddPage("error-delete", errorModal, true, true)
							pages.SwitchToPage("error-delete")
						} else {
							pages.SwitchToPage("pods")
							time.Sleep(500 * time.Millisecond)
							go refreshPods()
						}
					})
				}()
			} else {
				pages.RemovePage("confirm-delete")
				pages.SwitchToPage("pods")
			}
		})

	modal.SetBackgroundColor(runpodDarkBg).
		SetButtonBackgroundColor(runpodBlue).
		SetButtonTextColor(runpodLightGray).
		SetTextColor(runpodLightGray)

	pages.AddPage("confirm-delete", modal, true, true)
	pages.SwitchToPage("confirm-delete")
}

func ShowPodDetails(app *tview.Application, pages *tview.Pages, pod *api.Pod, runpodPurple, runpodBlue, runpodDarkBg, runpodLightGray tcell.Color) {
	detailsView := tview.NewTextView()
	detailsView.SetDynamicColors(true)
	detailsView.SetBackgroundColor(runpodDarkBg)
	detailsView.SetBorder(true)
	detailsView.SetTitle(fmt.Sprintf(" Pod Details: %s ", pod.Name))
	detailsView.SetTitleColor(runpodPurple)
	detailsView.SetBorderColor(runpodBlue)

	details := fmt.Sprintf(`[#6134E2]Name:[-] %s
[#6134E2]ID:[-] %s
[#6134E2]Status:[-] %s
[#6134E2]Cost per Hour:[-] $%.2f
[#6134E2]Location:[-] %s

[#6134E2]== Runtime Information ==[white]
`, pod.Name, pod.Id, pod.DesiredStatus, pod.CostPerHr, func() string {
		if pod.Machine != nil && pod.Machine.Location != "" {
			return pod.Machine.Location
		}
		return "Unknown"
	}())

	if pod.Runtime != nil {
		details += fmt.Sprintf(`[#6134E2]Uptime:[-] %s
`, func() string {
			uptimeSeconds := pod.Runtime.UptimeInSeconds
			if uptimeSeconds > 0 {
				hours := uptimeSeconds / 3600
				minutes := (uptimeSeconds % 3600) / 60
				if hours > 0 {
					return fmt.Sprintf("%dh %dm", hours, minutes)
				} else if minutes > 0 {
					return fmt.Sprintf("%dm", minutes)
				} else {
					return fmt.Sprintf("%ds", uptimeSeconds)
				}
			}
			return "N/A"
		}())

		if pod.Runtime.Container != nil {
			details += fmt.Sprintf(`[#6134E2]CPU Usage:[-] %.1f%%
[#6134E2]Memory Usage:[-] %.1f%%
`, pod.Runtime.Container.CpuPercent, pod.Runtime.Container.MemoryPercent)
		}

		if len(pod.Runtime.Gpus) > 0 {
			details += "\n[#6134E2]== GPU Information ==[white]\n"
			for i, gpu := range pod.Runtime.Gpus {
				details += fmt.Sprintf(`[#6134E2]GPU %d:[-] %.1f%% utilization
`, i+1, gpu.GpuUtilPercent)
			}
		}
	} else {
		details += "Runtime information not available\n"
	}

	details += fmt.Sprintf(`
[#6134E2]== Machine Information ==[white]
[#6134E2]Pod Type:[-] %s
[#6134E2]vCPUs:[-] %d
[#6134E2]Memory:[-] %d GB
[#6134E2]Disk:[-] %d GB
`, pod.PodType, pod.VcpuCount, pod.MemoryInGb, pod.ContainerDiskInGb)

	if pod.GpuCount > 0 {
		gpuType := "Unknown"
		if pod.Machine != nil && pod.Machine.GpuDisplayName != "" {
			gpuType = pod.Machine.GpuDisplayName
		}
		details += fmt.Sprintf(`[#6134E2]GPU Count:[-] %d
[#6134E2]GPU Type:[-] %s
`, pod.GpuCount, gpuType)
	}

	sshInfo := getSSHConnectionInfo(pod)
	if sshInfo != "" {
		details += fmt.Sprintf(`
[#6134E2]== SSH Connection ==[white]
[#6134E2]Command:[-] %s
`, sshInfo)
	}

	details += "\n[#CBCCD2]Press ESC or 'q' to go back | Press 's' to SSH (if available)[-]"

	detailsView.SetText(details)

	detailsView.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyEscape:
			pages.RemovePage("pod-details")
			pages.SwitchToPage("pods")
			return nil
		}
		switch event.Rune() {
		case 'q', 'Q':
			pages.RemovePage("pod-details")
			pages.SwitchToPage("pods")
			return nil
		case 's', 'S':
			sshCommand := getSSHConnectionInfo(pod)
			if sshCommand != "" {
				app.Suspend(func() {
					connectToSSH(sshCommand)
				})
			}
			return nil
		}
		return event
	})

	pages.AddPage("pod-details", detailsView, true, true)
	pages.SwitchToPage("pod-details")
}

func getSSHConnectionInfo(pod *api.Pod) string {
	if pod.Runtime == nil || pod.Runtime.Ports == nil {
		return ""
	}
	for _, port := range pod.Runtime.Ports {
		if port.IsIpPublic && port.PrivatePort == 22 {
			return fmt.Sprintf("ssh root@%s -p %d", port.Ip, port.PublicPort)
		}
	}
	return ""
}

func connectToSSH(sshCommand string) {
	fmt.Printf("Connecting via SSH...\n%s\n\n", sshCommand)

	args := strings.Fields(sshCommand)[1:] // Remove "ssh" from the beginning

	cmd := exec.Command("ssh", args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		fmt.Printf("SSH connection failed: %v\n", err)
	}

	fmt.Println("Press Enter to return to TUI...")
	_, _ = fmt.Scanln()
}
