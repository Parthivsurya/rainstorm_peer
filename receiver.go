package main

import (
	"bufio"
	"context"
	"encoding/binary"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	common "github.com/aadit-n3rdy/rainstorm_common"
	"github.com/google/uuid"

	"net"
	"sync"

	"github.com/quic-go/quic-go"
)

const (
	RECV_DONE = iota
	RECV_FAIL
)

var PeerIPBlackList map[string]interface{}
var PeerBlackListMut sync.Mutex

var RecvFiles map[string][]string
var RecvFilesMut sync.Mutex

func ReceiverInit() {
	PeerIPBlackList = make(map[string]interface{})
	RecvFiles = make(map[string][]string)
}

func IsPeerBlackListed(peerIP string) bool {
	PeerBlackListMut.Lock()
	_, isblack := PeerIPBlackList[peerIP]
	PeerBlackListMut.Unlock()
	return isblack
}

func AddPeerToBlackList(peerIP string) {
	PeerBlackListMut.Lock()
	defer PeerBlackListMut.Unlock()
	PeerIPBlackList[peerIP] = struct{}{}
}

func addToRecvFiles(fileID string, local_fname string, trackerIP string, chunkerID uuid.UUID) {
	RecvFilesMut.Lock()
	RecvFiles[fileID] = []string{fileID, local_fname, trackerIP}
	RecvFilesMut.Unlock()
	sf := StoredFile{
		FileID:    fileID,
		FileName:  local_fname,
		ChunkerID: chunkerID,
		TrackerIP: trackerIP,
	}
	FileManagerAddFile(sf)
}

func SaveReceivers(path string) error {
	RecvFilesMut.Lock()
	defer RecvFilesMut.Unlock()

	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	w := csv.NewWriter(f)
	defer w.Flush()

	for _, v := range RecvFiles {
		err = w.Write(v)
		if err != nil {
			return err
		}
	}
	return nil
}

func LoadReceivers(path string, chunker *Chunker) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	r := csv.NewReader(f)

	for {
		sl, err := r.Read()
		if err == io.EOF || sl == nil || len(sl) == 0 {
			return nil
		} else if err != nil {
			return err
		}
		AddFileReceiver(sl[0], sl[1], sl[2], chunker, nil)
	}
}

func AddFileReceiver(fileID string, local_fname string, trackerIP string, chunker *Chunker, onComplete func(int, int, error)) {

	fdd, err := fetchFDD(fileID, trackerIP)
	if err != nil {
		fmt.Printf("Error fetching FDD: %v\n", err)
		if onComplete != nil {
			onComplete(0, 0, err)
		}
		return
	}

	chunkerID := chunker.addEmptyFile(fdd.ChunkCount)

	addToRecvFiles(fileID, local_fname, trackerIP, chunkerID)

	go fileReceiver(
		fdd,
		local_fname,
		chunker,
		chunkerID,
		onComplete,
	)
}

func RemoveFileReceiver(fileID string) {
	RecvFilesMut.Lock()
	delete(RecvFiles, fileID)
	RecvFilesMut.Unlock()
}

const MAX_RECV_THREADS = 10

func fetchFDD(fileID string, trackerIP string) (common.FileDownloadData, error) {

	tstart := time.Now()

	dict := map[string]interface{}{
		"class":   "init",
		"type":    "download_start",
		"file_id": fileID,
	}
	dictMsg, err := json.Marshal(dict)

	conn, err := net.Dial("tcp", trackerIP+":"+fmt.Sprint(common.TRACKER_TCP_PORT))

	if err != nil {
		return common.FileDownloadData{}, err
	}
	defer conn.Close()

	conn.Write(dictMsg)

	buf := make([]byte, 1024)
	fdd := common.FileDownloadData{}
	n, err := conn.Read(buf)
	err = json.Unmarshal(buf[:n], &fdd)
	if err != nil {
		return common.FileDownloadData{}, err
	}
	if fdd.ChunkCount == 0 {
		return common.FileDownloadData{}, errors.New("Empty FDD returned")
	}

	conn.Write([]byte("OK"))

	br := bufio.NewReader(conn)

	fdd.Checksums = make([]string, fdd.ChunkCount)

	for i := 0; i < fdd.ChunkCount; i += 1 {
		fdd.Checksums[i], _ = br.ReadString('\n')
		fdd.Checksums[i] = fdd.Checksums[i][:len(fdd.Checksums[i])-1]
	}

	tend := time.Now()

	fmt.Printf("Fetching FDD took %v\n", tend.Sub(tstart))

	return fdd, nil
}

func fileReceiver(fdd common.FileDownloadData, dest string, chunker *Chunker, chunkerID uuid.UUID, onComplete func(int, int, error)) error {
	tstart := time.Now()
	defer RemoveFileReceiver(fdd.FileID)

	if len(fdd.Peers) == 0 {
		err := errors.New("No peers for given file")
		if onComplete != nil {
			onComplete(0, 0, err)
		}
		return err
	}
	trigChan := make(chan int)

	next_peer := 0
	n_fail := 0
	n_peers := 0

	for n_peers < MAX_RECV_THREADS && next_peer < len(fdd.Peers) {
		go fileReceiveStream(fdd, chunkerID, fdd.Peers[next_peer], chunker, trigChan, onComplete)
		n_peers += 1
		next_peer += 1
	}

	for {
		code := <-trigChan
		if code == RECV_DONE {
			break
		} else if code == RECV_FAIL {
			n_fail += 1
			n_peers -= 1
			if next_peer < len(fdd.Peers) {
				go fileReceiveStream(fdd, chunkerID, fdd.Peers[next_peer], chunker, trigChan, onComplete)
				n_peers += 1
				next_peer += 1
			} else if n_fail == len(fdd.Peers) {
				// All peers have failed, return error
				if chunker.isFileDone(chunkerID) {
					break
				}
				err := errors.New("All peers have failed")
				if onComplete != nil {
					onComplete(0, 0, err)
				}
				return err
			}
		} else {
			fmt.Println("Unknown code ", code)
		}
	}

	err := chunker.unchunk(chunkerID, dest)
	if err != nil {
		fmt.Println("Failed while unchunking:", err)
		if onComplete != nil {
			onComplete(0, 0, err)
		}
		return err
	}
	fmt.Println("Done unchunking file ", dest)
	fmt.Printf("File transfer took %v\n", time.Now().Sub(tstart))
	if onComplete != nil {
		onComplete(fdd.ChunkCount, fdd.ChunkCount, nil)
	}
	return nil
}

func fileReceiveStream(
	fdd common.FileDownloadData,
	chunkerID uuid.UUID,
	peer common.Peer,
	chunker *Chunker,
	trig chan int,
	onUpdate func(int, int, error)) error {

	quicConf := quic.Config{
		MaxIdleTimeout: 60 * time.Second,
	}

	if IsPeerBlackListed(peer.IP) {
		trig <- RECV_FAIL
		return errors.New("Peer IP " + peer.IP + " was blacklisted")
	}

	destStr := fmt.Sprintf("%v:%v", peer.IP, peer.Port)
	conn, err := quic.DialAddr(context.Background(), destStr, generateTLSConfig(), &quicConf)
	if err != nil {
		trig <- RECV_FAIL
		return errors.New(fmt.Sprintf("Error dialing addr %v, %v", destStr, err))
	}

	stream, err := conn.AcceptStream(conn.Context())
	if err != nil {
		trig <- RECV_FAIL
		return errors.New(fmt.Sprintf("Error accepting stream from %v, %v", destStr, err))
	}

	buf := make([]byte, 1024)

	n, err := stream.Read(buf)

	dict := map[string]interface{}{}

	err = json.Unmarshal(buf[:n], &dict)
	if err != nil {
		trig <- RECV_FAIL
		return errors.New(fmt.Sprintf("Error unmarshalling %v hello, %v", string(buf[:n]), err))
	}

	frm := FileReqMsg{FileID: fdd.FileID, FileName: fdd.FileName}
	frmBuf, err := json.Marshal(frm)
	if err != nil {
		trig <- RECV_FAIL
		return errors.New(fmt.Sprintf("Error Marshaling FRM, %v", err))
	}

	n, err = stream.Write(frmBuf)
	if err != nil {
		trig <- RECV_FAIL
		return errors.New(fmt.Sprintf("Error writing FRM, %v", err))
	}

	n, err = stream.Read(buf)
	if err != nil {
		trig <- RECV_FAIL
		return err
	}

	iters := 0

	for !chunker.isFileDone(chunkerID) && iters < 5 {
		cam := ChunkAvailMsg{}
		err = json.Unmarshal(buf[:n], &cam)
		if err != nil {
			trig <- RECV_FAIL
			return errors.New(fmt.Sprintf("Error Unmarshaling CAM, %v", err))
		}

		for i := 0; i < len(cam.Chunks); i++ {
			if !chunker.markChunkBusyIfFree(chunkerID, cam.Chunks[i]) {
				continue
			}
			crm := ChunkReqMsg{Chunk: cam.Chunks[i], Status: STATUS_OK}
			crmBuf, err := json.Marshal(crm)
			if err != nil {
				trig <- RECV_FAIL
				return errors.New(fmt.Sprintf("Error marshaling CRM, %v", err))
			}
			_, err = stream.Write(crmBuf)
			if err != nil {
				trig <- RECV_FAIL
				return errors.New(fmt.Sprintf("Error writing CRM, %v", err))
			}

			fname, err := chunker.getChunkFname(chunkerID, cam.Chunks[i])
			if err != nil {
				trig <- RECV_FAIL
				return errors.New(fmt.Sprintf("Error getting chunk %v fname, %v", cam.Chunks[i], err))
			}
			f, err := os.Create(fname)
			if err != nil {
				trig <- RECV_FAIL
				return errors.New("Could not open file: " + fname + " " + err.Error())
			}

			size_buf := make([]byte, 8)
			n, err := stream.Read(size_buf)
			if err != nil {
				trig <- RECV_FAIL
				return err
			}

			size := binary.LittleEndian.Uint64(size_buf)
			done := uint64(0)

			for done < size {
				n, err = stream.Read(buf)
				if err != nil || n == 0 {
					fmt.Println(err)
					fmt.Println("n is ", n, " done is ", done, " size is ", size)
					break
				}
				f.Write(buf[:n])
				done += uint64(n)
			}

			f.Close()

			if done < size {
				trig <- RECV_FAIL
				return errors.New(fmt.Sprintf("Chunk %v did not reach expected size", cam.Chunks[i]))
			}

			verified, _ := chunker.verifyChunk(chunkerID, cam.Chunks[i], fdd.Checksums[cam.Chunks[i]])
			if !verified {
				fmt.Printf("Chunk %d had hash %v failed\n", cam.Chunks[i], fdd.Checksums[cam.Chunks[i]])
				chunker.deleteChunk(chunkerID, cam.Chunks[i])
				AddPeerToBlackList(peer.IP)
				trig <- RECV_FAIL
				return errors.New(fmt.Sprintf("Checksum fail on chunk %v", cam.Chunks[i]))
			}

			chunker.markChunkDone(chunkerID, cam.Chunks[i])
			if onUpdate != nil {
				done, total := chunker.GetProgress(chunkerID)
				onUpdate(done, total, nil)
			}

		}
		iters += 1
	}
	crm := ChunkReqMsg{Chunk: -1, Status: STATUS_DONE}
	crmBuf, err := json.Marshal(crm)
	stream.Write(crmBuf)
	stream.Close()

	if chunker.isFileDone(chunkerID) {
		trig <- RECV_DONE
	} else {
		trig <- RECV_FAIL
	}
	return nil
}
