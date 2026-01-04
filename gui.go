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
	a := app.New()
	w := a.NewWindow("Rainstorm Peer")
	w.Resize(fyne.NewSize(600, 400))

	// Defines tabs
	pushTab := createPushTab(w, chunker)
	pullTab := createPullTab(w, chunker)
	logTab := createLogTab()

	tabs := container.NewAppTabs(
		container.NewTabItem("Push", pushTab),
		container.NewTabItem("Pull", pullTab),
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

		go func() {
			logMessage(fmt.Sprintf("Pushing file: %s", fname))
			err := PushHandler(localPath, fid, fname, trackerIP, chunker)
			if err != nil {
				logMessage(fmt.Sprintf("Error pushing: %s", err.Error()))
				// We can't easily show dialog from goroutine without careful sync, 
				// but Fyne is thread-safe for many things.
			} else {
				logMessage("Push successful!")
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

		go func() {
			logMessage(fmt.Sprintf("Pulling file ID: %s", fid))
			PullHandler(localPath, fid, trackerIP, chunker)
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

func createLogTab() fyne.CanvasObject {
	logArea = widget.NewMultiLineEntry()
	logArea.Disable() // Read-only
	return container.NewMax(logArea)
}
