package artifacttype

import (
	"bytes"
	"encoding/binary"
	"math"
	"strings"
	"unicode/utf16"
)

var compoundFileSignature = []byte{0xd0, 0xcf, 0x11, 0xe0, 0xa1, 0xb1, 0x1a, 0xe1}

const (
	cfbFreeSector    = uint32(0xffffffff)
	cfbEndOfChain    = uint32(0xfffffffe)
	cfbFATSector     = uint32(0xfffffffd)
	cfbDIFATSector   = uint32(0xfffffffc)
	cfbNoStream      = uint32(0xffffffff)
	cfbMiniStreamCut = uint64(4096)
)

type compoundFile struct {
	body            []byte
	majorVersion    uint16
	sectorSize      uint64
	miniSectorSize  uint64
	sectorCount     uint32
	fat             []uint32
	miniFAT         []uint32
	rootMiniStream  []byte
	directory       []compoundDirectoryEntry
	firstMiniFAT    uint32
	numMiniFAT      uint32
	firstDirectory  uint32
	numFATSectors   uint32
	firstDIFAT      uint32
	numDIFATSectors uint32
	headerDIFAT     []uint32
}

type compoundDirectoryEntry struct {
	name        string
	objectType  byte
	left        uint32
	right       uint32
	child       uint32
	startSector uint32
	size        uint64
}

func detectCompoundOffice(body []byte) (string, error) {
	file, err := parseCompoundFile(body)
	if err != nil {
		return "", ErrDenied
	}
	families := map[string]compoundDirectoryEntry{}
	for _, entry := range file.directory {
		if entry.objectType != 2 {
			continue
		}
		switch strings.ToLower(entry.name) {
		case "worddocument":
			families["word"] = entry
		case "workbook", "book":
			families["excel"] = entry
		case "powerpoint document":
			families["powerpoint"] = entry
		}
	}
	if len(families) != 1 {
		return "", ErrDenied
	}
	for family, entry := range families {
		if !passiveCompoundOfficeDirectory(file, family) {
			return "", ErrDenied
		}
		prefix, readErr := file.readStreamPrefix(entry, 32)
		if readErr != nil {
			return "", ErrDenied
		}
		switch family {
		case "word":
			if len(prefix) < 12 || binary.LittleEndian.Uint16(prefix[:2]) != 0xa5ec {
				return "", ErrDenied
			}
			return "application/msword", nil
		case "excel":
			if !validBIFFPrefix(prefix) {
				return "", ErrDenied
			}
			return "application/vnd.ms-excel", nil
		case "powerpoint":
			if len(prefix) < 8 || prefix[0]&0x0f != 0x0f || binary.LittleEndian.Uint16(prefix[2:4]) != 0x03e8 || uint64(binary.LittleEndian.Uint32(prefix[4:8])) > entry.size-8 {
				return "", ErrDenied
			}
			return "application/vnd.ms-powerpoint", nil
		}
	}
	return "", ErrDenied
}

func passiveCompoundOfficeDirectory(file *compoundFile, family string) bool {
	allowed := map[string]map[string]struct{}{
		"word": {
			"root entry": {}, "worddocument": {}, "0table": {}, "1table": {}, "data": {},
			"\x05summaryinformation": {}, "\x05documentsummaryinformation": {},
		},
		"excel": {
			"root entry": {}, "workbook": {}, "book": {},
			"\x05summaryinformation": {}, "\x05documentsummaryinformation": {},
		},
		"powerpoint": {
			"root entry": {}, "powerpoint document": {}, "current user": {}, "pictures": {},
			"\x05summaryinformation": {}, "\x05documentsummaryinformation": {},
		},
	}[family]
	if len(allowed) == 0 {
		return false
	}
	for _, entry := range file.directory {
		if entry.objectType == 0 {
			continue
		}
		if entry.objectType == 1 {
			return false
		}
		if _, ok := allowed[strings.ToLower(entry.name)]; !ok {
			return false
		}
	}
	return true
}

func parseCompoundFile(body []byte) (*compoundFile, error) {
	if len(body) < 512 || !bytes.Equal(body[:8], compoundFileSignature) || !allZero(body[8:24]) || binary.LittleEndian.Uint16(body[28:30]) != 0xfffe || !allZero(body[34:40]) {
		return nil, ErrDenied
	}
	major := binary.LittleEndian.Uint16(body[26:28])
	sectorShift := binary.LittleEndian.Uint16(body[30:32])
	miniShift := binary.LittleEndian.Uint16(body[32:34])
	if major != 3 && major != 4 || major == 3 && sectorShift != 9 || major == 4 && sectorShift != 12 || miniShift != 6 {
		return nil, ErrDenied
	}
	sectorSize := uint64(1) << sectorShift
	if uint64(len(body)) < sectorSize || uint64(len(body))%sectorSize != 0 || binary.LittleEndian.Uint32(body[56:60]) != uint32(cfbMiniStreamCut) {
		return nil, ErrDenied
	}
	if sectorSize > 512 && !allZero(body[512:sectorSize]) {
		return nil, ErrDenied
	}
	sectorCount64 := uint64(len(body))/sectorSize - 1
	if sectorCount64 == 0 || sectorCount64 > math.MaxUint32 {
		return nil, ErrDenied
	}
	file := &compoundFile{
		body: body, majorVersion: major, sectorSize: sectorSize, miniSectorSize: 64, sectorCount: uint32(sectorCount64),
		firstDirectory:  binary.LittleEndian.Uint32(body[48:52]),
		firstMiniFAT:    binary.LittleEndian.Uint32(body[60:64]),
		numMiniFAT:      binary.LittleEndian.Uint32(body[64:68]),
		firstDIFAT:      binary.LittleEndian.Uint32(body[68:72]),
		numDIFATSectors: binary.LittleEndian.Uint32(body[72:76]),
		numFATSectors:   binary.LittleEndian.Uint32(body[44:48]),
	}
	if major == 3 && binary.LittleEndian.Uint32(body[40:44]) != 0 || file.numFATSectors == 0 || file.firstDirectory >= file.sectorCount {
		return nil, ErrDenied
	}
	for offset := 76; offset < 512; offset += 4 {
		file.headerDIFAT = append(file.headerDIFAT, binary.LittleEndian.Uint32(body[offset:offset+4]))
	}
	if err := file.loadFAT(); err != nil {
		return nil, err
	}
	if err := file.loadDirectory(); err != nil {
		return nil, err
	}
	if err := file.loadMiniFATAndStream(); err != nil {
		return nil, err
	}
	return file, nil
}

func (file *compoundFile) loadFAT() error {
	fatSectors := make([]uint32, 0, file.numFATSectors)
	seen := map[uint32]struct{}{}
	appendFATSector := func(sector uint32) error {
		if sector == cfbFreeSector {
			return nil
		}
		if sector >= file.sectorCount {
			return ErrDenied
		}
		if _, duplicate := seen[sector]; duplicate {
			return ErrDenied
		}
		seen[sector] = struct{}{}
		fatSectors = append(fatSectors, sector)
		return nil
	}
	for _, sector := range file.headerDIFAT {
		if err := appendFATSector(sector); err != nil {
			return err
		}
	}
	current := file.firstDIFAT
	difatSectors := map[uint32]struct{}{}
	entriesPerSector := int(file.sectorSize / 4)
	for index := uint32(0); index < file.numDIFATSectors; index++ {
		if current >= file.sectorCount {
			return ErrDenied
		}
		if _, duplicate := difatSectors[current]; duplicate {
			return ErrDenied
		}
		difatSectors[current] = struct{}{}
		sector := file.sector(current)
		for entry := 0; entry < entriesPerSector-1; entry++ {
			if err := appendFATSector(binary.LittleEndian.Uint32(sector[entry*4 : entry*4+4])); err != nil {
				return err
			}
		}
		current = binary.LittleEndian.Uint32(sector[len(sector)-4:])
	}
	if file.numDIFATSectors == 0 && file.firstDIFAT != cfbEndOfChain || file.numDIFATSectors > 0 && current != cfbEndOfChain || uint32(len(fatSectors)) != file.numFATSectors {
		return ErrDenied
	}
	for _, sectorID := range fatSectors {
		sector := file.sector(sectorID)
		for offset := 0; offset < len(sector); offset += 4 {
			file.fat = append(file.fat, binary.LittleEndian.Uint32(sector[offset:offset+4]))
		}
	}
	if len(file.fat) < int(file.sectorCount) {
		return ErrDenied
	}
	for _, sectorID := range fatSectors {
		if file.fat[sectorID] != cfbFATSector {
			return ErrDenied
		}
	}
	for sectorID := range difatSectors {
		if file.fat[sectorID] != cfbDIFATSector {
			return ErrDenied
		}
	}
	return nil
}

func (file *compoundFile) loadDirectory() error {
	chain, err := file.followChain(file.firstDirectory, file.sectorCount)
	if err != nil || len(chain) == 0 {
		return ErrDenied
	}
	if file.majorVersion == 4 && uint32(len(chain)) != binary.LittleEndian.Uint32(file.body[40:44]) {
		return ErrDenied
	}
	directoryBytes := make([]byte, 0, uint64(len(chain))*file.sectorSize)
	for _, sectorID := range chain {
		directoryBytes = append(directoryBytes, file.sector(sectorID)...)
	}
	seenNames := map[string]struct{}{}
	rootCount := 0
	for offset := 0; offset+128 <= len(directoryBytes); offset += 128 {
		raw := directoryBytes[offset : offset+128]
		objectType := raw[66]
		if objectType == 0 {
			if binary.LittleEndian.Uint16(raw[64:66]) != 0 {
				return ErrDenied
			}
			file.directory = append(file.directory, compoundDirectoryEntry{objectType: 0, left: cfbNoStream, right: cfbNoStream, child: cfbNoStream})
			continue
		}
		if objectType != 1 && objectType != 2 && objectType != 5 || raw[67] > 1 {
			return ErrDenied
		}
		name, nameErr := compoundName(raw[:64], binary.LittleEndian.Uint16(raw[64:66]))
		if nameErr != nil {
			return nameErr
		}
		key := strings.ToLower(name)
		if _, duplicate := seenNames[key]; duplicate {
			return ErrDenied
		}
		seenNames[key] = struct{}{}
		size := binary.LittleEndian.Uint64(raw[120:128])
		if file.majorVersion == 3 && size > math.MaxUint32 {
			return ErrDenied
		}
		entry := compoundDirectoryEntry{
			name: name, objectType: objectType,
			left: binary.LittleEndian.Uint32(raw[68:72]), right: binary.LittleEndian.Uint32(raw[72:76]), child: binary.LittleEndian.Uint32(raw[76:80]),
			startSector: binary.LittleEndian.Uint32(raw[116:120]), size: size,
		}
		if objectType == 5 {
			rootCount++
			if offset != 0 || name != "Root Entry" {
				return ErrDenied
			}
		}
		file.directory = append(file.directory, entry)
	}
	if rootCount != 1 || len(file.directory) == 0 {
		return ErrDenied
	}
	for _, entry := range file.directory {
		for _, streamID := range []uint32{entry.left, entry.right, entry.child} {
			if streamID != cfbNoStream && streamID >= uint32(len(file.directory)) {
				return ErrDenied
			}
		}
	}
	return file.validateDirectoryTree()
}

func (file *compoundFile) validateDirectoryTree() error {
	root := file.directory[0]
	if root.objectType != 5 || root.left != cfbNoStream || root.right != cfbNoStream {
		return ErrDenied
	}
	visited := map[uint32]struct{}{0: {}}
	var visitTree func(uint32) error
	visitTree = func(streamID uint32) error {
		if streamID == cfbNoStream {
			return nil
		}
		if streamID == 0 || streamID >= uint32(len(file.directory)) {
			return ErrDenied
		}
		if _, duplicate := visited[streamID]; duplicate {
			return ErrDenied
		}
		entry := file.directory[streamID]
		if entry.objectType == 0 || entry.objectType == 5 || entry.objectType == 2 && entry.child != cfbNoStream {
			return ErrDenied
		}
		visited[streamID] = struct{}{}
		if err := visitTree(entry.left); err != nil {
			return err
		}
		if err := visitTree(entry.right); err != nil {
			return err
		}
		if entry.objectType == 1 {
			return visitTree(entry.child)
		}
		return nil
	}
	if err := visitTree(root.child); err != nil {
		return err
	}
	for index, entry := range file.directory {
		if entry.objectType == 0 {
			continue
		}
		if _, ok := visited[uint32(index)]; !ok {
			return ErrDenied
		}
	}
	return nil
}

func (file *compoundFile) loadMiniFATAndStream() error {
	root := file.directory[0]
	if file.numMiniFAT == 0 {
		if file.firstMiniFAT != cfbEndOfChain {
			return ErrDenied
		}
	} else {
		chain, err := file.followChainExact(file.firstMiniFAT, file.numMiniFAT)
		if err != nil {
			return err
		}
		for _, sectorID := range chain {
			sector := file.sector(sectorID)
			for offset := 0; offset < len(sector); offset += 4 {
				file.miniFAT = append(file.miniFAT, binary.LittleEndian.Uint32(sector[offset:offset+4]))
			}
		}
	}
	if root.size > maxContainerUncompressedBytes {
		return ErrDenied
	}
	if root.size > 0 {
		stream, err := file.readRegularStream(root.startSector, root.size)
		if err != nil {
			return err
		}
		file.rootMiniStream = stream
	}
	return nil
}

func (file *compoundFile) readStreamPrefix(entry compoundDirectoryEntry, limit uint64) ([]byte, error) {
	if entry.size == 0 || entry.size > maxContainerUncompressedBytes {
		return nil, ErrDenied
	}
	var stream []byte
	var err error
	if entry.size < cfbMiniStreamCut {
		stream, err = file.readMiniStream(entry.startSector, entry.size)
	} else {
		stream, err = file.readRegularStream(entry.startSector, entry.size)
	}
	if err != nil {
		return nil, err
	}
	if uint64(len(stream)) > limit {
		stream = stream[:limit]
	}
	return stream, nil
}

func (file *compoundFile) readRegularStream(start uint32, size uint64) ([]byte, error) {
	needed := uint32((size + file.sectorSize - 1) / file.sectorSize)
	chain, err := file.followChainExact(start, needed)
	if err != nil {
		return nil, err
	}
	body := make([]byte, 0, uint64(len(chain))*file.sectorSize)
	for _, sectorID := range chain {
		body = append(body, file.sector(sectorID)...)
	}
	return body[:size], nil
}

func (file *compoundFile) readMiniStream(start uint32, size uint64) ([]byte, error) {
	if len(file.miniFAT) == 0 || len(file.rootMiniStream) == 0 {
		return nil, ErrDenied
	}
	needed := uint32((size + file.miniSectorSize - 1) / file.miniSectorSize)
	chain := make([]uint32, 0, needed)
	seen := map[uint32]struct{}{}
	current := start
	for uint32(len(chain)) < needed {
		if current >= uint32(len(file.miniFAT)) || uint64(current+1)*file.miniSectorSize > uint64(len(file.rootMiniStream)) {
			return nil, ErrDenied
		}
		if _, duplicate := seen[current]; duplicate {
			return nil, ErrDenied
		}
		seen[current] = struct{}{}
		chain = append(chain, current)
		current = file.miniFAT[current]
	}
	if current != cfbEndOfChain {
		return nil, ErrDenied
	}
	body := make([]byte, 0, uint64(len(chain))*file.miniSectorSize)
	for _, sectorID := range chain {
		offset := uint64(sectorID) * file.miniSectorSize
		body = append(body, file.rootMiniStream[offset:offset+file.miniSectorSize]...)
	}
	return body[:size], nil
}

func (file *compoundFile) followChainExact(start uint32, expected uint32) ([]uint32, error) {
	chain, err := file.followChain(start, expected)
	if err != nil || uint32(len(chain)) != expected {
		return nil, ErrDenied
	}
	return chain, nil
}

func (file *compoundFile) followChain(start uint32, maximum uint32) ([]uint32, error) {
	chain := make([]uint32, 0, min(maximum, 16))
	seen := map[uint32]struct{}{}
	current := start
	for current != cfbEndOfChain {
		if current >= file.sectorCount || current >= uint32(len(file.fat)) || uint32(len(chain)) >= maximum {
			return nil, ErrDenied
		}
		if _, duplicate := seen[current]; duplicate {
			return nil, ErrDenied
		}
		seen[current] = struct{}{}
		chain = append(chain, current)
		next := file.fat[current]
		if next == cfbFreeSector || next == cfbFATSector || next == cfbDIFATSector {
			return nil, ErrDenied
		}
		current = next
	}
	return chain, nil
}

func (file *compoundFile) sector(sectorID uint32) []byte {
	offset := uint64(sectorID+1) * file.sectorSize
	return file.body[offset : offset+file.sectorSize]
}

func compoundName(raw []byte, length uint16) (string, error) {
	if length < 2 || length > 64 || length%2 != 0 || len(raw) < int(length) || binary.LittleEndian.Uint16(raw[length-2:length]) != 0 {
		return "", ErrDenied
	}
	units := make([]uint16, 0, length/2-1)
	for offset := 0; offset < int(length)-2; offset += 2 {
		unit := binary.LittleEndian.Uint16(raw[offset : offset+2])
		if unit == 0 {
			return "", ErrDenied
		}
		units = append(units, unit)
	}
	name := string(utf16.Decode(units))
	if name == "" || strings.ContainsRune(name, '\uFFFD') {
		return "", ErrDenied
	}
	return name, nil
}

func validBIFFPrefix(prefix []byte) bool {
	if len(prefix) < 8 {
		return false
	}
	recordType := binary.LittleEndian.Uint16(prefix[:2])
	recordLength := binary.LittleEndian.Uint16(prefix[2:4])
	if recordLength < 4 || int(recordLength)+4 > len(prefix) {
		return false
	}
	switch recordType {
	case 0x0009, 0x0209, 0x0409, 0x0809:
		return true
	default:
		return false
	}
}
