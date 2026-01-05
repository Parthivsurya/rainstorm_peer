package main

import (
	"sync"

	"fyne.io/fyne/v2/data/binding"
)

type TransferStatus string

const (
	StatusPending    TransferStatus = "Pending"
	StatusInProgress TransferStatus = "In Progress"
	StatusDone       TransferStatus = "Done"
	StatusFailed     TransferStatus = "Failed"
)

type TransferItem struct {
	ID       string
	FileName string
	Type     string // "Push" or "Pull"
	Status   TransferStatus
}

type TransferManager struct {
	Items     []TransferItem
	Data      binding.ExternalStringList
	Listeners map[string]func()
	mutex     sync.Mutex
}

var GlobalTransferManager *TransferManager

func InitTransferManager() {
	GlobalTransferManager = &TransferManager{
		Items:     make([]TransferItem, 0),
		Listeners: make(map[string]func()),
	}
	// We will use a custom mechanism or just refresh the table manually
	// binding.NewExternalStringList is simple, but we have a struct.
	// For simplicity in Fyne tables, we'll access Items directly and call Refresh() on the table widget.
}

func (tm *TransferManager) AddTransfer(id, filename, ttype string) int {
	tm.mutex.Lock()
	defer tm.mutex.Unlock()

	item := TransferItem{
		ID:       id,
		FileName: filename,
		Type:     ttype,
		Status:   StatusPending,
	}
	tm.Items = append(tm.Items, item)
	return len(tm.Items) - 1 // Return index
}

func (tm *TransferManager) UpdateStatus(index int, status TransferStatus) {
	tm.mutex.Lock()
	defer tm.mutex.Unlock()

	if index >= 0 && index < len(tm.Items) {
		tm.Items[index].Status = status
		// Notify listeners (UI refresh)
		if refresh, ok := tm.Listeners["refresh_table"]; ok {
			refresh()
		}
	}
}

func (tm *TransferManager) RegisterListener(name string, callback func()) {
	tm.mutex.Lock()
	defer tm.mutex.Unlock()
	tm.Listeners[name] = callback
}
