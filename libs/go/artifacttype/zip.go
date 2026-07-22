package artifacttype

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"encoding/xml"
	"errors"
	"io"
	"strings"
)

const (
	zipLocalHeaderSignature = 0x04034b50
	zipEOCDSignature        = 0x06054b50
	zipDescriptorSignature  = 0x08074b50
)

type checkedZIP struct {
	reader            *zip.Reader
	files             map[string]*zip.File
	localOrder        []string
	localExtraLengths map[string]uint16
}

func isZIPSignature(body []byte) bool {
	if len(body) < 4 {
		return false
	}
	signature := binary.LittleEndian.Uint32(body[:4])
	return signature == zipLocalHeaderSignature || signature == zipEOCDSignature
}

func detectZIPPackage(body []byte) (string, error) {
	container, err := validateZIP(body)
	if err != nil {
		return "", ErrDenied
	}
	mimetypeFile, hasMIMEType := container.files["mimetype"]
	contentTypesFile, hasContentTypes := container.files["[Content_Types].xml"]
	if hasMIMEType && hasContentTypes {
		return "", ErrDenied
	}
	if hasMIMEType {
		mimetype, readErr := readZIPEntry(mimetypeFile, 256)
		if readErr != nil {
			return "", ErrDenied
		}
		format, supported := packageMIMETypes[normalizeMediaType(string(mimetype))]
		if supported {
			if string(mimetype) != format.MediaType || len(container.localOrder) == 0 || container.localOrder[0] != "mimetype" || mimetypeFile.Method != zip.Store || container.localExtraLengths["mimetype"] != 0 {
				return "", ErrDenied
			}
			if hasActivePackagePart(container) {
				return "", ErrDenied
			}
			manifestFile, exists := container.files["META-INF/manifest.xml"]
			if !exists {
				return "", ErrDenied
			}
			if _, exists := container.files["content.xml"]; !exists {
				return "", ErrDenied
			}
			manifest, manifestErr := readZIPEntry(manifestFile, maxIdentificationEntryBytes)
			if manifestErr != nil || !validPackageManifest(manifest, format) {
				return "", ErrDenied
			}
			return format.MediaType, nil
		}
		return "", ErrDenied
	}
	if hasContentTypes {
		contentTypes, readErr := readZIPEntry(contentTypesFile, maxIdentificationEntryBytes)
		if readErr != nil {
			return "", ErrDenied
		}
		format, identifyErr := identifyOOXML(container, contentTypes)
		if identifyErr != nil {
			return "", ErrDenied
		}
		if validateOOXMLPassivePackage(container) != nil {
			return "", ErrDenied
		}
		return format.MediaType, nil
	}
	return "application/zip", nil
}

func validateZIP(body []byte) (checkedZIP, error) {
	if len(body) < 22 || binary.LittleEndian.Uint32(body[:4]) != zipLocalHeaderSignature && binary.LittleEndian.Uint32(body[:4]) != zipEOCDSignature {
		return checkedZIP{}, ErrDenied
	}
	eocdOffset, centralOffset, centralSize, entries, err := parseZIPEOCD(body)
	if err != nil || centralOffset+centralSize != uint64(eocdOffset) || entries > maxContainerEntries {
		return checkedZIP{}, ErrDenied
	}
	reader, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil || len(reader.File) != entries {
		return checkedZIP{}, ErrDenied
	}
	container := checkedZIP{
		reader: reader, files: make(map[string]*zip.File, len(reader.File)),
		localExtraLengths: make(map[string]uint16, len(reader.File)),
	}
	var totalUncompressed uint64
	for _, file := range reader.File {
		if !validContainerPath(file.Name, file.FileInfo().IsDir()) || file.Flags&(0x0001|0x0020|0x0040|0x2000) != 0 || file.Method != zip.Store && file.Method != zip.Deflate {
			return checkedZIP{}, ErrDenied
		}
		if malformedZIPExtra(file.Extra) || hasZIPExtraField(file.Extra, 0x0001) {
			return checkedZIP{}, ErrDenied
		}
		if file.CompressedSize64 > uint64(len(body)) || file.UncompressedSize64 > maxContainerEntryBytes || totalUncompressed > maxContainerUncompressedBytes-file.UncompressedSize64 {
			return checkedZIP{}, ErrDenied
		}
		if file.UncompressedSize64 > 0 && (file.CompressedSize64 == 0 || file.UncompressedSize64 > file.CompressedSize64*maxCompressionRatio) {
			return checkedZIP{}, ErrDenied
		}
		totalUncompressed += file.UncompressedSize64
		key := strings.ToLower(strings.TrimSuffix(file.Name, "/"))
		for existingName := range container.files {
			if strings.ToLower(strings.TrimSuffix(existingName, "/")) == key {
				return checkedZIP{}, ErrDenied
			}
		}
		container.files[file.Name] = file
	}
	if err := validateZIPLocalRecords(body, &container, centralOffset); err != nil {
		return checkedZIP{}, err
	}
	if err := validateZIPEntryStreams(reader.File); err != nil {
		return checkedZIP{}, err
	}
	return container, nil
}

func validateZIPEntryStreams(files []*zip.File) error {
	for _, file := range files {
		reader, err := file.Open()
		if err != nil {
			return ErrDenied
		}
		written, copyErr := io.Copy(io.Discard, io.LimitReader(reader, int64(file.UncompressedSize64)+1))
		closeErr := reader.Close()
		if copyErr != nil || closeErr != nil || written < 0 || uint64(written) != file.UncompressedSize64 {
			return ErrDenied
		}
	}
	return nil
}

func parseZIPEOCD(body []byte) (int, uint64, uint64, int, error) {
	minimum := max(0, len(body)-(1<<16)-22)
	for offset := len(body) - 22; offset >= minimum; offset-- {
		if binary.LittleEndian.Uint32(body[offset:offset+4]) != zipEOCDSignature {
			continue
		}
		commentLength := int(binary.LittleEndian.Uint16(body[offset+20 : offset+22]))
		if offset+22+commentLength != len(body) {
			continue
		}
		disk := binary.LittleEndian.Uint16(body[offset+4 : offset+6])
		centralDisk := binary.LittleEndian.Uint16(body[offset+6 : offset+8])
		entriesOnDisk := binary.LittleEndian.Uint16(body[offset+8 : offset+10])
		entries := binary.LittleEndian.Uint16(body[offset+10 : offset+12])
		centralSize := binary.LittleEndian.Uint32(body[offset+12 : offset+16])
		centralOffset := binary.LittleEndian.Uint32(body[offset+16 : offset+20])
		if disk != 0 || centralDisk != 0 || entriesOnDisk != entries || entries == 0xffff || centralSize == 0xffffffff || centralOffset == 0xffffffff {
			return 0, 0, 0, 0, ErrDenied
		}
		return offset, uint64(centralOffset), uint64(centralSize), int(entries), nil
	}
	return 0, 0, 0, 0, ErrDenied
}

func validateZIPLocalRecords(body []byte, container *checkedZIP, centralOffset uint64) error {
	if centralOffset > uint64(len(body)) {
		return ErrDenied
	}
	position := uint64(0)
	seen := make(map[string]struct{}, len(container.files))
	for position < centralOffset {
		if centralOffset-position < 30 || binary.LittleEndian.Uint32(body[position:position+4]) != zipLocalHeaderSignature {
			return ErrDenied
		}
		flags := binary.LittleEndian.Uint16(body[position+6 : position+8])
		method := binary.LittleEndian.Uint16(body[position+8 : position+10])
		crc := binary.LittleEndian.Uint32(body[position+14 : position+18])
		compressed32 := binary.LittleEndian.Uint32(body[position+18 : position+22])
		uncompressed32 := binary.LittleEndian.Uint32(body[position+22 : position+26])
		nameLength := uint64(binary.LittleEndian.Uint16(body[position+26 : position+28]))
		extraLength := uint64(binary.LittleEndian.Uint16(body[position+28 : position+30]))
		dataOffset := position + 30 + nameLength + extraLength
		if dataOffset > centralOffset || nameLength == 0 {
			return ErrDenied
		}
		name := string(body[position+30 : position+30+nameLength])
		extra := body[position+30+nameLength : dataOffset]
		file, exists := container.files[name]
		if !exists {
			return ErrDenied
		}
		if _, duplicate := seen[name]; duplicate || flags != file.Flags || method != file.Method || file.CompressedSize64 > centralOffset-dataOffset {
			return ErrDenied
		}
		if compressed32 == 0xffffffff || uncompressed32 == 0xffffffff || malformedZIPExtra(extra) || hasZIPExtraField(extra, 0x0001) {
			return ErrDenied
		}
		if flags&0x0008 == 0 && (crc != file.CRC32 || uint64(compressed32) != file.CompressedSize64 || uint64(uncompressed32) != file.UncompressedSize64) {
			return ErrDenied
		}
		seen[name] = struct{}{}
		container.localOrder = append(container.localOrder, name)
		container.localExtraLengths[name] = uint16(extraLength)
		position = dataOffset + file.CompressedSize64
		if flags&0x0008 != 0 {
			next, descriptorErr := parseZIPDescriptor(body, position, centralOffset, file)
			if descriptorErr != nil {
				return descriptorErr
			}
			position = next
		}
	}
	if position != centralOffset || len(seen) != len(container.files) {
		return ErrDenied
	}
	return nil
}

func parseZIPDescriptor(body []byte, position uint64, centralOffset uint64, file *zip.File) (uint64, error) {
	if centralOffset-position < 12 {
		return 0, ErrDenied
	}
	if binary.LittleEndian.Uint32(body[position:position+4]) == zipDescriptorSignature {
		position += 4
	}
	if centralOffset-position < 12 {
		return 0, ErrDenied
	}
	if binary.LittleEndian.Uint32(body[position:position+4]) != file.CRC32 || uint64(binary.LittleEndian.Uint32(body[position+4:position+8])) != file.CompressedSize64 || uint64(binary.LittleEndian.Uint32(body[position+8:position+12])) != file.UncompressedSize64 {
		return 0, ErrDenied
	}
	return position + 12, nil
}

func readZIPEntry(file *zip.File, limit uint64) ([]byte, error) {
	if file == nil || file.FileInfo().IsDir() || file.UncompressedSize64 > limit {
		return nil, ErrDenied
	}
	reader, err := file.Open()
	if err != nil {
		return nil, ErrDenied
	}
	body, readErr := io.ReadAll(io.LimitReader(reader, int64(limit)+1))
	closeErr := reader.Close()
	if readErr != nil || closeErr != nil || uint64(len(body)) != file.UncompressedSize64 || uint64(len(body)) > limit {
		return nil, ErrDenied
	}
	return body, nil
}

func malformedZIPExtra(extra []byte) bool {
	for len(extra) > 0 {
		if len(extra) < 4 {
			return true
		}
		length := int(binary.LittleEndian.Uint16(extra[2:4]))
		if length > len(extra)-4 {
			return true
		}
		extra = extra[4+length:]
	}
	return false
}

func hasZIPExtraField(extra []byte, expected uint16) bool {
	for len(extra) >= 4 {
		identifier := binary.LittleEndian.Uint16(extra[:2])
		length := int(binary.LittleEndian.Uint16(extra[2:4]))
		if length > len(extra)-4 {
			return false
		}
		if identifier == expected {
			return true
		}
		extra = extra[4+length:]
	}
	return false
}

func validPackageManifest(body []byte, format Format) bool {
	expectedNamespace := "urn:oasis:names:tc:opendocument:xmlns:manifest:1.0"
	if format.Family == "openoffice-xml" {
		expectedNamespace = "http://openoffice.org/2001/manifest"
	}
	decoder := xml.NewDecoder(bytes.NewReader(body))
	tokens := 0
	rootEntries := 0
	rootSeen := false
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil || tokens >= 20_000 {
			return false
		}
		tokens++
		start, ok := token.(xml.StartElement)
		if !ok {
			continue
		}
		if !rootSeen {
			if start.Name.Local != "manifest" || start.Name.Space != expectedNamespace {
				return false
			}
			rootSeen = true
		}
		if start.Name.Local != "file-entry" {
			continue
		}
		if start.Name.Space != expectedNamespace {
			return false
		}
		fullPath := ""
		mediaType := ""
		for _, attribute := range start.Attr {
			if attribute.Name.Space != expectedNamespace {
				continue
			}
			switch attribute.Name.Local {
			case "full-path":
				fullPath = attribute.Value
			case "media-type":
				mediaType = normalizeMediaType(attribute.Value)
			}
		}
		if fullPath == "/" {
			rootEntries++
			if mediaType != format.MediaType {
				return false
			}
		}
	}
	return rootSeen && rootEntries == 1
}

func identifyOOXML(container checkedZIP, body []byte) (Format, error) {
	relationshipsFile, ok := container.files["_rels/.rels"]
	if !ok {
		return Format{}, ErrDenied
	}
	decoder := xml.NewDecoder(bytes.NewReader(body))
	tokens := 0
	rootSeen := false
	overrideParts := map[string]struct{}{}
	matches := []ooxmlFormat{}
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil || tokens >= 20_000 {
			return Format{}, ErrDenied
		}
		tokens++
		start, ok := token.(xml.StartElement)
		if !ok {
			continue
		}
		if !rootSeen {
			if start.Name.Local != "Types" || start.Name.Space != "http://schemas.openxmlformats.org/package/2006/content-types" {
				return Format{}, ErrDenied
			}
			rootSeen = true
		}
		if start.Name.Local != "Override" && start.Name.Local != "Default" {
			continue
		}
		if start.Name.Space != "http://schemas.openxmlformats.org/package/2006/content-types" {
			return Format{}, ErrDenied
		}
		contentType := normalizeMediaType(attributeValue(start.Attr, "ContentType"))
		if activeOOXMLContentType(contentType) {
			return Format{}, ErrDenied
		}
		if start.Name.Local == "Default" {
			continue
		}
		partName := ""
		for _, attribute := range start.Attr {
			switch attribute.Name.Local {
			case "PartName":
				partName = strings.TrimPrefix(attribute.Value, "/")
			}
		}
		if !strings.HasPrefix(attributeValue(start.Attr, "PartName"), "/") || !validContainerPath(partName, false) {
			return Format{}, ErrDenied
		}
		partKey := strings.ToLower(partName)
		if _, duplicate := overrideParts[partKey]; duplicate {
			return Format{}, ErrDenied
		}
		overrideParts[partKey] = struct{}{}
		if format, ok := ooxmlMainContentTypes[contentType]; ok {
			if partName != format.MainPart {
				return Format{}, ErrDenied
			}
			matches = append(matches, format)
		}
	}
	if !rootSeen || len(matches) != 1 {
		return Format{}, ErrDenied
	}
	if _, ok := container.files[matches[0].MainPart]; !ok {
		return Format{}, ErrDenied
	}
	relationships, err := readZIPEntry(relationshipsFile, maxIdentificationEntryBytes)
	if err != nil || !validOOXMLRootRelationships(relationships, matches[0].MainPart) {
		return Format{}, ErrDenied
	}
	return matches[0].Format, nil
}

func validOOXMLRootRelationships(body []byte, expectedMainPart string) bool {
	decoder := xml.NewDecoder(bytes.NewReader(body))
	tokens := 0
	rootSeen := false
	matches := 0
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil || tokens >= 20_000 {
			return false
		}
		tokens++
		start, ok := token.(xml.StartElement)
		if !ok {
			continue
		}
		if !rootSeen {
			if start.Name.Local != "Relationships" || start.Name.Space != "http://schemas.openxmlformats.org/package/2006/relationships" {
				return false
			}
			rootSeen = true
		}
		if start.Name.Local != "Relationship" {
			continue
		}
		if start.Name.Space != "http://schemas.openxmlformats.org/package/2006/relationships" {
			return false
		}
		relationshipType := attributeValue(start.Attr, "Type")
		if relationshipType != "http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" && relationshipType != "http://purl.oclc.org/ooxml/officeDocument/relationships/officeDocument" {
			continue
		}
		if attributeValue(start.Attr, "TargetMode") != "" || strings.TrimPrefix(attributeValue(start.Attr, "Target"), "/") != expectedMainPart {
			return false
		}
		matches++
	}
	return rootSeen && matches == 1
}

func attributeValue(attributes []xml.Attr, localName string) string {
	for _, attribute := range attributes {
		if attribute.Name.Local == localName {
			return attribute.Value
		}
	}
	return ""
}

func hasActivePackagePart(container checkedZIP) bool {
	for name := range container.files {
		lower := strings.ToLower(strings.TrimSuffix(name, "/"))
		for _, prefix := range []string{"basic/", "scripts/", "meta-inf/scripts.xml", "meta-inf/basic-script-lc.xml"} {
			if lower == strings.TrimSuffix(prefix, "/") || strings.HasPrefix(lower, prefix) {
				return true
			}
		}
	}
	return false
}

func validateOOXMLPassivePackage(container checkedZIP) error {
	for name, file := range container.files {
		if activeOOXMLPartName(name) {
			return ErrDenied
		}
		if !strings.HasSuffix(strings.ToLower(name), ".rels") {
			continue
		}
		body, err := readZIPEntry(file, maxIdentificationEntryBytes)
		if err != nil || !validPassiveOOXMLRelationships(body) {
			return ErrDenied
		}
	}
	return nil
}

func activeOOXMLPartName(name string) bool {
	lower := strings.ToLower(strings.TrimSuffix(name, "/"))
	if strings.HasSuffix(lower, ".bin") {
		return true
	}
	for _, marker := range []string{
		"/activex/", "/embeddings/", "/externallinks/", "/macros/", "/customui/", "/webextensions/",
		"/ctrlprops/", "/macrosheets/", "/dialogsheets/", "/querytables/", "/model/",
	} {
		if strings.Contains("/"+lower+"/", marker) {
			return true
		}
	}
	for _, suffix := range []string{"/connections.xml", "/attachedtoolbars.xml"} {
		if strings.HasSuffix("/"+lower, suffix) {
			return true
		}
	}
	return false
}

func activeOOXMLContentType(contentType string) bool {
	for _, marker := range []string{"macroenabled", "vbaproject", "activex", "oleobject", "external", "ms-excel.sheet.binary", "x-msdownload"} {
		if strings.Contains(contentType, marker) {
			return true
		}
	}
	return false
}

func validPassiveOOXMLRelationships(body []byte) bool {
	decoder := xml.NewDecoder(bytes.NewReader(body))
	tokens := 0
	rootSeen := false
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil || tokens >= 20_000 {
			return false
		}
		tokens++
		start, ok := token.(xml.StartElement)
		if !ok {
			continue
		}
		if !rootSeen {
			if start.Name.Local != "Relationships" || start.Name.Space != "http://schemas.openxmlformats.org/package/2006/relationships" {
				return false
			}
			rootSeen = true
		}
		if start.Name.Local != "Relationship" {
			continue
		}
		if start.Name.Space != "http://schemas.openxmlformats.org/package/2006/relationships" {
			return false
		}
		if strings.TrimSpace(attributeValue(start.Attr, "TargetMode")) != "" {
			return false
		}
		target := strings.ToLower(strings.TrimSpace(attributeValue(start.Attr, "Target")))
		if target == "" || strings.Contains(target, "://") || strings.HasPrefix(target, "//") || strings.HasPrefix(target, "file:") || strings.HasPrefix(target, "data:") || strings.HasPrefix(target, "javascript:") || strings.HasPrefix(target, "mailto:") {
			return false
		}
		relationshipType := strings.ToLower(attributeValue(start.Attr, "Type"))
		for _, marker := range []string{"vbaproject", "activex", "oleobject", "external", "attachedtemplate", "hyperlink", "afchunk", "/package", "webextension", "control"} {
			if strings.Contains(relationshipType, marker) {
				return false
			}
		}
	}
	return rootSeen
}
