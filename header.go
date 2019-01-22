package rpmpack

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"sort"
)

const (
	signatures = 0x3e
	immutable  = 0x3f

	typeInt32       = 0x04
	typeBinary      = 0x07
	typeStringArray = 0x08
)

var boundaries = map[int]int{
	typeInt32: 4,
}

type indexEntry struct {
	rpmtype, count int
	data           []byte
}

func (e indexEntry) indexBytes(tag, contentOffset int) []byte {
	b := &bytes.Buffer{}
	binary.Write(b, binary.BigEndian, []int32{int32(tag), int32(e.rpmtype), int32(contentOffset), int32(e.count)})
	return b.Bytes()
}

func StringArrayEntry(value []string) indexEntry {
	b := [][]byte{}
	for _, v := range value {
		b = append(b, []byte(v))
	}
	bb := append(bytes.Join(b, []byte{00}), byte(00))
	return indexEntry{typeStringArray, len(value), bb}
}

func BinaryEntry(value []byte) indexEntry {
	return indexEntry{typeBinary, len(value), value}
}

func Int32Entry(value []int32) indexEntry {
	b := &bytes.Buffer{}
	binary.Write(b, binary.BigEndian, value)
	return indexEntry{typeInt32, len(value), b.Bytes()}
}

type index struct {
	entries map[int]indexEntry
	size    int
	h       int
}

func NewIndex(h int) *index {
	return &index{entries: make(map[int]indexEntry), h: h}
}
func (i *index) Add(tag int, e indexEntry) {
	i.entries[tag] = e
}
func (i *index) sortedTags() []int {
	t := []int{}
	for k := range i.entries {
		t = append(t, k)
	}
	sort.Ints(t)
	return t
}
func pad(w io.Writer, rpmtype, offset int) {
	// We need to align integer entries...
	if b, ok := boundaries[rpmtype]; ok && offset%b != 0 {
		w.Write(make([]byte, b-offset%b))
	}
}

// Write finalizes the index and writes it.
func (i *index) Write(w io.Writer) error {
	// Even the header has three parts: The lead, the index entries, and the entries.
	// Because of alignment, we can only tell the actual size and offset after writing
	// the entries.
	entryData := &bytes.Buffer{}
	tags := i.sortedTags()
	offsets := make([]int, len(tags))
	for ii, tag := range tags {
		e := i.entries[tag]
		pad(entryData, e.rpmtype, entryData.Len())
		offsets[ii] = entryData.Len()
		entryData.Write(e.data)
	}
	entryData.Write(i.eigenHeader().data)

	// 4 magic and 4 reserved
	w.Write([]byte{0x8e, 0xad, 0xe8, 0x01, 0, 0, 0, 0})
	// 4 count and 4 size
	// We add the pseudo-entry "selfddHeader" to count.
	if err := binary.Write(w, binary.BigEndian, []int32{int32(len(i.entries)) + 1, int32(entryData.Len())}); err != nil {
		return err
	}
	// Write the selfHeader index entry
	w.Write(i.eigenHeader().indexBytes(i.h, entryData.Len()-0x10))
	// Write all of the other index entries
	for ii, tag := range tags {
		e := i.entries[tag]
		w.Write(e.indexBytes(tag, offsets[ii]))
	}
	w.Write(entryData.Bytes())
	return nil
}

// the eigenHeader is a weird entry. Its index entry is sorted first, but its content
// is last. The content is a 16 byte index entry, which is almost the same as the index
// entry except for the offset. The offset here is ... minus the length of the index entry region.
// Which is always 0x10 * number of entries.
// I kid you not.
func (i *index) eigenHeader() indexEntry {
	b := &bytes.Buffer{}
	binary.Write(b, binary.BigEndian, []int32{int32(i.h), int32(typeBinary), -int32(0x10 * (len(i.entries) + 1)), int32(0x10)})
	return BinaryEntry(b.Bytes())
}

func Lead(name, version, release string) []byte {
	// RPM format = 0xedabeedb
	// version 3.0 = 0x0300
	// type binary = 0x0000
	// machine archnum (i386?) = 0x0001
	// name ( 66 bytes, with null termination)
	// osnum (linux?) = 0x0001
	// sig type (header-style) = 0x0005
	// reserved 16 bytes of 0x00
	n := []byte(fmt.Sprintf("%s-%s-%s", name, version, release))
	if len(n) > 65 {
		n = n[:65]
	}
	n = append(n, make([]byte, 66-len(n))...)
	b := []byte{0xed, 0xab, 0xee, 0xdb, 0x03, 0x00, 0x00, 0x00, 0x00, 0x01}
	b = append(b, n...)
	b = append(b, []byte{0x00, 0x01, 0x00, 0x05}...)
	b = append(b, make([]byte, 16)...)
	return b
}