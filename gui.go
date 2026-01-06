package main

import (
	"fmt"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"
)

var logArea *widget.Entry

func StartGUI(chunker *Chunker, savePath string) {
	InitTransferManager()
	a := app.New()
	w := a.NewWindow("Rainstorm Peer")
	w.Resize(fyne.NewSize(600, 400))

	// Defines tabs
	pushTab := createPushTab(w, chunker)
	pullTab := createPullTab(w, chunker)
	transfersTab := createTransfersTab()
	logTab := createLogTab()

	tabs := container.NewAppTabs(
		container.NewTabItem("Push", pushTab),
		container.NewTabItem("Pull", pullTab),
		container.NewTabItem("Transfers", transfersTab),
		container.NewTabItem("Logs", logTab),
	)

	w.SetContent(tabs)
	w.ShowAndRun()
}

func logMessage(msg string) {
	if logArea != nil {
		logArea.SetText(logArea.Text + msg + "\n")
		// Auto scroll could be simulated by cursor position but Entry widget handles it reasonably
		logArea.Refresh()
	}
	fmt.Println(msg)
}

func createPushTab(w fyne.Window, chunker *Chunker) fyne.CanvasObject {
	filePathEntry := widget.NewEntry()
	filePathEntry.SetPlaceHolder("Path to file")

	fidEntry := widget.NewEntry()
	fidEntry.SetPlaceHolder("File ID (FID)")

	fnameEntry := widget.NewEntry()
	fnameEntry.SetPlaceHolder("Target Filename")

	trackerIPEntry := widget.NewEntry()
	trackerIPEntry.SetPlaceHolder("Tracker IP")

	fileButton := widget.NewButton("Choose File", func() {
		fd := dialog.NewFileOpen(func(reader fyne.URIReadCloser, err error) {
			if err != nil {
				dialog.ShowError(err, w)
				return
			}
			if reader == nil {
				return
			}
			filePathEntry.SetText(reader.URI().Path())
		}, w)
		fd.Show()
	})

	pushButton := widget.NewButton("Push File", func() {
		localPath := filePathEntry.Text
		fid := fidEntry.Text
		fname := fnameEntry.Text
		trackerIP := trackerIPEntry.Text

		if localPath == "" || fid == "" || fname == "" || trackerIP == "" {
			dialog.ShowError(fmt.Errorf("please fill all fields"), w)
			return
		}

		idx := GlobalTransferManager.AddTransfer(fid, fname, "Push")
		GlobalTransferManager.UpdateStatus(idx, StatusInProgress)

		go func() {
			logMessage(fmt.Sprintf("Pushing file: %s", fname))
			err := PushHandler(localPath, fid, fname, trackerIP, chunker)
			if err != nil {
				logMessage(fmt.Sprintf("Error pushing: %s", err.Error()))
				GlobalTransferManager.UpdateStatus(idx, StatusFailed)
			} else {
				logMessage("Push successful!")
				GlobalTransferManager.UpdateStatus(idx, StatusDone)
			}
		}()
	})

	return container.NewVBox(
		widget.NewLabel("Push File"),
		container.New(layout.NewFormLayout(), widget.NewLabel("File:"), container.NewBorder(nil, nil, nil, fileButton, filePathEntry)),
		widget.NewForm(
			widget.NewFormItem("File ID", fidEntry),
			widget.NewFormItem("Target Name", fnameEntry),
			widget.NewFormItem("Tracker IP", trackerIPEntry),
		),
		pushButton,
	)
}

func createPullTab(w fyne.Window, chunker *Chunker) fyne.CanvasObject {
	fidEntry := widget.NewEntry()
	fidEntry.SetPlaceHolder("File ID (FID)")

	trackerIPEntry := widget.NewEntry()
	trackerIPEntry.SetPlaceHolder("Tracker IP")

	savePathEntry := widget.NewEntry()
	savePathEntry.SetPlaceHolder("Save Path (Local filename)")

	// Button to choose save location
	saveButton := widget.NewButton("Choose Save Location", func() {
		fd := dialog.NewFileSave(func(writer fyne.URIWriteCloser, err error) {
			if err != nil {
				dialog.ShowError(err, w)
				return
			}
			if writer == nil {
				return
			}
			savePathEntry.SetText(writer.URI().Path())
		}, w)
		fd.Show()
	})

	pullButton := widget.NewButton("Pull File", func() {
		fid := fidEntry.Text
		trackerIP := trackerIPEntry.Text
		localPath := savePathEntry.Text

		if fid == "" || trackerIP == "" || localPath == "" {
			dialog.ShowError(fmt.Errorf("please fill all fields"), w)
			return
		}

		idx := GlobalTransferManager.AddTransfer(fid, localPath, "Pull")
		GlobalTransferManager.UpdateStatus(idx, StatusInProgress)

		go func() {
			logMessage(fmt.Sprintf("Pulling file ID: %s", fid))
			PullHandler(localPath, fid, trackerIP, chunker, func(completed int, total int, err error) {
				if err != nil {
					logMessage(fmt.Sprintf("Pull failed: %v", err))
					GlobalTransferManager.UpdateStatus(idx, StatusFailed)
				} else {
					if completed == total && total > 0 {
						logMessage(fmt.Sprintf("Pull successful: %s", fid))
						GlobalTransferManager.UpdateStatus(idx, StatusDone)
						GlobalTransferManager.UpdateProgress(idx, 100.0)
					} else if total > 0 {
						progress := float64(completed) / float64(total) * 100.0
						GlobalTransferManager.UpdateProgress(idx, progress)
						// logMessage(fmt.Sprintf("Progress: %.1f%%", progress)) // Optional: don't spam logs
					}
				}
			})
			logMessage("Pull request sent (check logs/console for details)")
		}()
	})

	return container.NewVBox(
		widget.NewLabel("Pull File"),
		widget.NewForm(
			widget.NewFormItem("File ID", fidEntry),
			widget.NewFormItem("Tracker IP", trackerIPEntry),
		),
		container.New(layout.NewFormLayout(), widget.NewLabel("Save As:"), container.NewBorder(nil, nil, nil, saveButton, savePathEntry)),
		pullButton,
	)
}

func createTransfersTab() fyne.CanvasObject {
	table := widget.NewTable(
		func() (int, int) {
			return len(GlobalTransferManager.Items), 5 // Rows, Cols
		},
		func() fyne.CanvasObject {
			return widget.NewLabel("Cell Content") // Template
		},
		func(i widget.TableCellID, o fyne.CanvasObject) {
			if i.Row >= len(GlobalTransferManager.Items) {
				return
			}
			item := GlobalTransferManager.Items[i.Row]
			label := o.(*widget.Label)
			switch i.Col {
			case 0:
				label.SetText(item.Type)
			case 1:
				label.SetText(item.ID)
			case 2:
				label.SetText(item.FileName)
			case 3:
				label.SetText(string(item.Status))
			case 4:
				label.SetText(fmt.Sprintf("%.1f%%", item.Progress))
			}
		})

	// Configure Headers
	table.ShowHeaderRow = true
	table.CreateHeader = func() fyne.CanvasObject {
		return widget.NewLabelWithStyle("Header", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	}
	table.UpdateHeader = func(id widget.TableCellID, o fyne.CanvasObject) {
		label := o.(*widget.Label)
		switch id.Col {
		case 0:
			label.SetText("Type")
		case 1:
			label.SetText("ID")
		case 2:
			label.SetText("File Name")
		case 3:
			label.SetText("Status")
		case 4:
			label.SetText("Progress")
		}
	}

	table.SetColumnWidth(0, 50)
	table.SetColumnWidth(1, 150)
	table.SetColumnWidth(2, 200)
	table.SetColumnWidth(3, 100)
	table.SetColumnWidth(4, 80)

	GlobalTransferManager.RegisterListener("refresh_table", func() {
		table.Refresh()
	})

	return container.NewMax(table)
}

func createLogTab() fyne.CanvasObject {
	logArea = widget.NewMultiLineEntry()
	logArea.Disable() // Read-only
	return container.NewMax(logArea)
}
