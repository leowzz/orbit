package core

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

var oggCRCTable = func() [256]uint32 {
	var table [256]uint32
	const poly = 0x04c11db7
	for i := 0; i < 256; i++ {
		r := uint32(i) << 24
		for j := 0; j < 8; j++ {
			if r&0x80000000 != 0 {
				r = (r << 1) ^ poly
			} else {
				r <<= 1
			}
		}
		table[i] = r
	}
	return table
}()

func oggCRC(data []byte) uint32 {
	var crc uint32
	for _, b := range data {
		crc = (crc << 8) ^ oggCRCTable[byte(crc>>24)^b]
	}
	return crc
}

func appendOggPage(dst []byte, headerType byte, granule uint64, serial uint32, seq uint32, packets [][]byte) []byte {
	var lacing []byte
	payloadLen := 0
	for _, pkt := range packets {
		n := len(pkt)
		for n >= 255 {
			lacing = append(lacing, 255)
			n -= 255
		}
		lacing = append(lacing, byte(n))
		payloadLen += len(pkt)
	}
	start := len(dst)
	headerLen := 27 + len(lacing)
	dst = append(dst, make([]byte, headerLen+payloadLen)...)
	copy(dst[start:], "OggS")
	dst[start+4] = 0
	dst[start+5] = headerType
	binary.LittleEndian.PutUint64(dst[start+6:], granule)
	binary.LittleEndian.PutUint32(dst[start+14:], serial)
	binary.LittleEndian.PutUint32(dst[start+18:], seq)
	dst[start+26] = byte(len(lacing))
	copy(dst[start+27:], lacing)
	pos := start + headerLen
	for _, pkt := range packets {
		copy(dst[pos:], pkt)
		pos += len(pkt)
	}
	binary.LittleEndian.PutUint32(dst[start+22:], oggCRC(dst[start:]))
	return dst
}

func encodeOggOpus(packets []string, sampleRate int, channels int) ([]byte, error) {
	if len(packets) == 0 {
		return nil, errors.New("empty opus packets")
	}
	rawPackets := make([][]byte, len(packets))
	for i, p := range packets {
		b, err := base64.StdEncoding.Strict().DecodeString(p)
		if err != nil || len(b) == 0 {
			return nil, errors.New("invalid opus packet")
		}
		rawPackets[i] = b
	}

	opusHead := make([]byte, 19)
	copy(opusHead[0:], "OpusHead")
	opusHead[8] = 1
	opusHead[9] = byte(channels)
	binary.LittleEndian.PutUint16(opusHead[10:], 0)
	binary.LittleEndian.PutUint32(opusHead[12:], uint32(sampleRate))
	binary.LittleEndian.PutUint16(opusHead[16:], 0)
	opusHead[18] = 0

	vendor := "orbit"
	opusTags := make([]byte, 16+len(vendor))
	copy(opusTags[0:], "OpusTags")
	binary.LittleEndian.PutUint32(opusTags[8:], uint32(len(vendor)))
	copy(opusTags[12:], vendor)
	binary.LittleEndian.PutUint32(opusTags[12+len(vendor):], 0)

	const serial uint32 = 1
	var out []byte
	var seq uint32
	out = appendOggPage(out, 0x02, 0, serial, seq, [][]byte{opusHead})
	seq++
	out = appendOggPage(out, 0x00, 0, serial, seq, [][]byte{opusTags})
	seq++

	var granule uint64
	for i := 0; i < len(rawPackets); {
		var pagePackets [][]byte
		lacingCount := 0
		for i < len(rawPackets) {
			need := len(rawPackets[i])/255 + 1
			if lacingCount+need > 255 {
				break
			}
			pagePackets = append(pagePackets, rawPackets[i])
			lacingCount += need
			granule += 960
			i++
		}
		var flag byte
		if i == len(rawPackets) {
			flag = 0x04
		}
		out = appendOggPage(out, flag, granule, serial, seq, pagePackets)
		seq++
	}
	return out, nil
}

func decodeOggOpus(data []byte) ([]string, error) {
	var allPackets [][]byte
	var current []byte
	pos := 0
	for pos < len(data) {
		if pos+27 > len(data) || string(data[pos:pos+4]) != "OggS" {
			return nil, errors.New("invalid ogg page header")
		}
		segCount := int(data[pos+26])
		headerEnd := pos + 27 + segCount
		if headerEnd > len(data) {
			return nil, errors.New("truncated ogg segment table")
		}
		lacing := data[pos+27 : headerEnd]
		payloadLen := 0
		for _, l := range lacing {
			payloadLen += int(l)
		}
		pageEnd := headerEnd + payloadLen
		if pageEnd > len(data) {
			return nil, errors.New("truncated ogg page payload")
		}
		expectedCRC := binary.LittleEndian.Uint32(data[pos+22 : pos+26])
		pageCopy := make([]byte, pageEnd-pos)
		copy(pageCopy, data[pos:pageEnd])
		pageCopy[22], pageCopy[23], pageCopy[24], pageCopy[25] = 0, 0, 0, 0
		if oggCRC(pageCopy) != expectedCRC {
			return nil, errors.New("ogg crc mismatch")
		}
		offset := headerEnd
		for _, l := range lacing {
			seg := data[offset : offset+int(l)]
			offset += int(l)
			current = append(current, seg...)
			if l < 255 {
				pkt := make([]byte, len(current))
				copy(pkt, current)
				allPackets = append(allPackets, pkt)
				current = current[:0]
			}
		}
		pos = pageEnd
	}
	if len(current) != 0 {
		return nil, errors.New("unterminated ogg packet")
	}
	if len(allPackets) < 3 || !bytes.HasPrefix(allPackets[0], []byte("OpusHead")) || !bytes.HasPrefix(allPackets[1], []byte("OpusTags")) {
		return nil, errors.New("missing opus headers")
	}
	packets := make([]string, len(allPackets)-2)
	for i, pkt := range allPackets[2:] {
		packets[i] = base64.StdEncoding.Strict().EncodeToString(pkt)
	}
	return packets, nil
}

type audioFileStore struct {
	dir              string
	maxBytes         int64
	thresholdPercent int
	mu               sync.Mutex
}

func newAudioFileStore(dir string, maxBytes int64, thresholdPercent int) (*audioFileStore, error) {
	if strings.TrimSpace(dir) == "" {
		return nil, errors.New("audio data_dir is required")
	}
	if maxBytes <= 0 {
		return nil, errors.New("audio max_bytes must be positive")
	}
	if thresholdPercent <= 0 || thresholdPercent > 100 {
		return nil, errors.New("audio cleanup_threshold_percent must be between 1 and 100")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".opus.tmp") {
			_ = os.Remove(filepath.Join(dir, entry.Name()))
		}
	}
	s := &audioFileStore{
		dir:              dir,
		maxBytes:         maxBytes,
		thresholdPercent: thresholdPercent,
	}
	if _, err := s.cleanup(""); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *audioFileStore) pathFor(segmentID string) string {
	return filepath.Join(s.dir, segmentID+".opus")
}

func (s *audioFileStore) save(segment AudioSegment) error {
	encoded, err := encodeOggOpus(segment.Packets, segment.SampleRate, segment.Channels)
	if err != nil {
		return err
	}
	startedAt, err := time.Parse(time.RFC3339Nano, segment.StartedAt)
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	target := s.pathFor(segment.SegmentID)
	if existing, err := os.ReadFile(target); err == nil {
		if !bytes.Equal(existing, encoded) {
			return errors.New("audio segment ID conflict")
		}
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	tmp := target + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	writeOK := false
	defer func() {
		if !writeOK {
			_ = f.Close()
			_ = os.Remove(tmp)
		}
	}()
	if _, err := f.Write(encoded); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Chtimes(tmp, startedAt, startedAt); err != nil {
		return err
	}
	if err := os.Rename(tmp, target); err != nil {
		return err
	}
	writeOK = true
	return nil
}

func (s *audioFileStore) loadPackets(segmentID string) ([]string, error) {
	data, err := os.ReadFile(s.pathFor(segmentID))
	if err != nil {
		return nil, err
	}
	return decodeOggOpus(data)
}

func (s *audioFileStore) remove(segmentID string) error {
	err := os.Remove(s.pathFor(segmentID))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func (s *audioFileStore) triggerBytes() int64 {
	return (s.maxBytes * int64(s.thresholdPercent)) / 100
}

type audioFileEntry struct {
	id      string
	path    string
	size    int64
	modTime time.Time
}

func (s *audioFileStore) cleanup(keepSegmentID string) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, err
	}
	var files []audioFileEntry
	var totalBytes int64
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".opus") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, err
		}
		files = append(files, audioFileEntry{
			id:      strings.TrimSuffix(entry.Name(), ".opus"),
			path:    filepath.Join(s.dir, entry.Name()),
			size:    info.Size(),
			modTime: info.ModTime(),
		})
		totalBytes += info.Size()
	}

	limit := s.triggerBytes()
	if totalBytes < limit {
		return nil, nil
	}

	sort.Slice(files, func(i, j int) bool {
		if !files[i].modTime.Equal(files[j].modTime) {
			return files[i].modTime.Before(files[j].modTime)
		}
		return files[i].id < files[j].id
	})

	var removed []string
	for _, f := range files {
		if totalBytes < limit {
			break
		}
		if keepSegmentID != "" && f.id == keepSegmentID {
			continue
		}
		if err := os.Remove(f.path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return removed, err
		}
		totalBytes -= f.size
		removed = append(removed, f.id)
	}
	return removed, nil
}

func (s *audioFileStore) stats() (count int, totalBytes int64, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return 0, 0, err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".opus") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return 0, 0, err
		}
		count++
		totalBytes += info.Size()
	}
	return count, totalBytes, nil
}

var _ = io.EOF
