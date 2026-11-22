package managedssh

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"
	"testing"
)

// testRequest constructs one fully pinned synthetic request; all endpoint names and digests are unrelated to a real target.
// testRequest создаёт один полностью pinned synthetic request; endpoint names и digests не связаны с настоящим target.
func testRequest(operation Operation) Request {
	request := Request{Version: Version, ProfileDigest: strings.Repeat("a", 64), AttemptID: "11111111-1111-4111-8111-111111111111", Operation: operation}
	switch operation {
	case ProcessList:
		request.Limit = 128
	case FileList:
		request.Resource, request.Limit = "documents", 64
	case FileStat, FileCreate:
		request.Resource, request.Path = "documents", "folder/report.txt"
	case FileRead:
		request.Resource, request.Path, request.Expected, request.Limit = "documents", "folder/report.txt", strings.Repeat("b", 64), MaximumContent
	case FileReplace, FileDelete:
		request.Resource, request.Path, request.Expected = "documents", "folder/report.txt", strings.Repeat("b", 64)
	case ServiceRead, ServiceRestart, ServiceReload:
		request.Resource = "application"
	case JournalRead:
		request.Resource, request.Limit = "application", 64
	}
	return request
}

// TestRequestClosedOperationMatrix proves every selected recipe is encodable and unknown wire values never become reads or extension operations.
// TestRequestClosedOperationMatrix доказывает кодируемость каждого выбранного recipe; неизвестные wire values не становятся чтением либо extension operations.
func TestRequestClosedOperationMatrix(t *testing.T) {
	for operation := Inspect; operation <= JournalRead; operation++ {
		request := testRequest(operation)
		raw, err := EncodeRequest(request)
		if err != nil {
			t.Fatal(operation, err)
		}
		decoded, err := DecodeRequest(raw)
		if err != nil || decoded.Operation != operation {
			t.Fatal(operation, err)
		}
		parsed, err := ParseOperation(operation.Name())
		if err != nil || parsed != operation {
			t.Fatal(operation, err)
		}
	}
	for _, operation := range []Operation{0, JournalRead + 1, 255} {
		if _, err := operation.Effect(); err == nil {
			t.Fatal("unknown operation classified")
		}
		if operation.Name() != "" {
			t.Fatal("unknown operation named")
		}
		if _, err := EncodeRequest(testRequest(operation)); err == nil {
			t.Fatal("unknown operation encoded")
		}
	}
	for _, name := range []string{"ssh.file.read; sudo id", "ssh.file.read\n", "ssh.execute", "SSH.FILE.READ", "ssh.file.read.extra"} {
		if _, err := ParseOperation(name); err == nil {
			t.Fatal("unknown name accepted")
		}
	}
	for operation, expected := range map[Operation]EffectClass{FileCreate: ScopedChange, FileReplace: ScopedChange, FileDelete: EachChange, ServiceRestart: EachChange, ServiceReload: EachChange, ServiceRead: ReadOnly} {
		actual, err := operation.Effect()
		if err != nil || actual != expected {
			t.Fatal("recipe privilege classification changed")
		}
	}
}

// TestCanonicalJSONRejectsAmbiguity exercises the actual decoder boundary rather than a parallel schema or a duplicate token scanner.
// TestCanonicalJSONRejectsAmbiguity проверяет actual decoder boundary, а не параллельную schema либо duplicate token scanner.
func TestCanonicalJSONRejectsAmbiguity(t *testing.T) {
	raw, err := EncodeRequest(testRequest(Inspect))
	if err != nil {
		t.Fatal(err)
	}
	for _, invalid := range [][]byte{
		append(bytes.Clone(raw), '\n'),
		append([]byte(" "), raw...),
		bytes.Replace(raw, []byte(`"version":1`), []byte(`"version":1,"version":1`), 1),
		bytes.Replace(raw, []byte(`"version":1`), []byte(`"Version":1`), 1),
		bytes.Replace(raw, []byte(`"version":1`), []byte(`"version":1,"Version":1`), 1),
		bytes.Replace(raw, []byte(`"version":1`), []byte(`"version":null`), 1),
		bytes.Replace(raw, []byte(`"version":1`), []byte(`"version":1,"content":null`), 1),
		bytes.Replace(raw, []byte(`"version":1`), []byte(`"version":1,"unknown":{}`), 1),
		bytes.Replace(raw, []byte(`"operation":1`), []byte(`"operation":1e0`), 1),
		[]byte(`null`), []byte(`{}`), append(bytes.Clone(raw), raw...), bytes.Repeat([]byte("x"), MaximumFrame+1),
	} {
		if _, err := DecodeRequest(invalid); err == nil {
			t.Fatalf("ambiguous request accepted: %q", invalid[:min(len(invalid), 300)])
		}
	}
}

// TestRecipeArgumentsDenyScopeAndShellExtensions ensures unused arguments and traversal cannot be smuggled through an otherwise valid read recipe.
// TestRecipeArgumentsDenyScopeAndShellExtensions проверяет, что лишние arguments и traversal нельзя передать через другой valid read recipe.
func TestRecipeArgumentsDenyScopeAndShellExtensions(t *testing.T) {
	for _, operation := range []Operation{Inspect, SystemRead, ProcessList, FileList, FileStat, FileRead, FileCreate, FileReplace, FileDelete, ServiceRead, ServiceRestart, ServiceReload, ReceiptRead, JournalRead} {
		request := testRequest(operation)
		request.ProfileDigest = strings.Repeat("0", 64)
		if request.Validate() == nil {
			t.Fatal("zero pin accepted")
		}
	}
	for _, path := range []string{"../etc/passwd", "/etc/passwd", "folder/../file", "a//b", "a/./b", "a/", "file\n", "a\\b", string([]byte{255}), strings.Repeat("a", 256)} {
		request := testRequest(FileRead)
		request.Path = path
		if request.Validate() == nil {
			t.Fatal("unsafe spelling accepted")
		}
	}
	request := testRequest(SystemRead)
	request.Content = []byte("sudo id")
	if request.Validate() == nil {
		t.Fatal("read accepted command content")
	}
	request = testRequest(FileReplace)
	request.Expected = ""
	if request.Validate() == nil {
		t.Fatal("unversioned replace accepted")
	}
	request = testRequest(FileCreate)
	request.Content = make([]byte, MaximumContent+1)
	if request.Validate() == nil {
		t.Fatal("unbounded content accepted")
	}
}

// TestRepliesBindExactRequestsAndNestedRequiredScalars verifies nullable/alias attacks cannot turn malformed results into success or move evidence to another invocation.
// TestRepliesBindExactRequestsAndNestedRequiredScalars проверяет, что nullable/alias attacks не превращают malformed results в success и не переносят evidence в другую invocation.
func TestRepliesBindExactRequestsAndNestedRequiredScalars(t *testing.T) {
	request := testRequest(Inspect)
	reply, _ := NewReply(request, Observed)
	reply.Identity = &Identity{UID: 1001, GID: 1001, Groups: []uint32{1001}, Executable: strings.Repeat("c", 64)}
	raw, err := EncodeReply(request, reply)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeReply(request, raw); err != nil {
		t.Fatal(err)
	}
	for _, invalid := range [][]byte{
		bytes.Replace(raw, []byte(`"uid":1001`), []byte(`"uid":null`), 1),
		bytes.Replace(raw, []byte(`"uid":1001`), []byte(`"uid":1001,"UID":0`), 1),
		bytes.Replace(raw, []byte(`"groups":[1001]`), []byte(`"groups":[0]`), 1),
		bytes.Replace(raw, []byte(`"uid":1001`), []byte(`"uid":0`), 1),
		bytes.Replace(raw, []byte(`"uid":1001`), []byte(`"uid":1001,"extra":"secret"`), 1),
	} {
		if _, err := DecodeReply(request, invalid); err == nil {
			t.Fatal("malformed identity accepted")
		}
	}
	foreign := request
	foreign.AttemptID = "22222222-2222-4222-8222-222222222222"
	if _, err := DecodeReply(foreign, raw); err == nil {
		t.Fatal("foreign attempt reply accepted")
	}
	request = testRequest(FileRead)
	reply, _ = NewReply(request, Observed)
	reply.Fragment = &Fragment{File: File{Size: 2, Digest: request.Expected, Modified: 1}, Content: []byte("a"), Truncated: true}
	if _, err := EncodeReply(request, reply); err == nil {
		t.Fatal("short non-limit fragment accepted")
	}
	reply.Fragment.Content = []byte("ab")
	reply.Fragment.Truncated = false
	if _, err := EncodeReply(request, reply); err != nil {
		t.Fatal(err)
	}
}

// TestReceiptRecoveryCannotBecomeReplay proves protected observation preserves the original effect digest and never encodes a new mutation inside a read.
// TestReceiptRecoveryCannotBecomeReplay доказывает, что protected observation сохраняет исходный effect digest и не кодирует новую mutation внутри read.
func TestReceiptRecoveryCannotBecomeReplay(t *testing.T) {
	write := testRequest(FileReplace)
	digest, _ := RequestDigest(write)
	recovery := testRequest(ReceiptRead)
	reply, _ := NewReply(recovery, Observed)
	reply.Receipt = &Receipt{AttemptID: write.AttemptID, RequestDigest: digest, Operation: FileReplace, Status: Unknown}
	raw, err := EncodeReply(recovery, reply)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeReply(recovery, raw)
	if err != nil || decoded.Receipt.Status != Unknown || decoded.Receipt.RequestDigest != digest {
		t.Fatal("unknown effect changed")
	}
	reply.Receipt.Operation = SystemRead
	if _, err := EncodeReply(recovery, reply); err == nil {
		t.Fatal("read recorded as managed effect")
	}
	writeReply, _ := NewReply(write, Applied)
	writeReply.Receipt = &Receipt{AttemptID: write.AttemptID, RequestDigest: strings.Repeat("d", 64), Operation: FileReplace, Status: Applied}
	if _, err := EncodeReply(write, writeReply); err == nil {
		t.Fatal("different input receipt accepted")
	}
}

// TestProfileIsFiniteDisjointAndPinned exercises deployment configuration separately from request authority and actual operator ownership evidence.
// TestProfileIsFiniteDisjointAndPinned проверяет deployment configuration отдельно от request authority и actual operator ownership evidence.
func TestProfileIsFiniteDisjointAndPinned(t *testing.T) {
	profile := Profile{Version: Version, Revision: 1, ValidUntil: 1800000000, UID: 1001, GID: 1001, Groups: []uint32{1001}, ExecutableDigest: strings.Repeat("c", 64), SystemctlDigest: strings.Repeat("d", 64), JournalctlDigest: strings.Repeat("e", 64), ControlPolicyDigest: strings.Repeat("9", 64), ShellDigest: strings.Repeat("8", 64), StateDirectory: "/srv/kodex/receipts", ReceiptLimit: 128, Roots: []RootScope{{Alias: "documents", Path: "/srv/kodex/documents"}}, Services: []ServiceScope{{Alias: "application", Unit: "application.service", UnitDigest: strings.Repeat("f", 64), UID: 1002}}}
	raw, err := EncodeProfile(profile)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeProfile(raw)
	if err != nil || decoded.Revision != 1 {
		t.Fatal(err)
	}
	first, _ := profile.Digest()
	missingShell := profile
	missingShell.ShellDigest = ""
	if missingShell.Validate() == nil {
		t.Fatal("unqualified account shell accepted")
	}
	changedShell := profile
	changedShell.ShellDigest = strings.Repeat("7", 64)
	changedDigest, err := changedShell.Digest()
	if err != nil || changedDigest == first {
		t.Fatal("account-shell change did not invalidate profile pin")
	}
	for _, uid := range []uint32{0, profile.UID} {
		invalid := profile
		invalid.Services = []ServiceScope{{Alias: "application", Unit: "application.service", UnitDigest: strings.Repeat("f", 64), UID: uid}}
		if invalid.Validate() == nil {
			t.Fatal("service shares privileged or helper identity")
		}
	}
	profile.ValidUntil++
	second, _ := profile.Digest()
	if first == second {
		t.Fatal("expiry did not change pin")
	}
	profile.Roots[0].Path = "/srv/kodex/receipts/subdirectory"
	if profile.Validate() == nil {
		t.Fatal("receipt namespace exposed")
	}
	var untrusted map[string]any
	if err := json.Unmarshal(raw, &untrusted); err != nil {
		t.Fatal(err)
	}
	untrusted["sudo"] = true
	invalid, _ := json.Marshal(untrusted)
	if _, err := DecodeProfile(invalid); err == nil {
		t.Fatal("unknown profile branch accepted")
	}
}

// TestEncodedReadCollectionBudgetPreservesExplicitTruncation checks JSON escaping against the real broker wire budget and never silently shortens required fragments or malformed data.
// TestEncodedReadCollectionBudgetPreservesExplicitTruncation проверяет JSON escaping по настоящему broker wire budget без скрытого усечения required fragments либо malformed data.
func TestEncodedReadCollectionBudgetPreservesExplicitTruncation(t *testing.T) {
	request := testRequest(FileList)
	reply, _ := NewReply(request, Observed)
	for index := 0; index < 64; index++ {
		name := strings.Repeat("<", 250) + strconv.FormatInt(int64(index+100), 10)
		reply.Entries = append(reply.Entries, Entry{Name: name, Kind: "FILE"})
	}
	if _, err := EncodeReply(request, reply); err == nil {
		t.Fatal("unbounded escaped collection encoded")
	}
	raw, err := EncodeBoundedReply(request, reply)
	if err != nil || len(raw) > MaximumReply {
		t.Fatal("bounded encoder failed")
	}
	decoded, err := DecodeReply(request, raw)
	if err != nil || !decoded.Truncated || len(decoded.Entries) == 0 || len(decoded.Entries) >= len(reply.Entries) {
		t.Fatal("collection truncation lost its semantics")
	}
	if decoded.Entries[0] != reply.Entries[0] {
		t.Fatal("bounded page ordering changed")
	}
	reply.Entries[0].Kind = "UNSUPPORTED"
	if _, err := EncodeBoundedReply(request, reply); err == nil {
		t.Fatal("truncation masked malformed data")
	}
}
