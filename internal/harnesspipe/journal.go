package harnesspipe

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"time"
)

// Journal publishes only fsynced frames. Offsets are rebuilt from the append-only
// file on startup. A torn final write is discarded; corruption in a complete
// record fails closed instead of silently skipping provider output.
type Journal struct {
	mu      sync.Mutex
	f       *os.File
	offsets []int64
	end     int64
	failure error
}

func openJournal(path string) (*Journal, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	j := &Journal{f: f}
	fail := func(err error) (*Journal, error) { f.Close(); return nil, err }
	r := bufio.NewReader(f)
	for {
		line, err := boundedRecord(r)
		if len(line) > MaxRecord {
			return fail(errors.New("oversized journal record"))
		}
		if errors.Is(err, io.EOF) {
			if len(line) > 0 {
				if e := f.Truncate(j.end); e != nil {
					return fail(e)
				}
				if e := f.Sync(); e != nil {
					return fail(e)
				}
			}
			break
		}
		if err != nil {
			return fail(err)
		}
		var frame RawFrame
		if json.Unmarshal(line, &frame) != nil || frame.Sequence != int64(len(j.offsets)+1) {
			return fail(errors.New("corrupt provider journal"))
		}
		j.offsets = append(j.offsets, j.end)
		j.end += int64(len(line))
	}
	return j, nil
}
func (j *Journal) Append(run, stream string, data []byte) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.failure != nil {
		return j.failure
	}
	if len(data) > MaxFrame {
		return errors.New("provider frame exceeds capture limit")
	}
	frame := RawFrame{Sequence: int64(len(j.offsets) + 1), RunID: run, ReceivedAt: time.Now().UTC(), Stream: stream, Data: data}
	text := frame.Text()
	frame.Original = &text
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(frame); err != nil {
		return err
	}
	raw := buffer.Bytes()
	var err error
	n, err := j.f.WriteAt(raw, j.end)
	if err == nil && n != len(raw) {
		err = io.ErrShortWrite
	}
	if err == nil {
		err = j.f.Sync()
	}
	if err != nil {
		j.failure = err
		return err
	}
	j.offsets = append(j.offsets, j.end)
	j.end += int64(n)
	return nil
}
func (j *Journal) Read(after int64) ([]RawFrame, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if after < 0 || after > int64(len(j.offsets)) {
		return nil, fmt.Errorf("invalid journal cursor %d", after)
	}
	out := []RawFrame{}
	var total int64
	for i := after; i < int64(len(j.offsets)) && len(out) < 64; i++ {
		end := j.end
		if i+1 < int64(len(j.offsets)) {
			end = j.offsets[i+1]
		}
		size := end - j.offsets[i]
		if len(out) > 0 && total+size > MaxFrame {
			break
		}
		raw := make([]byte, size)
		if _, err := j.f.ReadAt(raw, j.offsets[i]); err != nil {
			return nil, err
		}
		var frame RawFrame
		if err := json.Unmarshal(raw, &frame); err != nil {
			return nil, err
		}
		out = append(out, frame)
		total += size
	}
	return out, nil
}
func (j *Journal) Close() error { return j.f.Close() }

func boundedRecord(r *bufio.Reader) ([]byte, error) {
	var line []byte
	for {
		part, err := r.ReadSlice('\n')
		if len(line)+len(part) > MaxRecord {
			return nil, errors.New("oversized journal record")
		}
		line = append(line, part...)
		if !errors.Is(err, bufio.ErrBufferFull) {
			return line, err
		}
	}
}
