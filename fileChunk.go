package main

import (
	"crypto/sha256"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strconv"
	"sync"

	"github.com/google/uuid"
)

func must(a any, err error) any {
	if err == nil {
		return a
	}
	fmt.Println("Error: ", err.Error())
	panic(err)
}

const CHUNK_SIZE int64 = 10240

const (
	CHUNK_EMPTY = iota
	CHUNK_BUSY
	CHUNK_DONE
)

type ChunkedFile struct {
	Chunks     []Chunk
	ChunksDone int
}

type Chunk struct {
	FileName string
	Status   int
	Hash     string
}

type Chunker struct {
	chunkCache map[uuid.UUID]ChunkedFile // Map File ID to []Chunk
	chunkMutex sync.Mutex
	chunkPath  string
}

func (self *Chunker) init(chunkPath string) error {
	self.chunkPath = chunkPath
	_, err := os.Stat(chunkPath)
	if os.IsNotExist(err) {
		err = os.MkdirAll(chunkPath, 0777)
	} else if err != nil {
		return err
	}
	self.chunkCache = make(map[uuid.UUID]ChunkedFile)
	return err
}

func (self *Chunker) getChunks(fileID uuid.UUID) ([]Chunk, error) {
	self.chunkMutex.Lock()
	defer self.chunkMutex.Unlock()
	return self.getChunksUnsafe(fileID)
}

func (self *Chunker) getChunksUnsafe(fileID uuid.UUID) ([]Chunk, error) {
	cf, ok := self.chunkCache[fileID]
	if !ok {
		return []Chunk{}, errors.New(fmt.Sprintf("Unknown file ID %v", fileID))
	}
	return cf.Chunks, nil
}

func (self *Chunker) addDiskFile(fname string) (uuid.UUID, error) {
	self.chunkMutex.Lock()
	defer self.chunkMutex.Unlock()

	// Chunks the file with the given file name, and returns a UUID to refer to it
	fileID := uuid.New()

	fstat, err := os.Stat(fname)
	if err != nil {
		return fileID, err
	}
	size := fstat.Size()
	chunks := int(size / CHUNK_SIZE)
	if size%CHUNK_SIZE > 0 {
		chunks += 1
	}
	cf := make([]Chunk, chunks)
	f, err := os.Open(fname)
	defer f.Close()
	if err != nil {
		return fileID, err
	}
	buf := make([]byte, 1024)
	//hasher := fnv.New32()
	//hasher.Write([]byte(fileID.String()))
	//hash := hasher.Sum32()
	hash := fileID.String()
	for chunk := 0; chunk < chunks; chunk++ {
		cfname := fmt.Sprintf("%v/%v_%v.chunk", self.chunkPath, hash, chunk)
		fwrite, err := os.Create(cfname)
		if err != nil {
			return fileID, err
		}
		defer fwrite.Close()

		n, err := f.Read(buf)

		h := sha256.New()

		for n != 0 && err == nil {
			fwrite.Write(buf[:n])
			h.Write(buf[:n])
			n, err = f.Read(buf)
		}
		if err != nil && err != io.EOF {
			return fileID, err
		}
		//if n < int(CHUNK_SIZE) && chunk != chunks-1 {
		//	return fileID, errors.New("Unexpected read size drop")
		//}

		cf[chunk] = Chunk{
			FileName: cfname,
			Status:   CHUNK_DONE,
			Hash:     fmt.Sprintf("%x", h.Sum(nil)),
		}
	}
	self.chunkCache[fileID] = ChunkedFile{Chunks: cf, ChunksDone: len(cf)}
	return fileID, nil
}

func (self *Chunker) addEmptyFile(n_chunks int) uuid.UUID {
	self.chunkMutex.Lock()
	defer self.chunkMutex.Unlock()

	fileID := uuid.New()

	arr := make([]Chunk, n_chunks)
	self.chunkCache[fileID] = ChunkedFile{Chunks: arr, ChunksDone: 0}

	hash := fileID.String()
	for i := 0; i < n_chunks; i += 1 {
		cfname := fmt.Sprintf("%v/%v_%v.chunk", self.chunkPath, hash, i)
		arr[i] = Chunk{FileName: cfname, Status: CHUNK_EMPTY, Hash: ""}
	}

	return fileID
}

func (self *Chunker) getChunkFname(fileID uuid.UUID, chunk int) (string, error) {
	self.chunkMutex.Lock()
	defer self.chunkMutex.Unlock()

	cf, ok := self.chunkCache[fileID]
	if !ok {
		return "", errors.New(fmt.Sprintf("Unkown fileID %v", fileID.String()))
	}
	res := cf.Chunks[chunk]
	return res.FileName, nil
}

func (self *Chunker) isChunkDone(fileID uuid.UUID, chunk int) bool {
	self.chunkMutex.Lock()
	defer self.chunkMutex.Unlock()

	cf, ok := self.chunkCache[fileID]
	if !ok {
		return false
	}
	res := cf.Chunks[chunk]
	if res.Status != CHUNK_DONE {
		return false
	} else {
		return true
	}
}

func (self *Chunker) deleteFile(fileID uuid.UUID) {
	self.chunkMutex.Lock()
	defer self.chunkMutex.Unlock()

	delete(self.chunkCache, fileID)
}

func (self *Chunker) markChunkBusyIfFree(fileID uuid.UUID, chunk int) bool {
	self.chunkMutex.Lock()
	defer self.chunkMutex.Unlock()

	ch := &(self.chunkCache[fileID].Chunks[chunk])
	if ch.Status == CHUNK_EMPTY {
		ch.Status = CHUNK_BUSY
		return true
	} else {
		return false
	}
}

func (self *Chunker) markChunkDone(fileID uuid.UUID, chunk int) {
	self.chunkMutex.Lock()
	defer self.chunkMutex.Unlock()

	cf := self.chunkCache[fileID]
	if cf.Chunks[chunk].Status != CHUNK_DONE {
		cf.Chunks[chunk].Status = CHUNK_DONE
		cf.ChunksDone += 1
		self.chunkCache[fileID] = cf
	}
}

func (self *Chunker) verifyChunk(fileID uuid.UUID, chunk int, hash string) (bool, error) {
	self.chunkMutex.Lock()
	defer self.chunkMutex.Unlock()

	cf := self.chunkCache[fileID]
	chk := cf.Chunks[chunk]
	if chk.Hash == "" {
		h := sha256.New()
		f, err := os.Open(chk.FileName)
		defer f.Close()
		if err != nil {
			fmt.Printf("Couldn't open chunk file %v %v %v\n", fileID, chunk, chk.FileName)
			return false, errors.New("Couldn't open chunk file")
		}
		buf := make([]byte, 1024)
		n, err := f.Read(buf)
		for n != 0 && err == nil {
			h.Write(buf[:n])
			n, err = f.Read(buf)
		}
		if err != nil && err != io.EOF {
			fmt.Printf("Couldn't read from chunk file?\n")
			return false, errors.New("Couldn't read from chunk file")
		}
		chk.Hash = fmt.Sprintf("%x", h.Sum(nil))
		cf.Chunks[chunk] = chk
	}
	return chk.Hash == hash, nil
}

func (self *Chunker) deleteChunk(fileID uuid.UUID, chunk int) {
	self.chunkMutex.Lock()
	defer self.chunkMutex.Unlock()

	cf := self.chunkCache[fileID]
	//os.Remove(cf.Chunks[chunk].FileName)
	cf.Chunks[chunk].Hash = ""
	cf.Chunks[chunk].Status = CHUNK_EMPTY
}

func (self *Chunker) isFileDone(fileID uuid.UUID) bool {
	self.chunkMutex.Lock()
	defer self.chunkMutex.Unlock()

	cf := self.chunkCache[fileID]

	return cf.ChunksDone == len(cf.Chunks)
}

func (self *Chunker) unchunk(fileID uuid.UUID, dest string) error {
	self.chunkMutex.Lock()
	defer self.chunkMutex.Unlock()

	wf, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer wf.Close()

	cd, err := self.getChunksUnsafe(fileID)
	if err != nil {
		return err
	}

	chunks := len(cd)

	buf := make([]byte, 1024)
	for i := 0; i < chunks; i++ {
		fchunk := cd[i]
		if fchunk.Status != CHUNK_DONE {
			return errors.New(fmt.Sprintf("Chunk %v is not done", i))
		}
		f, err := os.Open(fchunk.FileName)
		if err != nil {
			return err
		}
		defer f.Close()

		n, err := f.Read(buf)
		for n != 0 && err == nil {
			wf.Write(buf[:n])
			n, err = f.Read(buf)
		}
		if err != io.EOF && err != nil {
			return err
		}
	}
	return nil
}

func (self *Chunker) getDoneChunks(fileID uuid.UUID) []int {
	self.chunkMutex.Lock()
	defer self.chunkMutex.Unlock()
	cf := self.chunkCache[fileID]
	res := make([]int, len(cf.Chunks))
	ri := 0
	for i := range cf.Chunks {
		if cf.Chunks[i].Status == CHUNK_DONE {
			res[ri] = i
			ri += 1
		}
	}
	return res[:ri]
}

func (self *Chunker) getCheckSums(fileID uuid.UUID) []string {
	self.chunkMutex.Lock()
	defer self.chunkMutex.Unlock()

	cf := self.chunkCache[fileID]
	res := make([]string, len(cf.Chunks))
	for i := range cf.Chunks {
		res[i] = cf.Chunks[i].Hash
	}
	return res
}

func writeChunks(fname string, chunks []Chunk) error {
	f, err := os.Create(fname)
	if err != nil {
		return err
	}
	defer f.Close()

	w := csv.NewWriter(f)
	defer w.Flush()

	for i := range chunks {
		st := chunks[i].Status
		if st == CHUNK_BUSY {
			st = CHUNK_EMPTY
		}
		w.Write([]string{
			chunks[i].FileName,
			chunks[i].Hash,
			strconv.Itoa(st),
		})
	}

	return nil
}

func readChunks(fname string, chunks []Chunk) error {
	f, err := os.Open(fname)
	if err != nil {
		return err
	}
	defer f.Close()

	r := csv.NewReader(f)

	for i := range chunks {
		rec, err := r.Read()
		if err != nil {
			return err
		}
		chunks[i] = Chunk{
			FileName: rec[0],
			Hash:     rec[1],
			Status:   must(strconv.Atoi(rec[2])).(int),
		}
	}

	return nil
}

func (self *Chunker) saveChunker() error {
	err := os.Mkdir(self.chunkPath+"/savefiles", 0777)
	if !errors.Is(err, fs.ErrExist) && err != nil {
		fmt.Println(err.Error())
		return err
	}
	err = os.Mkdir(self.chunkPath+"/savefiles/chunked", 0777)
	if !errors.Is(err, fs.ErrExist) && err != nil {
		fmt.Println(err.Error())
		return err
	}

	self.chunkMutex.Lock()
	defer self.chunkMutex.Unlock()

	f, err := os.Create(self.chunkPath + "/savefiles/chunkersave.csv")
	defer f.Close()
	if err != nil {
		return err
	}

	w := csv.NewWriter(f)
	defer w.Flush()

	for k, v := range self.chunkCache {
		w.Write([]string{
			k.String(),
			strconv.Itoa(v.ChunksDone),
			strconv.Itoa(len(v.Chunks)),
		})
		writeChunks(self.chunkPath+"/savefiles/chunked/"+k.String(), v.Chunks)
	}
	return nil
}

func (self *Chunker) loadChunker() error {
	save, err := os.Open(self.chunkPath + "/savefiles/chunkersave.csv")
	if err != nil {
		return err
	}
	r := csv.NewReader(save)

	for {
		sl, err := r.Read()
		if sl == nil {
			break
		} else if err != nil {
			return err
		}
		fileID, err := uuid.Parse(sl[0])
		if err != nil {
			return err
		}
		cf := ChunkedFile{
			ChunksDone: must(strconv.Atoi(sl[1])).(int),
			Chunks:     make([]Chunk, must(strconv.Atoi(sl[2])).(int)),
		}
		readChunks(self.chunkPath+"/savefiles/chunked/"+fileID.String(), cf.Chunks)
		self.chunkCache[fileID] = cf
	}

	return nil
}

func (self *Chunker) GetProgress(fileID uuid.UUID) (int, int) {
	self.chunkMutex.Lock()
	defer self.chunkMutex.Unlock()

	cf, ok := self.chunkCache[fileID]
	if !ok {
		return 0, 0
	}
	return cf.ChunksDone, len(cf.Chunks)
}
