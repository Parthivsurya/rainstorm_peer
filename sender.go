package main

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"

	common "github.com/aadit-n3rdy/rainstorm_common"
	"github.com/google/uuid"

	"net"

	"github.com/quic-go/quic-go"
)

var testFileID uuid.UUID

func pushFDD(fdd *common.FileDownloadData, trackerIP string) error {

	smallfdd := common.FileDownloadData{
		FileID:     fdd.FileID,
		FileName:   fdd.FileName,
		Peers:      fdd.Peers,
		Checksums:  []string{},
		ChunkCount: fdd.ChunkCount,
	}

	dict := map[string]interface{}{
		"class":              "init",
		"type":               "file_register",
		"file_download_data": smallfdd,
	}

	conn, err := net.Dial("tcp", trackerIP+":"+fmt.Sprint(common.TRACKER_TCP_PORT))

	if err != nil {
		return err
	}
	defer conn.Close()

	fdd_msg, err := json.Marshal(dict)
	_, err = conn.Write(fdd_msg)
	if err != nil {
		return err
	}

	buf := []byte("ABCD")

	conn.Read(buf)
	for i := 0; i < fdd.ChunkCount; i += 1 {
		conn.Write([]byte(fmt.Sprintf("%v\n", fdd.Checksums[i])))
	}

	return nil
}

func sendHandler(listener *quic.Listener, chunker *Chunker) {
	for {
		// 0. listen for connections
		conn, err := listener.Accept(context.Background())
		if err != nil {
			fmt.Println("Could not accept conn from ", conn.RemoteAddr().String(), err)
			return
		}
		go sendHandlerStream(conn, chunker)
	}
}

func sendHandlerStream(conn quic.Connection, chunker *Chunker) {
	defer conn.CloseWithError(quic.ApplicationErrorCode(0), "bye!")

	// 1. get file ID and file Name
	//fmt.Println("New connection from ", conn.RemoteAddr().String())
	stream, err := conn.OpenStream()
	if err != nil {
		fmt.Println("Could not open stream to ", conn.RemoteAddr().String(), err)
		return
	}
	defer stream.Close()
	helloDict := map[string]interface{}{
		"status": STATUS_OK,
	}
	helloMsg, err := json.Marshal(helloDict)
	if err != nil {
		fmt.Println("Could not marshal hello msg ", conn.RemoteAddr().String(), err)
		return
	}
	n, err := stream.Write(helloMsg)
	if err != nil {
		fmt.Printf("Could not send hello\n%v bytes sent\nError: %v\n", n, err)
		return
	}

	recvBuf := make([]byte, 1024)
	n, err = stream.Read(recvBuf)
	if err != nil {
		fmt.Printf("Could not read frm from %v\nError: %v\n", conn.RemoteAddr().String(), err)
		return
	}
	//fmt.Println("File request msg: ", string(recvBuf[:n]))
	frm := FileReqMsg{}
	err = json.Unmarshal(recvBuf[:n], &frm)
	if err != nil {
		fmt.Printf("Couldn't unmarshall frm, %v\n", err)
		return
	}

	// 2. check file cache
	sf, ok := FileManagerGetFile(frm.FileID)
	if !ok {
		stream.Write([]byte(fmt.Sprintf("{\"status\": %v}", STATUS_MISSING)))
		fmt.Printf("Unkown file ID %v\n", frm.FileID)
		return
	}
	cd, err := chunker.getChunks(sf.ChunkerID)
	if err != nil {
		fmt.Printf("Unknown chunker id %v %v\n", sf.ChunkerID.String(), err)
		return
	}

	cam := ChunkAvailMsg{}
	cam.Chunks = make([]int, len(cd))
	i := 0
	for k := range cd {
		cam.Chunks[i] = k
		i += 1
	}
	camBuf, err := json.Marshal(cam)
	if err != nil {
		fmt.Println("Couldnt marshal cam: ", err)
		return
	}
	n, err = stream.Write(camBuf)

	for true {
		n, err = stream.Read(recvBuf)
		if n == 0 {
			break
		}
		crm := ChunkReqMsg{}
		err = json.Unmarshal(recvBuf[:n], &crm)
		//fmt.Println(crm.Chunk)
		if crm.Status == STATUS_DONE {
			break
		}

		fname, err := chunker.getChunkFname(sf.ChunkerID, crm.Chunk)
		if err != nil {
			fmt.Println(err)
			break
		}

		st, err := os.Stat(fname)
		if err != nil {
			fmt.Println(err)
			break
		}
		size := st.Size()

		var done int64 = 0

		writeBuf := make([]byte, 1024)
		f, err := os.Open(fname)

		size_bytes := make([]byte, 8)
		binary.LittleEndian.PutUint64(size_bytes, uint64(size))

		stream.Write(size_bytes)
		for done < size {
			n, err = f.Read(writeBuf)
			if n == 0 || err != nil {
				break
			}
			done += int64(n)
			stream.Write(writeBuf[:n])
		}
	}
}
